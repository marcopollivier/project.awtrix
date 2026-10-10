package milestones

import "testing"

func TestStep(t *testing.T) {
	r := Rule{Step: 100}
	if got := r.Reached(793); got != 700 {
		t.Fatalf("reached = %d", got)
	}
	if got := r.Next(793); got != 800 {
		t.Fatalf("next = %d", got)
	}
	if pct, ok := r.Progress(793); !ok || pct != 93 {
		t.Fatalf("progress = %d %v", pct, ok)
	}
}

func TestListAndStepCombined(t *testing.T) {
	r := Rule{Step: 1000, List: []int64{15000, 12500}}
	if got := r.Next(12345); got != 12500 {
		t.Fatalf("next = %d", got)
	}
	if got := r.Reached(12600); got != 12500 {
		t.Fatalf("reached = %d", got)
	}
}

func TestListOnlyExhausted(t *testing.T) {
	r := Rule{List: []int64{1000}}
	if _, ok := r.Progress(2000); ok {
		t.Fatal("expected no next milestone")
	}
}

func TestCheckCelebratesOnce(t *testing.T) {
	r := Rule{Step: 100}

	c, party := r.Check(-1, 793) // first reading: baseline only
	if party || c != 700 {
		t.Fatalf("baseline: %d %v", c, party)
	}
	c, party = r.Check(c, 801)
	if !party || c != 800 {
		t.Fatalf("cross: %d %v", c, party)
	}
	c, party = r.Check(c, 799) // drop
	if party || c != 800 {
		t.Fatalf("drop: %d %v", c, party)
	}
	c, party = r.Check(c, 805) // back over the same milestone
	if party || c != 800 {
		t.Fatalf("re-cross: %d %v", c, party)
	}
}
