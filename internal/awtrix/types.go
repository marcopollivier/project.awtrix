// Package awtrix contains the payload types and HTTP client for the AWTRIX 3 API.
// Field names mirror https://blueforcer.github.io/awtrix3/#/api.
package awtrix

import (
	"encoding/json"
	"strings"
)

// Fragment is a piece of text with its own color.
type Fragment struct {
	T string `json:"t"`
	C string `json:"c,omitempty"`
}

// Text is either a plain string or a list of colored fragments.
type Text struct {
	Plain     string
	Fragments []Fragment
}

func Plain(s string) Text           { return Text{Plain: s} }
func Colored(fs ...Fragment) Text   { return Text{Fragments: fs} }
func Frag(t, color string) Fragment { return Fragment{T: t, C: color} }
func (t Text) IsZero() bool         { return t.Plain == "" && len(t.Fragments) == 0 }

// String returns the visible text without colors.
func (t Text) String() string {
	if len(t.Fragments) == 0 {
		return t.Plain
	}
	var b strings.Builder
	for _, f := range t.Fragments {
		b.WriteString(f.T)
	}
	return b.String()
}

func (t Text) MarshalJSON() ([]byte, error) {
	if len(t.Fragments) > 0 {
		return json.Marshal(t.Fragments)
	}
	return json.Marshal(t.Plain)
}

func (t *Text) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '[' {
		return json.Unmarshal(b, &t.Fragments)
	}
	return json.Unmarshal(b, &t.Plain)
}

// App is a custom app shown in the display loop (POST /api/custom?name=...).
type App struct {
	Text       Text   `json:"text,omitzero"`
	Icon       string `json:"icon,omitempty"`
	Color      string `json:"color,omitempty"`
	Duration   int    `json:"duration,omitempty"` // seconds on screen
	Lifetime   int    `json:"lifetime,omitempty"` // seconds without update before removal
	Pos        *int   `json:"pos,omitempty"`
	Progress   *int   `json:"progress,omitempty"` // 0-100, bottom bar
	ProgressC  string `json:"progressC,omitempty"`
	ProgressBC string `json:"progressBC,omitempty"`
	Rainbow    bool   `json:"rainbow,omitempty"`
	BlinkText  int    `json:"blinkText,omitempty"` // ms
	PushIcon   int    `json:"pushIcon,omitempty"`
	NoScroll   bool   `json:"noScroll,omitempty"`
	Center     *bool  `json:"center,omitempty"`
	Repeat     int    `json:"repeat,omitempty"`
}

// Notification interrupts the display loop (POST /api/notify).
type Notification struct {
	App
	Hold   bool   `json:"hold,omitempty"`
	Stack  *bool  `json:"stack,omitempty"`
	Sound  string `json:"sound,omitempty"`
	RTTTL  string `json:"rtttl,omitempty"`
	Wakeup bool   `json:"wakeup,omitempty"`
}
