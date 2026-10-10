// Package youtube reads channel subscribers (YouTube Data API v3) and, for a
// while after publishing, the views of the latest video.
//
// The public API rounds subscriber counts to 3 significant digits.
package youtube

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
	"github.com/marcopollivier/project.awtrix/internal/render"
	"github.com/marcopollivier/project.awtrix/internal/sources"
)

const LatestApp = "youtube_latest"

type Source struct {
	cfg     config.YouTube
	icons   func(string) string
	BaseURL string
}

func New(cfg config.YouTube, icons func(string) string) *Source {
	return &Source{cfg: cfg, icons: icons, BaseURL: "https://www.googleapis.com/youtube/v3"}
}

func (s *Source) Name() string                 { return "youtube" }
func (s *Source) Next(time.Time) time.Duration { return s.cfg.Interval }

func (s *Source) Fetch(ctx context.Context, now time.Time) (sources.Output, error) {
	if s.cfg.APIKey == "" {
		return sources.Output{}, errors.New("YOUTUBE_API_KEY not set")
	}
	var ch struct {
		Items []struct {
			Statistics struct {
				Subscribers string `json:"subscriberCount"`
			} `json:"statistics"`
			ContentDetails struct {
				Related struct {
					Uploads string `json:"uploads"`
				} `json:"relatedPlaylists"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if err := s.get(ctx, "channels", url.Values{"part": {"statistics,contentDetails"}, "forHandle": {s.cfg.Handle}}, &ch); err != nil {
		return sources.Output{}, err
	}
	if len(ch.Items) == 0 {
		return sources.Output{}, errors.New("channel not found: " + s.cfg.Handle)
	}
	subs, _ := strconv.ParseInt(ch.Items[0].Statistics.Subscribers, 10, 64)
	out := sources.Output{
		Metrics: []sources.Metric{{Key: "youtube_subs", Label: "YOUTUBE", Icon: s.icons("youtube"), Value: subs, Milestones: s.cfg.Milestones}},
		Apps:    map[string]*awtrix.App{LatestApp: nil},
	}

	// Latest video: playlistItems costs 1 quota unit (search would cost 100).
	var pl struct {
		Items []struct {
			ContentDetails struct {
				VideoID     string    `json:"videoId"`
				PublishedAt time.Time `json:"videoPublishedAt"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	uploads := ch.Items[0].ContentDetails.Related.Uploads
	if err := s.get(ctx, "playlistItems", url.Values{"part": {"contentDetails"}, "playlistId": {uploads}, "maxResults": {"1"}}, &pl); err != nil || len(pl.Items) == 0 {
		return out, nil // subscribers are still worth showing
	}
	latest := pl.Items[0].ContentDetails
	if now.Sub(latest.PublishedAt) > s.cfg.LatestWindow {
		return out, nil
	}
	var vids struct {
		Items []struct {
			Statistics struct {
				Views string `json:"viewCount"`
			} `json:"statistics"`
		} `json:"items"`
	}
	if err := s.get(ctx, "videos", url.Values{"part": {"statistics"}, "id": {latest.VideoID}}, &vids); err != nil || len(vids.Items) == 0 {
		return out, nil
	}
	views, _ := strconv.ParseInt(vids.Items[0].Statistics.Views, 10, 64)
	out.Apps[LatestApp] = &awtrix.App{
		Text: awtrix.Colored(awtrix.Frag("NEW ", "#FF0000"), awtrix.Frag(render.Count(views), "#FFFFFF")),
		Icon: s.icons("youtube"),
	}
	return out, nil
}

func (s *Source) get(ctx context.Context, endpoint string, q url.Values, v any) error {
	q.Set("key", s.cfg.APIKey)
	return sources.GetJSON(ctx, s.BaseURL+"/"+endpoint+"?"+q.Encode(), v)
}
