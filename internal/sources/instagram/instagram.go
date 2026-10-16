// Package instagram reads followers of a professional account through the
// Instagram API with Instagram Login (graph.instagram.com, no Facebook Page).
//
// Long-lived tokens last 60 days and can be refreshed once they are 24h old.
// The source refreshes weekly and keeps the current token in tokenPath, so the
// token from .env only seeds it (a different one in .env replaces it).
package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/sources"
)

const refreshEvery = 7 * 24 * time.Hour

type Source struct {
	cfg       config.Instagram
	icons     func(string) string
	tokenPath string
	BaseURL   string // https://graph.instagram.com
	alertedAt time.Time
}

func New(cfg config.Instagram, icons func(string) string, tokenPath string) *Source {
	return &Source{cfg: cfg, icons: icons, tokenPath: tokenPath, BaseURL: "https://graph.instagram.com"}
}

func (s *Source) Name() string                 { return "instagram" }
func (s *Source) Next(time.Time) time.Duration { return s.cfg.Interval }

type tokenFile struct {
	Token       string    `json:"token"`
	Seed        string    `json:"seed"` // the .env token this one descends from
	RefreshedAt time.Time `json:"refreshed_at"`
}

func (s *Source) Fetch(ctx context.Context, now time.Time) (sources.Output, error) {
	if s.cfg.Token == "" {
		return sources.Output{}, errors.New("INSTAGRAM_TOKEN not set")
	}
	tok := s.token(ctx, now)

	var resp struct {
		Followers *int64 `json:"followers_count"`
		Data      []struct {
			Followers *int64 `json:"followers_count"`
		} `json:"data"`
	}
	q := url.Values{"fields": {"followers_count"}, "access_token": {tok}}
	err := sources.GetJSON(ctx, s.BaseURL+"/"+s.cfg.GraphVer+"/me?"+q.Encode(), &resp)
	if he, ok := errors.AsType[*sources.HTTPError](err); ok && (he.Status == 400 || he.Status == 401) {
		// Expired/invalid token: warn on the clock at most once a day.
		if now.Sub(s.alertedAt) > 24*time.Hour {
			s.alertedAt = now
			return sources.Output{Notifications: []awtrix.Notification{{
				App: awtrix.App{Text: awtrix.Plain("TOKEN INSTAGRAM INVALIDO"), Icon: s.icons("instagram"), TextColor: "#FF3333"},
			}}}, nil
		}
		return sources.Output{}, err
	}
	if err != nil {
		return sources.Output{}, err
	}
	followers := resp.Followers
	if followers == nil && len(resp.Data) > 0 {
		followers = resp.Data[0].Followers
	}
	if followers == nil {
		return sources.Output{}, errors.New("followers_count missing (is the account professional?)")
	}
	return sources.Output{Metrics: []sources.Metric{
		{Key: "instagram_followers", Label: "INSTAGRAM", Icon: s.icons("instagram"), Value: *followers, Milestones: s.cfg.Milestones},
	}}, nil
}

// token returns the current token, refreshing it when it is a week old.
// Refresh failures are not fatal: the current token keeps working until it expires.
func (s *Source) token(ctx context.Context, now time.Time) string {
	tf, _ := s.load()
	if tf.Token == "" || tf.Seed != s.cfg.Token {
		tf = tokenFile{Token: s.cfg.Token, Seed: s.cfg.Token, RefreshedAt: now}
		s.save(tf)
	}
	if now.Sub(tf.RefreshedAt) < refreshEvery {
		return tf.Token
	}
	var r struct {
		AccessToken string `json:"access_token"`
	}
	q := url.Values{"grant_type": {"ig_refresh_token"}, "access_token": {tf.Token}}
	if err := sources.GetJSON(ctx, s.BaseURL+"/refresh_access_token?"+q.Encode(), &r); err == nil && r.AccessToken != "" {
		tf.Token, tf.RefreshedAt = r.AccessToken, now
		s.save(tf)
	}
	return tf.Token
}

func (s *Source) load() (tokenFile, error) {
	var tf tokenFile
	b, err := os.ReadFile(s.tokenPath)
	if err != nil {
		return tf, err
	}
	return tf, json.Unmarshal(b, &tf)
}

func (s *Source) save(tf tokenFile) {
	b, _ := json.MarshalIndent(tf, "", "  ")
	os.MkdirAll(filepath.Dir(s.tokenPath), 0o700)
	os.WriteFile(s.tokenPath, b, 0o600)
}
