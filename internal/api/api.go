// Package api serves the hub's latest counter values to the scripts that run
// on the clock (scripts/counter.ax). It is optional: when the hub is down the
// scripts fetch the same data by themselves.
//
//	GET /api/v1/metrics → {"now":1760650000,"metrics":{"github_followers":{"value":794,"updatedAt":1760649000},...}}
//
// Times are Unix seconds, easy to compare in Berry. The scripts keep their own
// daily delta and milestones, so only raw values are served.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/state"
)

type Metric struct {
	Value     int64 `json:"value"`
	UpdatedAt int64 `json:"updatedAt"`
}

type Metrics struct {
	Now     int64             `json:"now"`
	Metrics map[string]Metric `json:"metrics"`
}

func Handler(st *state.Store, now func() time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		out := Metrics{Now: now().Unix(), Metrics: map[string]Metric{}}
		st.View(func(d *state.Data) {
			for key, c := range d.Counters {
				out.Metrics[key] = Metric{Value: c.Value, UpdatedAt: c.UpdatedAt.Unix()}
			}
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
	return mux
}
