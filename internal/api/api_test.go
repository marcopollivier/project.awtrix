package api

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/state"
)

func TestMetrics(t *testing.T) {
	st, _ := state.Open(filepath.Join(t.TempDir(), "state.json"))
	at := time.Unix(1760649000, 0)
	st.Update(func(d *state.Data) {
		d.Counters["github_followers"] = &state.Counter{Value: 794, UpdatedAt: at, Day: "2026-10-16", DayStart: 790}
	})
	h := Handler(st, func() time.Time { return at.Add(time.Minute) })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/metrics", nil))
	want := `{"now":1760649060,"metrics":{"github_followers":{"value":794,"updatedAt":1760649000}}}` + "\n"
	if rec.Code != 200 || rec.Body.String() != want {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/metrics/github_followers", nil))
	if want := `{"age":60,"value":794}` + "\n"; rec.Code != 200 || rec.Body.String() != want {
		t.Fatalf("one metric: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/metrics/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown metric: %d", rec.Code)
	}
}
