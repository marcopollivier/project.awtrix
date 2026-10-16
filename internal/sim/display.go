package sim

import (
	"slices"
	"sync"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
)

const (
	defaultDuration = 7 * time.Second
	notifyDuration  = 5 * time.Second
	// The browser scrolls ~1px every 60ms; used to estimate how long a
	// repeating notification stays up.
	scrollPxPerSec = 1000 / 60.0
	charWidth      = 4
)

type entry struct {
	Name      string     `json:"name"`
	App       awtrix.App `json:"app"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// Screen is what the browser draws.
type Screen struct {
	Kind  string      `json:"kind"` // app | notify | empty
	Name  string      `json:"name,omitempty"`
	App   *awtrix.App `json:"app,omitempty"`
	Sound string      `json:"sound,omitempty"` // rtttl to play
	Hold  bool        `json:"hold,omitempty"`
	Loop  []LoopItem  `json:"loop"`
	Queue int         `json:"queue"`
}

type LoopItem struct {
	Name      string    `json:"name"`
	Text      string    `json:"text"`
	Icon      string    `json:"icon,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	Current   bool      `json:"current"`
}

// Display mimics the clock's app loop: apps rotate by duration, notifications
// take over the screen, apps vanish when their lifetime expires.
type Display struct {
	mu      sync.Mutex
	apps    []*entry // loop order = insertion order, like the device
	queue   []awtrix.Notification
	cur     int
	notif   *awtrix.Notification
	until   time.Time
	publish func(string, any)
	wake    chan struct{}
	now     func() time.Time
}

func NewDisplay(publish func(string, any)) *Display {
	return &Display{publish: publish, wake: make(chan struct{}, 1), now: time.Now}
}

func (d *Display) Upsert(name string, app awtrix.App) {
	d.mu.Lock()
	i := d.index(name)
	if i >= 0 {
		d.apps[i].App, d.apps[i].UpdatedAt = app, d.now()
	} else {
		d.apps = append(d.apps, &entry{Name: name, App: app, UpdatedAt: d.now()})
	}
	// Refresh the screen only if this app is showing (or nothing was).
	refresh := d.notif == nil && (len(d.apps) == 1 || d.cur == i)
	if len(d.apps) == 1 {
		d.cur, d.until = 0, d.now().Add(d.duration(app))
	}
	d.mu.Unlock()
	if refresh {
		d.emit()
	} else {
		d.emitLoop()
	}
}

func (d *Display) Remove(name string) {
	d.mu.Lock()
	if i := d.index(name); i >= 0 {
		d.apps = slices.Delete(d.apps, i, i+1)
		if d.cur >= i && d.cur > 0 {
			d.cur--
		}
	}
	d.mu.Unlock()
	d.emit()
}

func (d *Display) Notify(n awtrix.Notification) {
	d.mu.Lock()
	stack := n.Stack == nil || *n.Stack
	if d.notif != nil && stack {
		d.queue = append(d.queue, n)
		d.mu.Unlock()
		d.emitLoop()
		return
	}
	d.showNotif(n)
	d.mu.Unlock()
	d.emit()
	d.poke()
}

func (d *Display) Dismiss() {
	d.mu.Lock()
	d.until = d.now()
	d.mu.Unlock()
	d.poke()
}

func (d *Display) Skip() { d.Dismiss() }

func (d *Display) Current() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.notif != nil {
		return "notification"
	}
	if len(d.apps) == 0 {
		return ""
	}
	return d.apps[d.cur].Name
}

func (d *Display) Loop() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	names := make([]string, len(d.apps))
	for i, e := range d.apps {
		names[i] = e.Name
	}
	return names
}

// Run advances the loop until stop is closed.
func (d *Display) Run(stop <-chan struct{}) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		case <-d.wake:
		}
		if d.step() {
			d.emit()
		}
	}
}

// step expires apps and advances the screen when its time is up.
func (d *Display) step() (changed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()

	before := len(d.apps)
	d.apps = slices.DeleteFunc(d.apps, func(e *entry) bool {
		return e.App.LifetimeMs > 0 && now.Sub(e.UpdatedAt) > time.Duration(e.App.LifetimeMs)*time.Millisecond
	})
	if len(d.apps) != before {
		d.cur, changed = 0, true
	}

	if now.Before(d.until) || (d.notif != nil && d.notif.Hold && d.until.IsZero()) {
		return changed
	}
	switch {
	case len(d.queue) > 0:
		n := d.queue[0]
		d.queue = d.queue[1:]
		d.showNotif(n)
	case d.notif != nil:
		d.notif = nil
		d.until = now.Add(d.curDuration())
	case len(d.apps) > 0:
		d.cur = (d.cur + 1) % len(d.apps)
		d.until = now.Add(d.curDuration())
	default:
		return changed
	}
	return true
}

func (d *Display) showNotif(n awtrix.Notification) {
	d.notif = &n
	if n.Hold {
		d.until = time.Time{} // until dismissed
		return
	}
	dur := notifyDuration
	if n.DurationMs > 0 {
		dur = time.Duration(n.DurationMs) * time.Millisecond
	}
	if n.Repeat > 0 { // scroll the text n times
		px := len([]rune(n.Text.String()))*charWidth + 32
		dur = time.Duration(float64(n.Repeat*px)/scrollPxPerSec*float64(time.Second)) + time.Second
	}
	d.until = d.now().Add(dur)
}

func (d *Display) curDuration() time.Duration {
	if len(d.apps) == 0 {
		return defaultDuration
	}
	return d.duration(d.apps[d.cur].App)
}

func (d *Display) duration(a awtrix.App) time.Duration {
	if a.DurationMs > 0 {
		return time.Duration(a.DurationMs) * time.Millisecond
	}
	return defaultDuration
}

func (d *Display) index(name string) int {
	return slices.IndexFunc(d.apps, func(e *entry) bool { return e.Name == name })
}

func (d *Display) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Display) Snapshot() Screen {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := Screen{Kind: "empty", Queue: len(d.queue)}
	for i, e := range d.apps {
		s.Loop = append(s.Loop, LoopItem{Name: e.Name, Text: e.App.Text.String(), Icon: e.App.Icon,
			UpdatedAt: e.UpdatedAt, Current: d.notif == nil && i == d.cur})
	}
	switch {
	case d.notif != nil:
		app := d.notif.App
		s.Kind, s.App, s.Sound, s.Hold = "notify", &app, d.notif.RTTTL(), d.notif.Hold
	case len(d.apps) > 0:
		app := d.apps[d.cur].App
		s.Kind, s.Name, s.App = "app", d.apps[d.cur].Name, &app
	}
	return s
}

func (d *Display) emit()     { d.publish("screen", d.Snapshot()) }
func (d *Display) emitLoop() { d.publish("loop", d.Snapshot()) }
