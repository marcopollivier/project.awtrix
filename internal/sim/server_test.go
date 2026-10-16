package sim

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerSpeaksNG(t *testing.T) {
	s := New(slog.New(slog.DiscardHandler), t.TempDir(), t.TempDir())
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	call := func(method, path, ctype, body string) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := call("PUT", "/api/v1/apps/pushed/a", "application/json", `{"text":"A","durationMs":5000}`); got != 200 {
		t.Fatalf("push: %d", got)
	}
	if got := s.disp.Loop(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("loop after push: %v", got)
	}
	if got := call("PUT", "/api/v1/apps/pushed/b", "", `{"text":"B"}`); got != http.StatusUnsupportedMediaType {
		t.Fatalf("missing content type: %d", got)
	}
	if got := call("PUT", "/api/v1/apps/pushed/b", "application/json", `{"text":"B","progressC":"#fff"}`); got != http.StatusUnprocessableEntity {
		t.Fatalf("AWTRIX 3 key should be refused: %d", got)
	}
	if got := call("POST", "/api/v1/notifications", "application/json", `{"text":"OI","sound":{"rtttl":"x:d=4:c"}}`); got != 200 {
		t.Fatalf("notify: %d", got)
	}
	if snap := s.disp.Snapshot(); snap.Kind != "notify" || snap.Sound != "x:d=4:c" {
		t.Fatalf("notification not shown: %+v", snap)
	}
	if got := call("DELETE", "/api/v1/apps/a", "", ""); got != 200 {
		t.Fatalf("delete: %d", got)
	}
	if got := call("DELETE", "/api/v1/apps/a", "", ""); got != http.StatusNotFound {
		t.Fatalf("delete missing: %d", got)
	}
}
