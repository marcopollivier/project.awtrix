// Package awtrix contains the payload types and HTTP client for the AWTRIX NG API.
// Field names mirror https://blueforcer.github.io/awtrix-ng/esp32/reference/payload/.
// NG validates strictly (unknown keys answer 422), so only known keys are sent.
package awtrix

import (
	"encoding/json"
	"strings"
)

// Fragment is a piece of text with its own color.
type Fragment struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
}

// Text is either a plain string or a list of colored fragments.
type Text struct {
	Plain     string
	Fragments []Fragment
}

func Plain(s string) Text           { return Text{Plain: s} }
func Colored(fs ...Fragment) Text   { return Text{Fragments: fs} }
func Frag(t, color string) Fragment { return Fragment{Text: t, Color: color} }
func (t Text) IsZero() bool         { return t.Plain == "" && len(t.Fragments) == 0 }

// String returns the visible text without colors.
func (t Text) String() string {
	if len(t.Fragments) == 0 {
		return t.Plain
	}
	var b strings.Builder
	for _, f := range t.Fragments {
		b.WriteString(f.Text)
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

// Scroll controls how text that does not fit moves.
type Scroll struct {
	Mode  string `json:"mode,omitempty"`  // static | wrap | loop | bounce
	Speed int    `json:"speed,omitempty"` // percent
}

// Rainbow is the NG way of AWTRIX 3's "rainbow": a palette applied to the text.
const Rainbow = "Rainbow"

// App is a pushed app shown in the rotation (PUT /api/v1/apps/pushed/{name}).
type App struct {
	Text               Text    `json:"text,omitzero"`
	Icon               string  `json:"icon,omitempty"`
	TextColor          string  `json:"textColor,omitempty"` // "palette" colors the text with Palette
	DurationMs         int     `json:"durationMs,omitempty"`
	LifetimeMs         int     `json:"lifetimeMs,omitempty"` // removed after this long without update
	Progress           *int    `json:"progress,omitempty"`   // 0-100, bottom row
	ProgressColor      string  `json:"progressColor,omitempty"`
	ProgressTrackColor string  `json:"progressTrackColor,omitempty"`
	Palette            string  `json:"palette,omitempty"` // named palette, e.g. Rainbow
	TextBlinkMs        int     `json:"textBlinkMs,omitempty"`
	IconMode           string  `json:"iconMode,omitempty"`  // fixed | pushOnce | push
	TextAlign          string  `json:"textAlign,omitempty"` // center | start
	Scroll             *Scroll `json:"scroll,omitempty"`
	Repeat             int     `json:"repeat,omitempty"` // scroll passes before moving on
}

// WithRainbow colors the whole text with the rainbow palette.
func (a App) WithRainbow() App {
	a.Palette, a.TextColor = Rainbow, "palette"
	return a
}

// IsRainbow reports whether the text uses the rainbow palette.
func (a App) IsRainbow() bool { return a.TextColor == "palette" && a.Palette == Rainbow }

// Sound is a melody for a notification ({"rtttl": "..."}).
type Sound struct {
	RTTTL string `json:"rtttl,omitempty"`
}

// Notification interrupts the rotation (POST /api/v1/notifications).
type Notification struct {
	App
	Hold   bool   `json:"hold,omitempty"`
	Stack  *bool  `json:"stack,omitempty"`
	Wakeup bool   `json:"wakeup,omitempty"`
	Sound  *Sound `json:"sound,omitempty"`
}

// RTTTL returns the melody to play ("" if none).
func (n Notification) RTTTL() string {
	if n.Sound == nil {
		return ""
	}
	return n.Sound.RTTTL
}
