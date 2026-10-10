package scheduler

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/sources"
	"github.com/marcopollivier/project.awtrix/internal/state"
)

type fakeDisplay struct {
	apps  map[string]*awtrix.App
	notes []awtrix.Notification
}

func (f *fakeDisplay) Custom(_ context.Context, name string, app *awtrix.App) error {
	f.apps[name] = app
	return nil
}

func (f *fakeDisplay) Notify(_ context.Context, n awtrix.Notification) error {
	f.notes = append(f.notes, n)
	return nil
}

type fakeSource struct{ value int64 }

func (f *fakeSource) Name() string                 { return "fake" }
func (f *fakeSource) Next(time.Time) time.Duration { return time.Minute }
func (f *fakeSource) Fetch(context.Context, time.Time) (sources.Output, error) {
	return sources.Output{Metrics: []sources.Metric{{
		Key: "gh", Label: "GITHUB", Icon: "1", Value: f.value, Milestones: config.Milestones{Step: 100},
	}}}, nil
}

func TestCounterDeltaProgressAndMilestone(t *testing.T) {
	cfg := config.Default()
	cfg.Location, _ = time.LoadLocation("America/Sao_Paulo")
	st, _ := state.Open(filepath.Join(t.TempDir(), "state.json"))
	d := &fakeDisplay{apps: map[string]*awtrix.App{}}
	src := &fakeSource{value: 793}
	s := New(cfg, d, st, slog.New(slog.DiscardHandler), src)

	day1 := time.Date(2026, 10, 9, 10, 0, 0, 0, cfg.Location)
	s.Now = func() time.Time { return day1 }
	s.RunOnce(context.Background())
	if got := d.apps["gh"].Text.String(); got != "793" {
		t.Fatalf("first reading: %q", got)
	}
	if p := d.apps["gh"].Progress; p == nil || *p != 93 {
		t.Fatalf("progress: %v", p)
	}
	if len(d.notes) != 0 {
		t.Fatalf("baseline must not celebrate: %+v", d.notes)
	}

	src.value = 805
	s.RunOnce(context.Background())
	if got := d.apps["gh"].Text.String(); got != "805 +12" {
		t.Fatalf("delta: %q", got)
	}
	if len(d.notes) != 1 || d.notes[0].Text.String() != "GITHUB 800!" {
		t.Fatalf("milestone: %+v", d.notes)
	}

	// Next day: the delta restarts from the last value seen.
	s.Now = func() time.Time { return day1.Add(24 * time.Hour) }
	src.value = 807
	s.RunOnce(context.Background())
	if got := d.apps["gh"].Text.String(); got != "807 +2" {
		t.Fatalf("next day delta: %q", got)
	}
	if len(d.notes) != 1 {
		t.Fatalf("no new milestone expected: %+v", d.notes)
	}
}
