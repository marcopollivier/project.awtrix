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

// Client sends payloads to one or more AWTRIX NG-compatible targets
// (the real clock and/or the web simulator).
type Client struct {
	targets    []string
	http       *http.Client
	log        *slog.Logger
	user, pass string
}

func NewClient(targets []string, log *slog.Logger) *Client {
	ts := make([]string, len(targets))
	for i, t := range targets {
		ts[i] = strings.TrimRight(t, "/")
	}
	return &Client{targets: ts, http: &http.Client{Timeout: 5 * time.Second}, log: log}
}

// WithAuth sets the HTTP Basic credentials the clock asks for when authEnabled
// is on (the simulator ignores them).
func (c *Client) WithAuth(user, pass string) *Client {
	c.user, c.pass = user, pass
	return c
}

// Custom creates or replaces a pushed app. A nil app removes it.
func (c *Client) Custom(ctx context.Context, name string, app *App) error {
	path := "/api/v1/apps/pushed/" + url.PathEscape(name)
	if app == nil {
		return c.broadcast(ctx, http.MethodDelete, "/api/v1/apps/"+url.PathEscape(name), nil)
	}
	body, err := json.Marshal(app)
	if err != nil {
		return err
	}
	return c.broadcast(ctx, http.MethodPut, path, body)
}

func (c *Client) Notify(ctx context.Context, n Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return c.broadcast(ctx, http.MethodPost, "/api/v1/notifications", body)
}

// broadcast sends to every target concurrently; a failing target does not block the others.
func (c *Client) broadcast(ctx context.Context, method, path string, body []byte) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, t := range c.targets {
		wg.Go(func() {
			if err := c.send(ctx, method, t+path, body); err != nil {
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

func (c *Client) send(ctx context.Context, method, u string, body []byte) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	_, err := c.do(ctx, method, u, "application/json", r)
	return err
}

// do runs one request with auth and returns the body of a 2xx answer.
// A DELETE of something already gone (404) counts as success.
func (c *Client) do(ctx context.Context, method, u, contentType string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
		return b, nil
	}
	if resp.StatusCode >= 300 {
		return b, &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return b, nil
}

// StatusError is a non-2xx answer; Body carries NG's {"error":{...}} message.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("status %d", e.Status)
	}
	return fmt.Sprintf("status %d: %s", e.Status, e.Body)
}
