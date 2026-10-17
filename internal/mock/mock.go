// Package mock fakes the GitHub, YouTube, Instagram and TikTok APIs with counters that
// grow on every request, so `run --mock` shows deltas, progress bars and
// milestone celebrations within a minute, without credentials.
package mock

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// Counter starts just below a milestone (see config.example.yaml steps) and grows each call.
type counter struct {
	mu    sync.Mutex
	value int64
	step  int64
}

func (c *counter) next() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value += c.step
	return c.value
}

// Server serves the fake APIs under /github, /youtube, /instagram and /tiktok.
func Server() *httptest.Server {
	var (
		followers = &counter{value: 796, step: 1}    // -> 800
		stars     = &counter{value: 296, step: 1}    // -> 300
		subs      = &counter{value: 12_960, step: 9} // -> 13K
		views     = &counter{value: 1_200, step: 37}
		insta     = &counter{value: 4_471, step: 4} // -> 4500
		tt        = &counter{value: 1_985, step: 3} // -> 2000
	)
	published := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /github/users/{user}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"followers":%d}`, followers.next())
	})
	mux.HandleFunc("GET /github/users/{user}/repos", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"stargazers_count":%d,"fork":false}]`, stars.next())
	})
	mux.HandleFunc("GET /youtube/channels", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"items":[{"statistics":{"subscriberCount":"%d"},"contentDetails":{"relatedPlaylists":{"uploads":"UUmock"}}}]}`, subs.next())
	})
	mux.HandleFunc("GET /youtube/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"items":[{"contentDetails":{"videoId":"mock","videoPublishedAt":"%s"}}]}`, published)
	})
	mux.HandleFunc("GET /youtube/videos", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"items":[{"statistics":{"viewCount":"%d"}}]}`, views.next())
	})
	mux.HandleFunc("GET /instagram/{ver}/me", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"followers_count":%d}`, insta.next())
	})
	mux.HandleFunc("POST /tiktok/v2/oauth/token/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"access_token":"act.mock","expires_in":86400,"refresh_token":"rft.mock","token_type":"Bearer"}`)
	})
	mux.HandleFunc("GET /tiktok/v2/user/info/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":{"user":{"follower_count":%d}},"error":{"code":"ok"}}`, tt.next())
	})
	return httptest.NewServer(mux)
}
