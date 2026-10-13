package awtrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// The clock does not download icons by itself: they must be in its /ICONS
// folder as GIF or JPG. LaMetric serves PNG for static icons, so those are
// converted to single-frame GIFs.

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
// ok is false when the target has no file manager (the simulator).
func (c *Client) Icons(ctx context.Context, target string) (names []string, ok bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(target, "/")+"/list?dir=/ICONS", nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	var files []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, false, err
	}
	for _, f := range files {
		if f.Type == "file" {
			names = append(names, f.Name)
		}
	}
	return names, true, nil
}

// UploadIcon stores a GIF as /ICONS/<id>.gif through the clock's file manager.
func (c *Client) UploadIcon(ctx context.Context, target, id string, gifData []byte) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("data", "/ICONS/"+id+".gif")
	if err != nil {
		return err
	}
	fw.Write(gifData)
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(target, "/")+"/edit", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload %s: %s", id, resp.Status)
	}
	return nil
}

// Targets returns the configured target URLs.
func (c *Client) Targets() []string { return c.targets }
