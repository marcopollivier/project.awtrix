package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/config"
)

func TestFetch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/marcopollivier", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/user.json")
	})
	mux.HandleFunc("/users/marcopollivier/repos", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Write([]byte("[]"))
			return
		}
		http.ServeFile(w, r, "testdata/repos.json")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New(config.GitHub{User: "marcopollivier"}, func(k string) string { return k })
	s.BaseURL = srv.URL
	out, err := s.Fetch(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("metrics: %+v", out.Metrics)
	}
	if f := out.Metrics[0]; f.Key != "github_followers" || f.Value != 793 || f.Icon != "github" {
		t.Errorf("followers: %+v", f)
	}
	if s := out.Metrics[1]; s.Key != "github_stars" || s.Value != 277 {
		t.Errorf("stars (forks excluded): %+v", s)
	}
}
