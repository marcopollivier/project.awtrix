// Package sim is a browser simulator for the AWTRIX 3 clock. It speaks the same
// HTTP API as the device (the subset this project uses), so the hub can target
// it by URL, and streams what would be on screen to a canvas over SSE.
package sim

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
)

//go:embed web
var webFS embed.FS

type Server struct {
	log   *slog.Logger
	icons *Icons
	disp  *Display

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func New(log *slog.Logger, iconCache, customIcons string) *Server {
	s := &Server{log: log, icons: NewIcons(iconCache, customIcons), clients: map[chan []byte]struct{}{}}
	s.disp = NewDisplay(s.broadcast)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServerFS(web))
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /icons/{id}", s.icon)

	// AWTRIX 3 API subset
	mux.HandleFunc("POST /api/custom", s.custom)
	mux.HandleFunc("POST /api/notify", s.notify)
	mux.HandleFunc("POST /api/notify/dismiss", func(w http.ResponseWriter, r *http.Request) { s.disp.Dismiss() })
	mux.HandleFunc("POST /api/nextapp", func(w http.ResponseWriter, r *http.Request) { s.disp.Skip() })
	mux.HandleFunc("GET /api/loop", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.disp.Loop()) })
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": "simulator", "app": s.disp.Current()})
	})
	return mux
}

// Run drives the display loop until stop is closed.
func (s *Server) Run(stop <-chan struct{}) { s.disp.Run(stop) }

func (s *Server) custom(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	body, _ := io.ReadAll(r.Body)
	if len(strings.TrimSpace(string(body))) == 0 {
		s.disp.Remove(name)
		s.logRequest("custom", name, "removed")
		return
	}
	var app awtrix.App
	if err := json.Unmarshal(body, &app); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.disp.Upsert(name, app)
	s.logRequest("custom", name, string(body))
}

func (s *Server) notify(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var n awtrix.Notification
	if err := json.Unmarshal(body, &n); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.disp.Notify(n)
	s.logRequest("notify", "", string(body))
}

func (s *Server) icon(w http.ResponseWriter, r *http.Request) {
	ic, err := s.icons.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.log.Warn("icon", "id", r.PathValue("id"), "err", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "max-age=86400")
	writeJSON(w, ic)
}

// --- server-sent events ---

type event struct {
	Type string `json:"type"` // screen | log
	Data any    `json:"data"`
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan []byte, 32)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	send := func(b []byte) {
		w.Write([]byte("data: "))
		w.Write(b)
		w.Write([]byte("\n\n"))
		flusher.Flush()
	}
	first, _ := json.Marshal(event{Type: "screen", Data: s.disp.Snapshot()})
	send(first)
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			send(b)
		case <-ping.C:
			w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) broadcast(typ string, data any) {
	b, err := json.Marshal(event{Type: typ, Data: data})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- b:
		default: // slow client: drop
		}
	}
}

func (s *Server) logRequest(kind, name, body string) {
	s.log.Info("api", "kind", kind, "name", name, "body", body)
	s.broadcast("log", map[string]string{"time": time.Now().Format("15:04:05"), "kind": kind, "name": name, "body": body})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
