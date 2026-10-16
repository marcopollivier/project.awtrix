// Package api serves the hub's latest counter values to the scripts that run
// on the clock (scripts/counter.ax). It is optional: when the hub is down the
// scripts fetch the same data by themselves.
//
//	GET /api/v1/metrics       → {"now":1760650000,"metrics":{"github_followers":{"value":794,"updatedAt":1760649000},...}}
//	GET /api/v1/metrics/{key} → {"value":794,"age":60}
//
// The scripts read the per-key route: a tiny answer, and the age is computed
// here, so a skew between the hub's and the clock's time does not matter.
// Times are Unix seconds. The scripts keep their own daily delta and
// milestones, so only raw values are served.
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
	mux.HandleFunc("GET /api/v1/metrics/{key}", func(w http.ResponseWriter, r *http.Request) {
		var (
			c  state.Counter
			ok bool
		)
		st.View(func(d *state.Data) {
			if p := d.Counters[r.PathValue("key")]; p != nil {
				c, ok = *p, true
			}
		})
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"unknown metric"}`))
			return
		}
		age := max(0, int64(now().Sub(c.UpdatedAt).Seconds()))
		json.NewEncoder(w).Encode(map[string]int64{"value": c.Value, "age": age})
	})
	return mux
}
