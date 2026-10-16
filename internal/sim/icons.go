package sim

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// LaMetric icons, the same catalog AWTRIX downloads from.
const iconURL = "https://developer.lametric.com/content/apps/icon_thumbs/"

var (
	lametricID = regexp.MustCompile(`^[0-9]{1,7}$`)
	validID    = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`) // also custom icons, e.g. "gh_stars"
)

// Icon is decoded server-side so the browser just paints pixels.
type Icon struct {
	W      int     `json:"w"`
	H      int     `json:"h"`
	Frames []Frame `json:"frames"`
}

type Frame struct {
	DelayMs int      `json:"delay"`
	Pixels  []string `json:"pixels"` // "#rrggbb", "" = off
}

type Icons struct {
	dir    string // cache of LaMetric downloads
	custom string // project icons (<name>.gif), take precedence over LaMetric
	http   *http.Client
	mu     sync.Mutex
	mem    map[string]*Icon
}

func NewIcons(dir, custom string) *Icons {
	return &Icons{dir: dir, custom: custom, http: &http.Client{Timeout: 30 * time.Second}, mem: map[string]*Icon{}}
}

func (ic *Icons) Get(ctx context.Context, id string) (*Icon, error) {
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("invalid icon id %q", id)
	}
	ic.mu.Lock()
	cached := ic.mem[id]
	ic.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	raw, err := ic.Raw(ctx, id)
	if err != nil {
		return nil, err
	}
	icon, err := decode(raw)
	if err != nil {
		return nil, err
	}
	ic.mu.Lock()
	ic.mem[id] = icon
	ic.mu.Unlock()
	return icon, nil
}

// Raw returns the icon bytes (GIF or PNG): a project icon, the disk cache or LaMetric.
func (ic *Icons) Raw(ctx context.Context, id string) ([]byte, error) {
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("invalid icon id %q", id)
	}
	if ic.custom != "" {
		// id matched validID above, so it cannot leave the directory.
		if b, err := os.ReadFile(filepath.Join(ic.custom, id+".gif")); err == nil { //nolint:gosec // G703
			return b, nil
		}
	}
	if !lametricID.MatchString(id) {
		return nil, fmt.Errorf("icon %q: not in %s and not a LaMetric id", id, ic.custom)
	}
	path := filepath.Join(ic.dir, id)
	if b, err := os.ReadFile(path); err == nil { //nolint:gosec // G703: id matched lametricID
		return b, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL+id, nil) //nolint:gosec // G704: fixed host, numeric id
	if err != nil {
		return nil, err
	}
	resp, err := ic.http.Do(req) //nolint:gosec // G704: see above
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lametric: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(ic.dir, 0o755); err == nil { //nolint:gosec // G301: public icon cache
		_ = os.WriteFile(path, b, 0o644) //nolint:gosec // G306: public icon cache; a miss downloads again
	}
	return b, nil
}

func decode(b []byte) (*Icon, error) {
	if bytes.HasPrefix(b, []byte("GIF")) {
		g, err := gif.DecodeAll(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		// Frames may be partial; compose them onto a canvas like a browser would.
		bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
		canvas := image.NewRGBA(bounds)
		icon := &Icon{W: bounds.Dx(), H: bounds.Dy()}
		for i, fr := range g.Image {
			draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
			icon.Frames = append(icon.Frames, Frame{DelayMs: max(g.Delay[i]*10, 50), Pixels: pixels(canvas)})
			if i < len(g.Disposal) && g.Disposal[i] == gif.DisposalBackground {
				draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
			}
		}
		return icon, nil
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return &Icon{W: img.Bounds().Dx(), H: img.Bounds().Dy(), Frames: []Frame{{Pixels: pixels(img)}}}, nil
}

func pixels(img image.Image) []string {
	b := img.Bounds()
	out := make([]string, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A < 64 || (c.R|c.G|c.B) == 0 {
				out = append(out, "")
				continue
			}
			out = append(out, fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B))
		}
	}
	return out
}
