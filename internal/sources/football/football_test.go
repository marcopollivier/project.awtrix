package football

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/state"
)

var sp, _ = time.LoadLocation("America/Sao_Paulo")

func newSource(t *testing.T, st *state.Store, baseURL string) *Source {
	t.Helper()
	cfg := config.Default().Sources.Football
	s := New(cfg, func(k string) string { return k }, st, sp)
	s.BaseURL = baseURL
	return s
}

func texts(t *testing.T, s *Source, now time.Time) (app string, notes []string) {
	t.Helper()
	out, err := s.Fetch(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if a := out.Apps[AppName]; a != nil {
		app = a.Text.String()
	}
	for _, n := range out.Notifications {
		notes = append(notes, n.Text.String())
	}
	return app, notes
}

func TestMatchReplay(t *testing.T) {
	srv, err := FixtureServer("testdata/match", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	st, _ := state.Open(filepath.Join(t.TempDir(), "state.json"))
	s := newSource(t, st, srv.URL)

	kickoff := time.Date(2026, 10, 10, 21, 0, 0, 0, time.UTC) // 18h in São Paulo
	steps := []struct {
		at    time.Duration // relative to kick-off
		app   string
		notes []string
		phase Phase
	}{
		{-10 * time.Minute, "REM EM 10M", nil, Pre}, // summary_01 (pre)
		{3 * time.Minute, "VAS 0x0 REM 3'", []string{"COMECOU! VAS 0x0 REM"}, Live},
		{27 * time.Minute, "VAS 1x0 REM 27'", []string{"GOOOL DO VASCO! VAS 1x0 REM"}, Live},
		{47 * time.Minute, "VAS 1x0 REM INT", []string{"INTERVALO VAS 1x0 REM"}, Live},
		{75 * time.Minute, "VAS 1x1 REM 58'", []string{"GOL REM VAS 1x1 REM"}, Live},
		{98 * time.Minute, "VAS 2x1 REM 81'", []string{"GOOOL DO VASCO! VAS 2x1 REM"}, Live},
		{115 * time.Minute, "VAS 2x1 REM", []string{"FIM: VAS 2x1 REM"}, Post},
		{116 * time.Minute, "VAS 2x1 REM", nil, Post}, // no repeated final
	}
	for i, st := range steps {
		app, notes := texts(t, s, kickoff.Add(st.at))
		if app != st.app || strings.Join(notes, "|") != strings.Join(st.notes, "|") || s.phase != st.phase {
			t.Fatalf("step %d: app=%q notes=%q phase=%v; want app=%q notes=%q phase=%v",
				i, app, notes, s.phase, st.app, st.notes, st.phase)
		}
	}
}

func TestRestartMidMatchDoesNotRepeatGoals(t *testing.T) {
	srv, _ := FixtureServer("testdata/match", time.Time{})
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "state.json")
	st, _ := state.Open(path)
	s := newSource(t, st, srv.URL)
	kickoff := time.Date(2026, 10, 10, 21, 0, 0, 0, time.UTC)
	texts(t, s, kickoff.Add(-5*time.Minute)) // pre
	texts(t, s, kickoff.Add(3*time.Minute))  // 0x0
	texts(t, s, kickoff.Add(27*time.Minute)) // 1x0, goal notified

	// Restart: state reloaded from disk and ESPN still returns 1x0.
	dir := t.TempDir()
	copyFile(t, "testdata/match/team.json", filepath.Join(dir, "team.json"))
	copyFile(t, "testdata/match/summary_03.json", filepath.Join(dir, "summary_01.json"))
	srv2, _ := FixtureServer(dir, time.Time{})
	defer srv2.Close()
	st2, _ := state.Open(path)
	s2 := newSource(t, st2, srv2.URL)
	app, notes := texts(t, s2, kickoff.Add(28*time.Minute))
	if len(notes) != 0 || app != "VAS 1x0 REM 27'" {
		t.Fatalf("after restart: app=%q notes=%q", app, notes)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIdleAndMatchdayRendering(t *testing.T) {
	srv, _ := FixtureServer("testdata/match", time.Time{})
	defer srv.Close()
	st, _ := state.Open(filepath.Join(t.TempDir(), "state.json"))
	s := newSource(t, st, srv.URL)

	app, _ := texts(t, s, time.Date(2026, 10, 8, 12, 0, 0, 0, sp))
	if app != "SAB 18H REM" || s.phase != Idle {
		t.Fatalf("idle: %q %v", app, s.phase)
	}
	if got := s.Next(time.Date(2026, 10, 8, 12, 0, 0, 0, sp)); got != time.Hour {
		t.Fatalf("idle interval: %v", got)
	}
	app, _ = texts(t, s, time.Date(2026, 10, 10, 9, 0, 0, 0, sp))
	if app != "HOJE 18H REM" || s.phase != Matchday {
		t.Fatalf("matchday: %q %v", app, s.phase)
	}
	if got := s.Next(time.Date(2026, 10, 10, 9, 0, 0, 0, sp)); got != 10*time.Minute {
		t.Fatalf("matchday interval: %v", got)
	}
}

func TestParsesRealESPNTeamResponse(t *testing.T) {
	srv, _ := FixtureServer("testdata/real", time.Time{})
	defer srv.Close()
	st, _ := state.Open(filepath.Join(t.TempDir(), "state.json"))
	s := newSource(t, st, srv.URL)
	m, err := s.discover(context.Background(), nil, time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
	if err != nil || m == nil {
		t.Fatalf("discover: %v %v", m, err)
	}
	if m.EventID != "401841262" || m.Opponent != "REM" || !m.Home || m.Status != "pre" {
		t.Fatalf("unexpected match: %+v", m)
	}
}
