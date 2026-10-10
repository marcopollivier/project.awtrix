// Package scheduler runs each source on its own cadence and turns its output
// into AWTRIX apps and notifications.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/milestones"
	"github.com/marcopollivier/project.awtrix/internal/render"
	"github.com/marcopollivier/project.awtrix/internal/sources"
	"github.com/marcopollivier/project.awtrix/internal/state"
)

// Display is what the scheduler needs from awtrix.Client.
type Display interface {
	Custom(ctx context.Context, name string, app *awtrix.App) error
	Notify(ctx context.Context, n awtrix.Notification) error
}

type Scheduler struct {
	cfg     *config.Config
	display Display
	store   *state.Store
	log     *slog.Logger
	sources []sources.Source
	Now     func() time.Time
}

func New(cfg *config.Config, d Display, st *state.Store, log *slog.Logger, srcs ...sources.Source) *Scheduler {
	return &Scheduler{cfg: cfg, display: d, store: st, log: log, sources: srcs, Now: time.Now}
}

// Run polls every source until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, src := range s.sources {
		wg.Go(func() { s.loop(ctx, src) })
	}
	wg.Wait()
}

// RunOnce fetches every source a single time (used by `preview`).
func (s *Scheduler) RunOnce(ctx context.Context) {
	for _, src := range s.sources {
		s.tick(ctx, src)
	}
}

func (s *Scheduler) loop(ctx context.Context, src sources.Source) {
	backoff := time.Minute
	for {
		wait := src.Next(s.Now())
		if err := s.tick(ctx, src); err != nil {
			wait = min(backoff, wait)
			backoff = min(backoff*2, 30*time.Minute)
		} else {
			backoff = time.Minute
			wait = src.Next(s.Now())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, src sources.Source) error {
	out, err := src.Fetch(ctx, s.Now())
	if err != nil {
		s.log.Warn("source failed", "source", src.Name(), "err", err)
		return err
	}
	s.log.Debug("source fetched", "source", src.Name(), "metrics", len(out.Metrics), "apps", len(out.Apps), "notifications", len(out.Notifications))
	for _, m := range out.Metrics {
		app, party := s.counter(m)
		s.send(ctx, s.display.Custom(ctx, m.Key, app))
		if party != nil {
			s.send(ctx, s.display.Notify(ctx, *party))
		}
	}
	for name, app := range out.Apps {
		if app != nil {
			s.withDefaults(app)
		}
		s.send(ctx, s.display.Custom(ctx, name, app))
	}
	for _, n := range out.Notifications {
		s.send(ctx, s.display.Notify(ctx, n))
	}
	return nil
}

// send only logs: the client already logged per target, and an offline
// display must not stop the sources from updating state.
func (s *Scheduler) send(_ context.Context, err error) {
	if err != nil {
		s.log.Debug("display update failed", "err", err)
	}
}

// counter updates the daily delta and milestones for m and renders its app.
func (s *Scheduler) counter(m sources.Metric) (*awtrix.App, *awtrix.Notification) {
	now := s.Now().In(s.cfg.Location)
	today := now.Format(time.DateOnly)
	rule := milestones.Rule(m.Milestones)

	var (
		delta   int64
		reached int64
		party   bool
	)
	s.store.Update(func(d *state.Data) {
		c, ok := d.Counters[m.Key]
		if !ok {
			c = &state.Counter{Day: today, DayStart: m.Value, Celebrated: -1}
			d.Counters[m.Key] = c
		}
		if c.Day != today {
			c.Day, c.DayStart = today, c.Value // last value seen before midnight
		}
		c.Value, c.UpdatedAt = m.Value, now
		c.Celebrated, party = rule.Check(c.Celebrated, m.Value)
		reached = c.Celebrated
		delta = c.Value - c.DayStart
	})

	colors := s.cfg.Colors
	textColor := colors.Text
	if m.Stale {
		textColor = "#666666"
	}
	frags := []awtrix.Fragment{awtrix.Frag(render.Count(m.Value), textColor)}
	if d := render.Delta(delta); d != "" {
		c := colors.Up
		if delta < 0 {
			c = colors.Down
		}
		frags = append(frags, awtrix.Frag(" "+d, c))
	}
	app := &awtrix.App{Text: awtrix.Colored(frags...), Icon: m.Icon}
	if pct, ok := rule.Progress(m.Value); ok {
		app.Progress, app.ProgressC, app.ProgressBC = new(pct), colors.Bar, "#202020"
	}
	s.withDefaults(app)

	if !party {
		return app, nil
	}
	s.log.Info("milestone reached", "metric", m.Key, "milestone", reached)
	return app, &awtrix.Notification{
		App: awtrix.App{
			Text:    awtrix.Plain(m.Label + " " + render.Count(reached) + "!"),
			Icon:    s.cfg.Icon("party"),
			Rainbow: true,
			Repeat:  3,
		},
		RTTTL: "party:d=16,o=5,b=180:c,e,g,c6,8p,g,2c6",
	}
}

func (s *Scheduler) withDefaults(a *awtrix.App) {
	if a.Duration == 0 {
		a.Duration = s.cfg.Defaults.Duration
	}
	if a.Lifetime == 0 {
		a.Lifetime = s.cfg.Defaults.Lifetime
	}
}
