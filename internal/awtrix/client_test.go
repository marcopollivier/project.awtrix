package awtrix

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTextJSON(t *testing.T) {
	b, _ := json.Marshal(App{Text: Plain("793"), Icon: "2799"})
	if got, want := string(b), `{"text":"793","icon":"2799"}`; got != want {
		t.Fatalf("plain: got %s want %s", got, want)
	}
	b, _ = json.Marshal(App{Text: Colored(Frag("793", "#FFFFFF"), Frag("+5", "#00FF00")), Progress: new(0)})
	if got, want := string(b), `{"text":[{"text":"793","color":"#FFFFFF"},{"text":"+5","color":"#00FF00"}],"progress":0}`; got != want {
		t.Fatalf("fragments: got %s want %s", got, want)
	}
	var n Notification
	if err := json.Unmarshal([]byte(`{"text":[{"text":"GOL","color":"#FF0000"}],"textColor":"palette","palette":"Rainbow","hold":true,"sound":{"rtttl":"x:d=4:c"}}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Text.String() != "GOL" || !n.IsRainbow() || !n.Hold || n.RTTTL() != "x:d=4:c" {
		t.Fatalf("unmarshal: %+v", n)
	}
}

func TestNotificationJSON(t *testing.T) {
	n := Notification{App: App{Text: Plain("META!"), Repeat: 3}.WithRainbow(), Sound: &Sound{RTTTL: "p:d=16:c"}}
	b, _ := json.Marshal(n)
	want := `{"text":"META!","textColor":"palette","palette":"Rainbow","repeat":3,"sound":{"rtttl":"p:d=16:c"}}`
	if string(b) != want {
		t.Fatalf("got %s want %s", b, want)
	}
}

func TestClientSendsToAllTargets(t *testing.T) {
	var (
		mu  sync.Mutex
		got []string
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Content-Type")+" "+user+":"+pass+" "+string(b))
		if r.Method == http.MethodDelete {
			http.NotFound(w, r) // already gone: still a success
		}
	})
	a, b := httptest.NewServer(h), httptest.NewServer(h)
	defer a.Close()
	defer b.Close()
	broken := "http://127.0.0.1:1"

	c := NewClient([]string{a.URL, b.URL + "/", broken}, slog.New(slog.DiscardHandler)).WithAuth("u", "p")
	err := c.Custom(context.Background(), "github_followers", &App{Text: Plain("1")})
	if err == nil {
		t.Fatal("expected error from broken target")
	}
	if len(got) != 2 || got[0] != `PUT /api/v1/apps/pushed/github_followers application/json u:p {"text":"1"}` {
		t.Fatalf("unexpected requests: %q", got)
	}

	got = nil
	c = NewClient([]string{a.URL}, slog.New(slog.DiscardHandler))
	if err := c.Custom(context.Background(), "youtube_latest", nil); err != nil {
		t.Fatalf("delete of a missing app should succeed: %v", err)
	}
	if err := c.Notify(context.Background(), Notification{App: App{Text: Plain("OI")}}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`DELETE /api/v1/apps/youtube_latest  : `,
		`POST /api/v1/notifications application/json : {"text":"OI"}`,
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected requests: %q", got)
	}
}

func TestDeviceRoutes(t *testing.T) {
	var reqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/v1/files":
			if r.Method == http.MethodGet {
				w.Write([]byte(`{"files":[{"name":"21802.gif","size":10},{"name":"/ICONS/gh_stars.gif","size":20}],"usedBytes":1,"totalBytes":2}`))
				return
			}
			r.ParseMultipartForm(1 << 20)
			for _, fhs := range r.MultipartForm.File {
				reqs = append(reqs, "file "+fhs[0].Filename)
			}
		case "/api/v1/device":
			w.Write([]byte(`{"uptimeSeconds":42}`))
		case "/api/v1/apps":
			w.Write([]byte(`[{"name":"github","origin":"script","enabled":true,"inLoop":true,"error":{"message":"syntax error","line":3,"hook":"compile"}}]`))
		case "/api/v1/apps/script/github":
			b, _ := io.ReadAll(r.Body)
			reqs = append(reqs, r.Header.Get("Content-Type")+" "+string(b))
		case "/api/v1/apps/github/config":
			b, _ := io.ReadAll(r.Body)
			reqs = append(reqs, string(b))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	c := NewClient([]string{srv.URL}, slog.New(slog.DiscardHandler))

	names, ok, err := c.Icons(ctx, srv.URL)
	if err != nil || !ok || len(names) != 2 || names[1] != "gh_stars.gif" {
		t.Fatalf("icons: %v %v %v", names, ok, err)
	}
	if err := c.UploadIcon(ctx, srv.URL, "9554", []byte("GIF89a")); err != nil {
		t.Fatal(err)
	}
	if up, ok, err := c.Uptime(ctx, srv.URL); up != 42 || !ok || err != nil {
		t.Fatalf("uptime: %d %v %v", up, ok, err)
	}
	if err := c.PutScript(ctx, srv.URL, "github", []byte("return X()")); err != nil {
		t.Fatal(err)
	}
	if err := c.PatchAppConfig(ctx, srv.URL, "github", map[string]any{"user": "marco"}); err != nil {
		t.Fatal(err)
	}
	apps, err := c.Apps(ctx, srv.URL)
	if err != nil || len(apps) != 1 || apps[0].Error == nil || apps[0].Error.Line != 3 {
		t.Fatalf("apps: %+v %v", apps, err)
	}
	want := []string{
		"GET /api/v1/files?dir=/ICONS",
		"POST /api/v1/files?dir=/ICONS", "file 9554.gif",
		"GET /api/v1/device",
		"PUT /api/v1/apps/script/github", "text/plain return X()",
		"PATCH /api/v1/apps/github/config", `{"user":"marco"}`,
		"GET /api/v1/apps",
	}
	if strings.Join(reqs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s", strings.Join(reqs, "\n"))
	}
}
