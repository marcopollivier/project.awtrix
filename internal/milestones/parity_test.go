package milestones

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// testdata/parity.json is shared with scripts/test/scripts_test.be: the bar
// and the parties of scripts/counter.ax must match Progress and Check.
func TestParity(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Progress []struct {
			Step  int64 `json:"step"`
			Value int64 `json:"value"`
			Pct   *int  `json:"pct"` // null: no bar
		} `json:"progress"`
		Milestones []struct {
			Step     int64   `json:"step"`
			Readings []int64 `json:"readings"`
			Party    []int64 `json:"party"` // milestone celebrated at each reading, 0 for none
		} `json:"milestones"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Progress {
		pct, ok := Rule{Step: c.Step}.Progress(c.Value)
		if ok != (c.Pct != nil) || (ok && pct != *c.Pct) {
			t.Errorf("step %d, Progress(%d) = %d %v, parity.json wants %v", c.Step, c.Value, pct, ok, c.Pct)
		}
	}
	for _, c := range p.Milestones {
		r := Rule{Step: c.Step}
		celebrated := int64(-1)
		got := make([]int64, len(c.Readings))
		for i, v := range c.Readings {
			var party bool
			if celebrated, party = r.Check(celebrated, v); party {
				got[i] = celebrated
			}
		}
		if !slices.Equal(got, c.Party) {
			t.Errorf("step %d, readings %v: parties %v, parity.json wants %v", c.Step, c.Readings, got, c.Party)
		}
	}
}
