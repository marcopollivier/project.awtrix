// Package instagram reads followers of a professional account through the
// Instagram Graph API. Use a Page access token derived from a long-lived user
// token: those don't expire (see README).
package instagram

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/sources"
)

type Source struct {
	cfg       config.Instagram
	icons     func(string) string
	BaseURL   string
	alertedAt time.Time
}

func New(cfg config.Instagram, icons func(string) string) *Source {
	return &Source{cfg: cfg, icons: icons, BaseURL: "https://graph.facebook.com/" + cfg.GraphVer}
}

func (s *Source) Name() string                 { return "instagram" }
func (s *Source) Next(time.Time) time.Duration { return s.cfg.Interval }

func (s *Source) Fetch(ctx context.Context, now time.Time) (sources.Output, error) {
	if s.cfg.Token == "" || s.cfg.UserID == "" {
		return sources.Output{}, errors.New("INSTAGRAM_TOKEN / INSTAGRAM_USER_ID not set")
	}
	var resp struct {
		Followers int64 `json:"followers_count"`
	}
	q := url.Values{"fields": {"followers_count"}, "access_token": {s.cfg.Token}}
	err := sources.GetJSON(ctx, s.BaseURL+"/"+s.cfg.UserID+"?"+q.Encode(), &resp)
	if he, ok := errors.AsType[*sources.HTTPError](err); ok && (he.Status == 400 || he.Status == 401) {
		// Expired/invalid token: warn on the clock at most once a day.
		if now.Sub(s.alertedAt) > 24*time.Hour {
			s.alertedAt = now
			return sources.Output{Notifications: []awtrix.Notification{{
				App: awtrix.App{Text: awtrix.Plain("TOKEN INSTAGRAM INVALIDO"), Icon: s.icons("instagram"), Color: "#FF3333"},
			}}}, nil
		}
		return sources.Output{}, err
	}
	if err != nil {
		return sources.Output{}, err
	}
	return sources.Output{Metrics: []sources.Metric{
		{Key: "instagram_followers", Label: "INSTAGRAM", Icon: s.icons("instagram"), Value: resp.Followers, Milestones: s.cfg.Milestones},
	}}, nil
}
