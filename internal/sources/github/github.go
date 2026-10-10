// Package github reads followers and total stars of a user's own repositories.
package github

import (
	"context"
	"fmt"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/sources"
)

type Source struct {
	cfg     config.GitHub
	icons   func(string) string
	BaseURL string
}

func New(cfg config.GitHub, icons func(string) string) *Source {
	return &Source{cfg: cfg, icons: icons, BaseURL: "https://api.github.com"}
}

func (s *Source) Name() string                 { return "github" }
func (s *Source) Next(time.Time) time.Duration { return s.cfg.Interval }

func (s *Source) Fetch(ctx context.Context, _ time.Time) (sources.Output, error) {
	var user struct {
		Followers int64 `json:"followers"`
	}
	if err := s.get(ctx, "/users/"+s.cfg.User, &user); err != nil {
		return sources.Output{}, err
	}
	var stars int64
	for page := 1; ; page++ {
		var repos []struct {
			Stars int64 `json:"stargazers_count"`
			Fork  bool  `json:"fork"`
		}
		path := fmt.Sprintf("/users/%s/repos?type=owner&per_page=100&page=%d", s.cfg.User, page)
		if err := s.get(ctx, path, &repos); err != nil {
			return sources.Output{}, err
		}
		for _, r := range repos {
			if !r.Fork {
				stars += r.Stars
			}
		}
		if len(repos) < 100 {
			break
		}
	}
	return sources.Output{Metrics: []sources.Metric{
		{Key: "github_followers", Label: "GITHUB", Icon: s.icons("github"), Value: user.Followers, Milestones: s.cfg.Milestones},
		{Key: "github_stars", Label: "STARS", Icon: s.icons("stars"), Value: stars, Milestones: s.cfg.Stars},
	}}, nil
}

func (s *Source) get(ctx context.Context, path string, v any) error {
	h := []string{"Accept", "application/vnd.github+json"}
	if s.cfg.Token != "" {
		h = append(h, "Authorization", "Bearer "+s.cfg.Token)
	}
	return sources.GetJSON(ctx, s.BaseURL+path, v, h...)
}
