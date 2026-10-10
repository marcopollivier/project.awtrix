package awtrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client sends payloads to one or more AWTRIX-compatible targets
// (the real clock and/or the web simulator).
type Client struct {
	targets []string
	http    *http.Client
	log     *slog.Logger
}

func NewClient(targets []string, log *slog.Logger) *Client {
	ts := make([]string, len(targets))
	for i, t := range targets {
		ts[i] = strings.TrimRight(t, "/")
	}
	return &Client{targets: ts, http: &http.Client{Timeout: 5 * time.Second}, log: log}
}

// Custom creates or updates an app. A nil app removes it.
func (c *Client) Custom(ctx context.Context, name string, app *App) error {
	var body []byte
	if app != nil {
		var err error
		if body, err = json.Marshal(app); err != nil {
			return err
		}
	}
	return c.post(ctx, "/api/custom?name="+url.QueryEscape(name), body)
}

func (c *Client) Notify(ctx context.Context, n Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return c.post(ctx, "/api/notify", body)
}

// post sends to every target concurrently; a failing target does not block the others.
func (c *Client) post(ctx context.Context, path string, body []byte) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, t := range c.targets {
		wg.Go(func() {
			if err := c.send(ctx, t+path, body); err != nil {
				c.log.Warn("awtrix request failed", "target", t, "path", path, "err", err)
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", t, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (c *Client) send(ctx context.Context, u string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %s", resp.Status)
	}
	return nil
}
