// Package linkedin shows a follower count typed in by hand
// (`awtrix-hub set linkedin 2890`): LinkedIn has no API for personal profiles.
package linkedin

import (
	"context"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/sources"
	"github.com/marcopollivier/project.awtrix/internal/state"
)

const Key = "linkedin"

type Source struct {
	cfg        config.LinkedIn
	icons      func(string) string
	manualPath string
}

func New(cfg config.LinkedIn, icons func(string) string, manualPath string) *Source {
	return &Source{cfg: cfg, icons: icons, manualPath: manualPath}
}

func (s *Source) Name() string { return "linkedin" }

// Next is short so a `set` shows up quickly; it only reads a local file.
func (s *Source) Next(time.Time) time.Duration { return time.Minute }

func (s *Source) Fetch(_ context.Context, now time.Time) (sources.Output, error) {
	m, err := state.ReadManual(s.manualPath)
	if err != nil {
		return sources.Output{}, err
	}
	v, ok := m[Key]
	if !ok {
		return sources.Output{}, nil
	}
	return sources.Output{Metrics: []sources.Metric{{
		Key: "linkedin_followers", Label: "LINKEDIN", Icon: s.icons("linkedin"), Value: v.Value,
		Milestones: s.cfg.Milestones,
		Stale:      s.cfg.StaleAfter > 0 && now.Sub(v.UpdatedAt) > s.cfg.StaleAfter,
	}}}, nil
}
