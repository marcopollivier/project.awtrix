// Package state persists what the hub needs to remember between runs:
// counters (for daily deltas and milestones) and the last football match seen.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Counter struct {
	Value      int64     `json:"value"`
	UpdatedAt  time.Time `json:"updated_at"`
	Day        string    `json:"day"`       // YYYY-MM-DD (local) of DayStart
	DayStart   int64     `json:"day_start"` // value at the first reading of Day
	Celebrated int64     `json:"celebrated"`
}

type Match struct {
	EventID   string    `json:"event_id"`
	League    string    `json:"league"`
	Date      time.Time `json:"date"`
	Opponent  string    `json:"opponent"`
	Home      bool      `json:"home"`
	Status    string    `json:"status"` // pre | in | post
	Period    int       `json:"period"`
	Clock     string    `json:"clock"`
	Ours      int       `json:"ours"`
	Theirs    int       `json:"theirs"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Data struct {
	Counters map[string]*Counter `json:"counters"`
	Match    *Match              `json:"match,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, data: Data{Counters: map[string]*Counter{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data.Counters == nil {
		s.data.Counters = map[string]*Counter{}
	}
	return s, nil
}

// Update applies fn under lock and saves the result atomically.
func (s *Store) Update(fn func(*Data)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
	return writeJSON(s.path, s.data)
}

// View reads under lock without saving.
func (s *Store) View(fn func(*Data)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Manual values (e.g. LinkedIn followers) live in their own file so that
// `awtrix-hub set` can write them while `awtrix-hub run` is running.
type ManualValue struct {
	Value     int64     `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

func ReadManual(path string) (map[string]ManualValue, error) {
	m := map[string]ManualValue{}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	return m, json.Unmarshal(b, &m)
}

func SetManual(path, key string, v int64, now time.Time) error {
	m, err := ReadManual(path)
	if err != nil {
		return err
	}
	m[key] = ManualValue{Value: v, UpdatedAt: now}
	return writeJSON(path, m)
}
