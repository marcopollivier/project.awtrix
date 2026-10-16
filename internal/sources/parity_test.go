package sources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The hub serves each metric at /api/v1/metrics/{key} and the scripts ask for
// it by the key they pass to counter.Counter, so both sides and
// testdata/parity.json must name the same keys.
func TestMetricKeysMatchScripts(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	goKeys := grep(t, "*/*.go", `Key:\s*"([a-z_]+)"`)
	berryKeys := grep(t, "../../scripts/*.ax", `super\(self\)\.init\(store,\s*counter\.cfg,\s*"([a-z_]+)"`)
	want := slices.Sorted(slices.Values(p.Keys))
	if !slices.Equal(goKeys, want) {
		t.Errorf("internal/sources keys %v, parity.json has %v", goKeys, want)
	}
	if !slices.Equal(berryKeys, want) {
		t.Errorf("scripts/*.ax keys %v, parity.json has %v", berryKeys, want)
	}
}

// grep returns the sorted, unique first groups of re in the files matching glob.
func grep(t *testing.T, glob, re string) []string {
	t.Helper()
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	rx := regexp.MustCompile(re)
	var keys []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range rx.FindAllSubmatch(src, -1) {
			keys = append(keys, string(m[1]))
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}
