package scheduler

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
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

type rebootingDisplay struct {
	fakeDisplay
	mu      sync.Mutex
	uptimes []int64
	calls   int
}

func (r *rebootingDisplay) Targets() []string { return []string{"clock"} }

func (r *rebootingDisplay) Uptime(context.Context, string) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	up := r.uptimes[min(r.calls, len(r.uptimes)-1)]
	r.calls++
	return up, true, nil
}

func (r *rebootingDisplay) Custom(ctx context.Context, name string, app *awtrix.App) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakeDisplay.Custom(ctx, name, app)
}

func TestReplaysAppsAfterReboot(t *testing.T) {
	RebootCheck = 10 * time.Millisecond
	cfg := config.Default()
	cfg.Location = time.UTC
	st, _ := state.Open(filepath.Join(t.TempDir(), "state.json"))
	d := &rebootingDisplay{fakeDisplay: fakeDisplay{apps: map[string]*awtrix.App{}}, uptimes: []int64{500, 530, 5, 35}}
	s := New(cfg, d, st, slog.New(slog.DiscardHandler), &fakeSource{value: 10})
	s.RunOnce(context.Background())

	d.mu.Lock()
	d.apps = map[string]*awtrix.App{} // the clock forgot everything
	d.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	s.watchReboots(ctx, d)

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.apps["gh"] == nil || d.apps["gh"].Text.String() != "10" {
		t.Fatalf("app not replayed after reboot: %+v", d.apps)
	}
}
