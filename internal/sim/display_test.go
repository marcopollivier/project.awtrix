package sim

import (
	"testing"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newTestDisplay(c *clock) *Display {
	d := NewDisplay(func(string, any) {})
	d.now = c.now
	return d
}

func TestLoopRotatesAndExpires(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	d := newTestDisplay(c)
	d.Upsert("a", awtrix.App{Text: awtrix.Plain("A"), Duration: 5})
	d.Upsert("b", awtrix.App{Text: awtrix.Plain("B"), Duration: 5, Lifetime: 8})

	if got := d.Snapshot().Name; got != "a" {
		t.Fatalf("first app: %q", got)
	}
	c.add(6 * time.Second)
	d.step()
	if got := d.Snapshot().Name; got != "b" {
		t.Fatalf("after duration: %q", got)
	}
	c.add(3 * time.Second) // b is now 9s old > lifetime 8s
	d.step()
	if got := d.Loop(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("expired app still in loop: %v", got)
	}
}

func TestNotificationsPreemptStackAndHold(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	d := newTestDisplay(c)
	d.Upsert("a", awtrix.App{Text: awtrix.Plain("A")})

	d.Notify(awtrix.Notification{App: awtrix.App{Text: awtrix.Plain("ONE")}})
	d.Notify(awtrix.Notification{App: awtrix.App{Text: awtrix.Plain("TWO")}, Hold: true})
	s := d.Snapshot()
	if s.Kind != "notify" || s.App.Text.String() != "ONE" || s.Queue != 1 {
		t.Fatalf("first notification: %+v", s)
	}
	c.add(6 * time.Second)
	d.step()
	if s := d.Snapshot(); s.App.Text.String() != "TWO" || !s.Hold {
		t.Fatalf("queued notification: %+v", s)
	}
	c.add(time.Hour)
	d.step()
	if s := d.Snapshot(); s.Kind != "notify" {
		t.Fatalf("hold notification should stay: %+v", s)
	}
	d.Dismiss()
	d.step()
	if s := d.Snapshot(); s.Kind != "app" || s.Name != "a" {
		t.Fatalf("after dismiss: %+v", s)
	}
}
