package instagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/config"
)

type fakeGraph struct {
	mu        sync.Mutex
	valid     string // token currently accepted
	refreshes int
}

func (f *fakeGraph) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v26.0/me", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Query().Get("access_token") != f.valid || r.URL.Query().Get("fields") != "followers_count" {
			http.Error(w, `{"error":{"code":190}}`, http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"followers_count":4521,"id":"1"}`))
	})
	mux.HandleFunc("GET /refresh_access_token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Query().Get("grant_type") != "ig_refresh_token" || r.URL.Query().Get("access_token") != f.valid {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		f.refreshes++
		f.valid = "refreshed"
		w.Write([]byte(`{"access_token":"refreshed","token_type":"bearer","expires_in":5184000}`))
	})
	return mux
}

func newSource(t *testing.T, url, token, path string) *Source {
	s := New(config.Instagram{Token: token, GraphVer: "v26.0"}, func(k string) string { return k }, path)
	s.BaseURL = url
	return s
}

func TestFetchAndWeeklyRefresh(t *testing.T) {
	g := &fakeGraph{valid: "seed"}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "instagram_token.json")
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

	s := newSource(t, srv.URL, "seed", path)
	out, err := s.Fetch(context.Background(), now)
	if err != nil || out.Metrics[0].Value != 4521 {
		t.Fatalf("fetch: %+v %v", out, err)
	}
	if g.refreshes != 0 {
		t.Fatal("fresh token must not be refreshed")
	}

	// A week later, after a restart: refreshes and keeps using the new token.
	s = newSource(t, srv.URL, "seed", path)
	if _, err := s.Fetch(context.Background(), now.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if g.refreshes != 1 {
		t.Fatalf("refreshes = %d", g.refreshes)
	}
	if _, err := s.Fetch(context.Background(), now.Add(9*24*time.Hour)); err != nil {
		t.Fatalf("refreshed token not used: %v", err)
	}
}

func TestNewTokenInEnvReplacesStoredOne(t *testing.T) {
	g := &fakeGraph{valid: "new"}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "instagram_token.json")
	now := time.Now()

	newSource(t, srv.URL, "old", path).Fetch(context.Background(), now) // stores "old"
	if _, err := newSource(t, srv.URL, "new", path).Fetch(context.Background(), now); err != nil {
		t.Fatalf("new .env token ignored: %v", err)
	}
}

func TestInvalidTokenAlertsOncePerDay(t *testing.T) {
	g := &fakeGraph{valid: "other"}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()
	s := newSource(t, srv.URL, "seed", filepath.Join(t.TempDir(), "t.json"))
	now := time.Now()

	out, err := s.Fetch(context.Background(), now)
	if err != nil || len(out.Notifications) != 1 {
		t.Fatalf("first failure should alert: %+v %v", out, err)
	}
	if _, err := s.Fetch(context.Background(), now.Add(time.Hour)); err == nil {
		t.Fatal("second failure within a day should be a plain error")
	}
}
