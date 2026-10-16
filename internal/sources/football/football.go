// Package football follows a team through ESPN's unofficial API: next match,
// live score, goal/start/halftime/final notifications.
//
//	{base}/{league}/teams/{id}         -> nextEvent (one per league; we pick the earliest)
//	{base}/{league}/summary?event={id} -> live status and score
package football

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/sources"
	"github.com/marcopollivier/project.awtrix/internal/state"
)

const (
	AppName     = "football"
	preWindow   = 15 * time.Minute // start polling the summary this long before kick-off
	postWindow  = 2 * time.Hour    // keep showing the final score this long
	liveTimeout = 4 * time.Hour    // give up on a match stuck "in" (API hiccup)
)

type Phase int

const (
	Idle     Phase = iota // nothing today
	Matchday              // match later today
	Pre                   // kick-off within preWindow
	Live
	Post // finished, within postWindow
	None // no known match
)

type Source struct {
	cfg     config.Football
	icons   func(string) string
	store   *state.Store
	loc     *time.Location
	BaseURL string
	Fast    bool // fixture mode: poll every few seconds
	phase   Phase
}

func New(cfg config.Football, icons func(string) string, st *state.Store, loc *time.Location) *Source {
	return &Source{cfg: cfg, icons: icons, store: st, loc: loc, phase: None,
		BaseURL: "https://site.api.espn.com/apis/site/v2/sports/soccer"}
}

func (s *Source) Name() string { return "football" }

func (s *Source) Next(now time.Time) time.Duration {
	if s.Fast {
		return 3 * time.Second
	}
	switch s.phase {
	case Live:
		return 30 * time.Second
	case Pre:
		return time.Minute
	case Matchday:
		return 10 * time.Minute
	}
	// Idle/Post/None: hourly, but wake up in time for the pre-match window.
	wait := time.Hour
	s.store.View(func(d *state.Data) {
		if m := d.Match; m != nil && m.Status == "pre" {
			if until := m.Date.Add(-preWindow).Sub(now); until > 0 && until < wait {
				wait = until
			}
		}
	})
	return wait
}

func (s *Source) Fetch(ctx context.Context, now time.Time) (sources.Output, error) {
	var prev *state.Match
	s.store.View(func(d *state.Data) {
		if d.Match != nil {
			cp := *d.Match
			prev = &cp
		}
	})

	var cur *state.Match
	if prev != nil {
		cp := *prev
		cur = &cp
	}
	if prev == nil || !s.tracking(prev, now) {
		found, err := s.discover(ctx, prev, now)
		if err != nil {
			return sources.Output{}, err
		}
		if found != nil {
			if prev != nil && found.EventID != prev.EventID {
				prev = nil // new match: no events against the old one
			}
			cur = found
		}
	}
	if cur != nil && now.After(cur.Date.Add(-preWindow)) && cur.Status != "post" {
		if err := s.live(ctx, cur, now); err != nil {
			return sources.Output{}, err
		}
	}

	var out sources.Output
	if cur != nil {
		out.Notifications = s.events(prev, cur)
		// Best effort: the match stays in memory; the file only helps after a restart.
		_ = s.store.Update(func(d *state.Data) { d.Match = cur })
	}
	s.phase = s.phaseOf(cur, now)
	out.Apps = map[string]*awtrix.App{AppName: s.render(cur, now)}
	return out, nil
}

// tracking reports whether we should keep polling prev instead of looking for another match.
func (s *Source) tracking(m *state.Match, now time.Time) bool {
	switch m.Status {
	case "in":
		return now.Sub(m.Date) < liveTimeout
	case "post":
		return now.Sub(m.UpdatedAt) < postWindow
	default: // pre: re-discover hourly to catch rescheduling, but not once we're close
		return now.After(m.Date.Add(-preWindow))
	}
}

func (s *Source) phaseOf(m *state.Match, now time.Time) Phase {
	if m == nil {
		return None
	}
	switch m.Status {
	case "in":
		return Live
	case "post":
		if now.Sub(m.UpdatedAt) < postWindow {
			return Post
		}
		return None
	}
	switch {
	case now.After(m.Date.Add(-preWindow)):
		return Pre
	case sameDay(m.Date.In(s.loc), now.In(s.loc)):
		return Matchday
	}
	return Idle
}

// --- ESPN ---

type competitor struct {
	HomeAway string `json:"homeAway"`
	Score    score  `json:"score"`
	Team     struct {
		ID           string `json:"id"`
		Abbreviation string `json:"abbreviation"`
	} `json:"team"`
}

type status struct {
	DisplayClock string `json:"displayClock"`
	Period       int    `json:"period"`
	Type         struct {
		Name  string `json:"name"`
		State string `json:"state"` // pre | in | post
	} `json:"type"`
}

type competition struct {
	Competitors []competitor `json:"competitors"`
	Status      status       `json:"status"`
}

// discover asks every league for the team's next event and keeps the earliest,
// skipping a match that already finished (ESPN keeps returning it for a while).
func (s *Source) discover(ctx context.Context, prev *state.Match, now time.Time) (*state.Match, error) {
	var best *state.Match
	var lastErr error
	for _, league := range s.cfg.Leagues {
		var resp struct {
			Team struct {
				NextEvent []struct {
					ID           string        `json:"id"`
					Date         espnTime      `json:"date"`
					Competitions []competition `json:"competitions"`
				} `json:"nextEvent"`
			} `json:"team"`
		}
		if err := sources.GetJSON(ctx, fmt.Sprintf("%s/%s/teams/%s", s.BaseURL, league, s.cfg.TeamID), &resp); err != nil {
			lastErr = err
			continue
		}
		for _, ev := range resp.Team.NextEvent {
			if len(ev.Competitions) == 0 {
				continue
			}
			if prev != nil && prev.EventID == ev.ID && prev.Status == "post" {
				continue
			}
			m := &state.Match{EventID: ev.ID, League: league, Date: ev.Date.Time, UpdatedAt: now}
			s.apply(m, ev.Competitions[0])
			if m.Status == "post" {
				continue
			}
			if best == nil || m.Date.Before(best.Date) {
				best = m
			}
		}
	}
	if best == nil && lastErr != nil {
		return nil, lastErr
	}
	return best, nil
}

func (s *Source) live(ctx context.Context, m *state.Match, now time.Time) error {
	var resp struct {
		Header struct {
			Competitions []competition `json:"competitions"`
		} `json:"header"`
	}
	u := fmt.Sprintf("%s/%s/summary?event=%s", s.BaseURL, m.League, m.EventID)
	if err := sources.GetJSON(ctx, u, &resp); err != nil {
		return err
	}
	if len(resp.Header.Competitions) == 0 {
		return fmt.Errorf("summary %s: no competitions", m.EventID)
	}
	s.apply(m, resp.Header.Competitions[0])
	m.UpdatedAt = now
	return nil
}

func (s *Source) apply(m *state.Match, c competition) {
	m.Status = c.Status.Type.State
	m.Period = c.Status.Period
	m.Clock = c.Status.DisplayClock
	if c.Status.Type.Name == "STATUS_HALFTIME" {
		m.Clock = "INT"
	}
	for _, cp := range c.Competitors {
		if cp.Team.ID == s.cfg.TeamID {
			m.Home = cp.HomeAway == "home"
			m.Ours = int(cp.Score)
		} else {
			m.Opponent = shortName(cp.Team.Abbreviation)
			m.Theirs = int(cp.Score)
		}
	}
}

// --- events & rendering ---

func (s *Source) events(prev, cur *state.Match) []awtrix.Notification {
	if prev == nil || prev.EventID != cur.EventID {
		return nil
	}
	var ns []awtrix.Notification
	if prev.Status == "pre" && cur.Status == "in" {
		ns = append(ns, s.note("COMECOU! "+s.score(cur), "#FFFFFF", false))
	}
	if cur.Status == "in" || cur.Status == "post" {
		for range cur.Ours - prev.Ours {
			ns = append(ns, awtrix.Notification{
				App: awtrix.App{
					Text:   awtrix.Plain("GOOOL DO " + s.cfg.Name + "! " + s.score(cur)),
					Icon:   s.icons("goal"),
					Repeat: 2,
				}.WithRainbow(),
				Sound: &awtrix.Sound{RTTTL: "gol:d=8,o=5,b=200:c6,c6,c6,4g,a,4c6,p,c6,c6,c6,4g,a,2c6"},
			})
		}
		for range cur.Theirs - prev.Theirs {
			ns = append(ns, s.note("GOL "+cur.Opponent+" "+s.score(cur), "#FF3333", false))
		}
	}
	if prev.Clock != "INT" && cur.Clock == "INT" {
		ns = append(ns, s.note("INTERVALO "+s.score(cur), "#FFFFFF", false))
	}
	if prev.Status != "post" && cur.Status == "post" {
		ns = append(ns, s.note("FIM: "+s.score(cur), s.resultColor(cur), true))
	}
	return ns
}

func (s *Source) note(text, color string, sound bool) awtrix.Notification {
	n := awtrix.Notification{App: awtrix.App{Text: awtrix.Plain(text), Icon: s.icons("football"), TextColor: color, Repeat: 1}}
	if sound {
		n.Sound = &awtrix.Sound{RTTTL: "apito:d=4,o=6,b=180:8c,p,2c"}
	}
	return n
}

func (s *Source) render(m *state.Match, now time.Time) *awtrix.App {
	app := &awtrix.App{Icon: s.icons("football"), LifetimeMs: int((postWindow + time.Hour).Milliseconds())}
	switch s.phaseOf(m, now) {
	case None:
		return nil
	case Idle:
		d := m.Date.In(s.loc)
		app.Text = awtrix.Colored(
			awtrix.Frag(weekday(d)+" "+hour(d)+" ", "#FFFFFF"),
			awtrix.Frag(m.Opponent, "#FFD700"))
	case Matchday:
		app.Text = awtrix.Colored(
			awtrix.Frag("HOJE "+hour(m.Date.In(s.loc))+" ", "#FFD700"),
			awtrix.Frag(m.Opponent, "#FFFFFF"))
	case Pre:
		mins := max(0, int(m.Date.Sub(now).Minutes()))
		app.Text = awtrix.Plain(fmt.Sprintf("%s EM %dM", m.Opponent, mins))
		app.TextColor = "#FFD700"
	case Live:
		app.Text = awtrix.Colored(awtrix.Frag(s.score(m)+" ", "#FFFFFF"), awtrix.Frag(m.Clock, "#00FF66"))
		app.DurationMs = 15_000
	case Post:
		app.Text = awtrix.Plain(s.score(m))
		app.TextColor = s.resultColor(m)
	}
	return app
}

// score renders "VAS 1x0 REM" with the home team first.
func (s *Source) score(m *state.Match) string {
	if m.Home {
		return fmt.Sprintf("%s %dx%d %s", s.cfg.Abbrev, m.Ours, m.Theirs, m.Opponent)
	}
	return fmt.Sprintf("%s %dx%d %s", m.Opponent, m.Theirs, m.Ours, s.cfg.Abbrev)
}

func (s *Source) resultColor(m *state.Match) string {
	switch {
	case m.Ours > m.Theirs:
		return "#00FF66"
	case m.Ours < m.Theirs:
		return "#FF3333"
	}
	return "#FFD700"
}

// --- helpers ---

var weekdays = [...]string{"DOM", "SEG", "TER", "QUA", "QUI", "SEX", "SAB"}

func weekday(t time.Time) string { return weekdays[t.Weekday()] }

func hour(t time.Time) string {
	if t.Minute() == 0 {
		return fmt.Sprintf("%dH", t.Hour())
	}
	return fmt.Sprintf("%dH%02d", t.Hour(), t.Minute())
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// shortName keeps scores narrow on a 32px display ("REMO" -> "REM").
func shortName(abbr string) string {
	if len(abbr) > 3 {
		return abbr[:3]
	}
	return abbr
}

// score accepts "1", 1 or {"value":1,"displayValue":"1"}; missing means 0.
type score int

func (sc *score) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		*sc = score(n)
	case float64:
		*sc = score(x)
	case map[string]any:
		if f, ok := x["value"].(float64); ok {
			*sc = score(f)
		}
	}
	return nil
}

// espnTime parses "2026-10-10T21:00Z" (no seconds) as well as RFC 3339.
type espnTime struct{ time.Time }

func (t *espnTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for _, layout := range []string{"2006-01-02T15:04Z07:00", time.RFC3339} {
		if p, err := time.Parse(layout, s); err == nil {
			t.Time = p
			return nil
		}
	}
	return fmt.Errorf("bad ESPN time %q", s)
}
