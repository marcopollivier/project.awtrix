package youtube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/config"
)

func server(t *testing.T, published string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/channels", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("forHandle") != "omarcodev" || r.URL.Query().Get("key") != "k" {
			http.Error(w, "bad query", 400)
			return
		}
		w.Write([]byte(`{"items":[{"statistics":{"subscriberCount":"12300"},"contentDetails":{"relatedPlaylists":{"uploads":"UU1"}}}]}`))
	})
	mux.HandleFunc("/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"contentDetails":{"videoId":"v1","videoPublishedAt":"` + published + `"}}]}`))
	})
	mux.HandleFunc("/videos", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"statistics":{"viewCount":"1534"}}]}`))
	})
	return httptest.NewServer(mux)
}

func TestFetchRecentVideo(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	srv := server(t, "2026-10-08T20:00:00Z")
	defer srv.Close()

	s := New(config.YouTube{Handle: "omarcodev", APIKey: "k", LatestWindow: 48 * time.Hour}, func(k string) string { return k })
	s.BaseURL = srv.URL
	out, err := s.Fetch(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if m := out.Metrics[0]; m.Value != 12300 || m.Key != "youtube_subs" {
		t.Errorf("subs: %+v", m)
	}
	if a := out.Apps[LatestApp]; a == nil || a.Text.String() != "NEW 1534" {
		t.Errorf("latest: %+v", a)
	}
}

func TestFetchOldVideoRemovesLatestApp(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	srv := server(t, "2026-09-01T20:00:00Z")
	defer srv.Close()

	s := New(config.YouTube{Handle: "omarcodev", APIKey: "k", LatestWindow: 48 * time.Hour}, func(k string) string { return k })
	s.BaseURL = srv.URL
	out, err := s.Fetch(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := out.Apps[LatestApp]; !ok || a != nil {
		t.Errorf("expected removal of latest app, got %+v", a)
	}
}
