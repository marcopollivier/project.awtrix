package awtrix

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestTextJSON(t *testing.T) {
	b, _ := json.Marshal(App{Text: Plain("793"), Icon: "2799"})
	if got, want := string(b), `{"text":"793","icon":"2799"}`; got != want {
		t.Fatalf("plain: got %s want %s", got, want)
	}
	b, _ = json.Marshal(App{Text: Colored(Frag("793", "#FFFFFF"), Frag("+5", "#00FF00")), Progress: new(0)})
	if got, want := string(b), `{"text":[{"t":"793","c":"#FFFFFF"},{"t":"+5","c":"#00FF00"}],"progress":0}`; got != want {
		t.Fatalf("fragments: got %s want %s", got, want)
	}
	var n Notification
	if err := json.Unmarshal([]byte(`{"text":[{"t":"GOL","c":"#FF0000"}],"rainbow":true,"hold":true}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Text.String() != "GOL" || !n.Rainbow || !n.Hold {
		t.Fatalf("unmarshal: %+v", n)
	}
}

func TestClientSendsToAllTargets(t *testing.T) {
	var (
		mu  sync.Mutex
		got []string
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r.URL.RequestURI()+" "+string(b))
	})
	a, b := httptest.NewServer(h), httptest.NewServer(h)
	defer a.Close()
	defer b.Close()
	broken := "http://127.0.0.1:1"

	c := NewClient([]string{a.URL, b.URL + "/", broken}, slog.New(slog.DiscardHandler))
	err := c.Custom(context.Background(), "github followers", &App{Text: Plain("1")})
	if err == nil {
		t.Fatal("expected error from broken target")
	}
	if len(got) != 2 || got[0] != `/api/custom?name=github+followers {"text":"1"}` {
		t.Fatalf("unexpected requests: %q", got)
	}
}
