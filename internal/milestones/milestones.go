// Package milestones decides when a counter reached a goal worth celebrating
// and how far it is from the next one.
package milestones

import (
	"slices"

	"github.com/marcopollivier/project.awtrix/internal/config"
)

type Rule config.Milestones

func (r Rule) Enabled() bool { return r.Step > 0 || len(r.List) > 0 }

// Reached returns the highest milestone <= v (0 if none).
func (r Rule) Reached(v int64) int64 {
	var best int64
	if r.Step > 0 {
		best = v / r.Step * r.Step
	}
	for _, m := range r.List {
		if m <= v && m > best {
			best = m
		}
	}
	return best
}

// Next returns the smallest milestone > v (0 if none).
func (r Rule) Next(v int64) int64 {
	var next int64
	if r.Step > 0 {
		next = (v/r.Step + 1) * r.Step
	}
	list := slices.Clone(r.List)
	slices.Sort(list)
	for _, m := range list {
		if m > v {
			if next == 0 || m < next {
				next = m
			}
			break
		}
	}
	return next
}

// Progress is how far v is between the last reached milestone and the next one (0-100).
// ok is false when there is no next milestone.
func (r Rule) Progress(v int64) (pct int, ok bool) {
	next := r.Next(v)
	if next == 0 {
		return 0, false
	}
	prev := r.Reached(v)
	return int((v - prev) * 100 / (next - prev)), true
}

// Check returns the milestone to celebrate given the highest one already celebrated.
// A first reading (celebrated < 0) only records the baseline, so existing counts
// don't trigger a party when the hub starts for the first time.
func (r Rule) Check(celebrated, v int64) (newCelebrated int64, celebrate bool) {
	reached := r.Reached(v)
	if celebrated < 0 {
		return reached, false
	}
	if reached > celebrated {
		return reached, true
	}
	return celebrated, false
}
