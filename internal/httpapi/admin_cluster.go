package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

const (
	clusterPath     = "/v1/admin/cluster"
	recentRunsLimit = 5 // recent_runs depth of the pipeline component
)

// ClusterStore is the narrow surface GET /v1/admin/cluster needs; it reuses Pinger (⑥-A)
// unchanged (convergence decision: P2/PR #34 merged; no duplicate ping interface, PR #35).
type ClusterStore interface {
	Pinger
	RecentRuns(ctx context.Context, limit int) ([]ingest.RunStatus, error)
	RunStatusCounts(ctx context.Context) (ingest.RunCounts, error)
	BarCoverage(ctx context.Context) ([]ingest.BarCoverage, error)
}

// processJSON mirrors the GET /v1/admin/status process fields (⑥-A).
type processJSON struct {
	Version       string  `json:"version"`
	PID           int     `json:"pid"`
	StartedAt     string  `json:"started_at"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	ListenAddr    string  `json:"listen_addr"`
}

// clusterJSON is the GET /v1/admin/cluster body: a single-process component view.
type clusterJSON struct {
	Components componentsJSON `json:"components"`
}

type componentsJSON struct {
	Process   processJSON   `json:"process"`
	DB        dbStatusJSON  `json:"db"`
	Pipeline  pipelineJSON  `json:"pipeline"`
	DataPlane dataPlaneJSON `json:"data_plane"`
}

// pipelineJSON aggregates ingestion runs: status counts plus the most recent runs.
type pipelineJSON struct {
	Counts     runCountsJSON `json:"counts"`
	RecentRuns []runJSON     `json:"recent_runs"`
}

type runCountsJSON struct {
	Running   int64 `json:"running"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}

// dataPlaneJSON describes bars coverage per symbol×timeframe.
type dataPlaneJSON struct {
	BarsCoverage []barCoverageJSON `json:"bars_coverage"`
}

type barCoverageJSON struct {
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	Count     int64  `json:"count"`
	MinTs     string `json:"min_ts"`
	MaxTs     string `json:"max_ts"`
}

// ClusterHandler returns an http.Handler serving GET /v1/admin/cluster: a single-process
// component view (process/db/pipeline/data plane); wbot runs no real cluster to report.
func ClusterHandler(meta ProcessMeta, store ClusterStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(clusterPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		out := clusterJSON{Components: componentsJSON{
			Process: processJSON{
				Version:       meta.Version,
				PID:           os.Getpid(),
				StartedAt:     meta.StartedAt.Format(time.RFC3339),
				UptimeSeconds: time.Since(meta.StartedAt).Seconds(),
				ListenAddr:    meta.ListenAddr,
			},
			DB: dbStatusJSON{OK: true},
		}}
		pctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()
		start := time.Now()
		if err := store.Ping(pctx); err == nil && pctx.Err() == nil {
			out.Components.DB.LatencyMS = time.Since(start).Seconds() * 1000
		} else {
			out.Components.DB = dbStatusJSON{OK: false}
			if err == nil {
				err = pctx.Err()
			}
			fmt.Fprintf(os.Stderr, "httpapi: admin: db ping failed: %v\n", err)
		}

		runs, err := store.RecentRuns(r.Context(), recentRunsLimit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		counts, err := store.RunStatusCounts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		coverage, err := store.BarCoverage(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		out.Components.Pipeline.Counts = runCountsJSON{Running: counts.Running, Succeeded: counts.Succeeded, Failed: counts.Failed}
		out.Components.Pipeline.RecentRuns = toRunJSON(runs)
		out.Components.DataPlane.BarsCoverage = make([]barCoverageJSON, 0, len(coverage))
		for _, c := range coverage {
			out.Components.DataPlane.BarsCoverage = append(out.Components.DataPlane.BarsCoverage, barCoverageJSON{
				Symbol: c.Symbol, Timeframe: c.Timeframe, Count: c.Count,
				MinTs: c.MinTs.Format(time.RFC3339), MaxTs: c.MaxTs.Format(time.RFC3339),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	// Any other path under the cluster handler: JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return mux
}
