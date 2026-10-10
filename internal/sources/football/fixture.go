package football

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var dateField = regexp.MustCompile(`"date"\s*:\s*"[^"]*"`)

// FixtureServer replays a recorded match so goals can be tested without a real game.
// dir must contain team.json (the teams/{id} response) and summary_*.json files,
// served in name order, one per request; the last one repeats.
// A non-zero kickoff replaces the match date so the replay starts right away.
func FixtureServer(dir string, kickoff time.Time) (*httptest.Server, error) {
	summaries, err := filepath.Glob(filepath.Join(dir, "summary_*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(summaries)
	var (
		mu sync.Mutex
		i  int
	)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/teams/"):
			b, err := os.ReadFile(filepath.Join(dir, "team.json"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !kickoff.IsZero() {
				b = dateField.ReplaceAll(b, []byte(`"date":"`+kickoff.UTC().Format("2006-01-02T15:04Z")+`"`))
			}
			w.Write(b)
		case strings.HasSuffix(r.URL.Path, "/summary") && len(summaries) > 0:
			mu.Lock()
			f := summaries[min(i, len(summaries)-1)]
			i++
			mu.Unlock()
			http.ServeFile(w, r, f)
		default:
			http.NotFound(w, r)
		}
	})), nil
}
