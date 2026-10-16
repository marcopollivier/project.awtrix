package render

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/parity.json is shared with scripts/test/scripts_test.be: the Berry
// count() in scripts/counter.ax must format exactly like Count.
func TestParity(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Count       [][2]any `json:"count"`
		Delta       [][2]any `json:"delta"`
		Divergences struct {
			Count []struct {
				Value int64  `json:"value"`
				Go    string `json:"go"`
			} `json:"count"`
		} `json:"known_divergences"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Count {
		if got := Count(int64(c[0].(float64))); got != c[1] {
			t.Errorf("Count(%v) = %q, parity.json wants %q", c[0], got, c[1])
		}
	}
	for _, c := range p.Delta {
		if got := Delta(int64(c[0].(float64))); got != c[1] {
			t.Errorf("Delta(%v) = %q, parity.json wants %q", c[0], got, c[1])
		}
	}
	// Documented divergences must stay accurate: once fixed, move them to "count".
	for _, d := range p.Divergences.Count {
		if got := Count(d.Value); got != d.Go {
			t.Errorf("known divergence Count(%d) = %q, parity.json says %q", d.Value, got, d.Go)
		}
	}
}
