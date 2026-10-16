// Package sim is a browser simulator for the AWTRIX NG clock. It speaks the same
// HTTP API as the device (the subset this project uses), so the hub can target
// it by URL, and streams what would be on screen to a canvas over SSE.
package sim

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
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

	// AWTRIX NG API subset
	mux.HandleFunc("PUT /api/v1/apps/pushed/{name}", s.pushed)
	mux.HandleFunc("DELETE /api/v1/apps/{name}", s.remove)
	mux.HandleFunc("POST /api/v1/notifications", s.notify)
	mux.HandleFunc("DELETE /api/v1/notifications/active", func(w http.ResponseWriter, r *http.Request) { s.disp.Dismiss(); ok(w) })
	mux.HandleFunc("POST /api/v1/apps/next", func(w http.ResponseWriter, r *http.Request) { s.disp.Skip(); ok(w) })
	mux.HandleFunc("GET /api/v1/apps", func(w http.ResponseWriter, r *http.Request) {
		apps := []map[string]any{}
		for _, name := range s.disp.Loop() {
			apps = append(apps, map[string]any{"name": name, "origin": "pushed", "enabled": true, "inLoop": true})
		}
		writeJSON(w, apps)
	})
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": "simulator", "app": s.disp.Current()})
	})
	return mux
}

// Run drives the display loop until stop is closed.
func (s *Server) Run(stop <-chan struct{}) { s.disp.Run(stop) }

func (s *Server) pushed(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	body, okBody := readJSON(w, r)
	if !okBody {
		return
	}
	var app awtrix.App
	if err := strictUnmarshal(body, &app); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "validationFailed", err.Error())
		return
	}
	s.disp.Upsert(name, app)
	s.logRequest("pushed", name, string(body))
	ok(w)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !slices.Contains(s.disp.Loop(), name) {
		apiError(w, http.StatusNotFound, "notFound", "no such app")
		return
	}
	s.disp.Remove(name)
	s.logRequest("delete", name, "")
	ok(w)
}

func (s *Server) notify(w http.ResponseWriter, r *http.Request) {
	body, okBody := readJSON(w, r)
	if !okBody {
		return
	}
	var n awtrix.Notification
	if err := strictUnmarshal(body, &n); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "validationFailed", err.Error())
		return
	}
	s.disp.Notify(n)
	s.logRequest("notify", "", string(body))
	ok(w)
}

// readJSON enforces what the clock enforces: a JSON content type and a body.
func readJSON(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if mt, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(mt) != "application/json" {
		apiError(w, http.StatusUnsupportedMediaType, "unsupportedMediaType", "Content-Type must be application/json")
		return nil, false
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8192+1))
	if len(body) > 8192 {
		apiError(w, http.StatusRequestEntityTooLarge, "payloadTooLarge", "payload over 8192 bytes")
		return nil, false
	}
	return body, true
}

// strictUnmarshal rejects unknown keys, like the clock (which answers 422).
func strictUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func apiError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func ok(w http.ResponseWriter) { writeJSON(w, map[string]bool{"ok": true}) }

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
