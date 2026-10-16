package awtrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	_ "image/png"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Device-only endpoints (files, device state, scripts) that the simulator does not have.
//
// The clock does not download icons by itself: they must be in its /ICONS
// folder as GIF or JPEG (not PNG). LaMetric serves PNG for static icons, so
// those are converted to single-frame GIFs.

// IconGIF returns raw as a GIF (unchanged if it already is one).
func IconGIF(raw []byte) ([]byte, error) {
	if bytes.HasPrefix(raw, []byte("GIF")) {
		return raw, nil
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	pal := image.NewPaletted(img.Bounds(), palette.Plan9)
	draw.Draw(pal, img.Bounds(), img, img.Bounds().Min, draw.Src)
	var buf bytes.Buffer
	if err := gif.Encode(&buf, pal, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Icons lists the icon files on one target (e.g. "2799.gif").
// ok is false when the target has no file storage (the simulator).
func (c *Client) Icons(ctx context.Context, target string) (names []string, ok bool, err error) {
	b, err := c.do(ctx, http.MethodGet, strings.TrimRight(target, "/")+"/api/v1/files?dir=/ICONS", "", nil)
	if se, isStatus := errors.AsType[*StatusError](err); isStatus && se.Status == http.StatusNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var list struct {
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, false, err
	}
	for _, f := range list.Files {
		names = append(names, path.Base(f.Name))
	}
	return names, true, nil
}

// UploadIcon stores a GIF as /ICONS/<id>.gif.
func (c *Client) UploadIcon(ctx context.Context, target, id string, gifData []byte) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", id+".gif")
	if err != nil {
		return err
	}
	fw.Write(gifData)
	mw.Close()
	_, err = c.do(ctx, http.MethodPost, strings.TrimRight(target, "/")+"/api/v1/files?dir=/ICONS", mw.FormDataContentType(), &body)
	if err != nil {
		return fmt.Errorf("upload %s: %w", id, err)
	}
	return nil
}

// Targets returns the configured target URLs.
func (c *Client) Targets() []string { return c.targets }

// Uptime returns the target's uptime in seconds. ok is false when the target
// does not report one (the simulator).
func (c *Client) Uptime(ctx context.Context, target string) (secs int64, ok bool, err error) {
	b, err := c.do(ctx, http.MethodGet, strings.TrimRight(target, "/")+"/api/v1/device", "", nil)
	if err != nil {
		return 0, false, err
	}
	var st struct {
		Uptime *int64 `json:"uptimeSeconds"`
	}
	if err := json.Unmarshal(b, &st); err != nil || st.Uptime == nil {
		return 0, false, err
	}
	return *st.Uptime, true, nil
}

// --- scripts (Berry apps that run on the clock) ---

// PutScript installs or replaces a script; the body is Berry source.
func (c *Client) PutScript(ctx context.Context, target, name string, src []byte) error {
	_, err := c.do(ctx, http.MethodPut, strings.TrimRight(target, "/")+"/api/v1/apps/script/"+url.PathEscape(name), "text/plain", bytes.NewReader(src))
	return err
}

// PatchAppConfig changes a script's @config settings (the script restarts).
func (c *Client) PatchAppConfig(ctx context.Context, target, name string, values map[string]any) error {
	body, err := json.Marshal(values)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPatch, strings.TrimRight(target, "/")+"/api/v1/apps/"+url.PathEscape(name)+"/config", "application/json", bytes.NewReader(body))
	return err
}

// AppInfo is one entry of GET /api/v1/apps.
type AppInfo struct {
	Name    string    `json:"name"`
	Origin  string    `json:"origin"` // builtin | pushed | script
	Enabled bool      `json:"enabled"`
	InLoop  bool      `json:"inLoop"`
	Error   *AppError `json:"error"`
}

// AppError is a script's compile or runtime error.
type AppError struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Hook    string `json:"hook"` // compile, setup, loop, draw...
}

// Apps lists every app on the clock, with script compile/runtime errors.
func (c *Client) Apps(ctx context.Context, target string) ([]AppInfo, error) {
	b, err := c.do(ctx, http.MethodGet, strings.TrimRight(target, "/")+"/api/v1/apps", "", nil)
	if err != nil {
		return nil, err
	}
	// The list comes either bare or wrapped in {"apps": [...]}.
	var apps []AppInfo
	if err := json.Unmarshal(b, &apps); err == nil {
		return apps, nil
	}
	var wrapped struct {
		Apps []AppInfo `json:"apps"`
	}
	if err := json.Unmarshal(b, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Apps, nil
}
