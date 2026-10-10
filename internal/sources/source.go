// Package sources defines what a data source produces. Counter-like sources
// return Metrics (the scheduler handles deltas, milestones and rendering);
// richer sources (football) return ready-made apps and notifications.
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
)

type Metric struct {
	Key        string // app name and state key, e.g. "github_followers"
	Label      string // used in celebrations, e.g. "GITHUB"
	Icon       string
	Value      int64
	Milestones config.Milestones
	Stale      bool // value is old (shown dimmed)
}

type Output struct {
	Metrics       []Metric
	Apps          map[string]*awtrix.App // a nil app removes it from the clock
	Notifications []awtrix.Notification
}

type Source interface {
	Name() string
	Fetch(ctx context.Context, now time.Time) (Output, error)
	// Next is how long to wait before the next Fetch (may depend on what was fetched).
	Next(now time.Time) time.Duration
}

// HTTPClient is shared by sources; tests point base URLs at httptest servers.
var HTTPClient = &http.Client{Timeout: 15 * time.Second}

// GetJSON fetches url into v, adding optional headers.
func GetJSON(ctx context.Context, url string, v any, headers ...string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &HTTPError{Status: resp.StatusCode, Body: string(b)}
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Body) }
