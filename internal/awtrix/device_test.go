package awtrix

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScriptAndReboot(t *testing.T) {
	rebooted := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/apps/script/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") != "github" {
			http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("return G()\n"))
	})
	mux.HandleFunc("POST /api/v1/device/reboot", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "awtrix" || p != "pw" {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" || r.ContentLength > 0 {
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		rebooted = true
		w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(nil, slog.New(slog.DiscardHandler)).WithAuth("awtrix", "pw")
	ctx := context.Background()

	src, found, err := c.Script(ctx, srv.URL, "github")
	if err != nil || !found || string(src) != "return G()\n" {
		t.Fatalf("installed: %q %v %v", src, found, err)
	}
	if _, found, err := c.Script(ctx, srv.URL, "tiktok"); err != nil || found {
		t.Fatalf("missing script should be found=false without error: %v %v", found, err)
	}
	if err := c.Reboot(ctx, srv.URL); err != nil || !rebooted {
		t.Fatalf("reboot: %v %v", rebooted, err)
	}
}
