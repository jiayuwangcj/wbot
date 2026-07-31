package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// fakeClusterStore is a scriptable ClusterStore for unit tests.
type fakeClusterStore struct {
	runs      []ingest.RunStatus
	counts    ingest.RunCounts
	coverage  []ingest.BarCoverage
	pingErr   error
	runsErr   error
	countsErr error
	covErr    error

	gotLimit int
}

func (f *fakeClusterStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeClusterStore) RecentRuns(_ context.Context, limit int) ([]ingest.RunStatus, error) {
	f.gotLimit = limit
	if f.runsErr != nil {
		return nil, f.runsErr
	}
	return f.runs, nil
}

func (f *fakeClusterStore) RunStatusCounts(context.Context) (ingest.RunCounts, error) {
	if f.countsErr != nil {
		return ingest.RunCounts{}, f.countsErr
	}
	return f.counts, nil
}

func (f *fakeClusterStore) BarCoverage(context.Context) ([]ingest.BarCoverage, error) {
	if f.covErr != nil {
		return nil, f.covErr
	}
	return f.coverage, nil
}

func TestClusterComponents(t *testing.T) {
	meta := testMeta()
	minTs := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	maxTs := time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	finished := start.Add(time.Minute)
	store := &fakeClusterStore{
		runs: []ingest.RunStatus{
			{ID: 2, Source: "cli-mock", Status: "succeeded", StartedAt: start, FinishedAt: &finished},
			{ID: 1, Source: "cli-file", Status: "running", StartedAt: start.Add(-time.Hour), FinishedAt: nil},
		},
		counts: ingest.RunCounts{Running: 1, Succeeded: 9, Failed: 0},
		coverage: []ingest.BarCoverage{
			{Symbol: "DEMO.US", Timeframe: "1d", Count: 3, MinTs: minTs, MaxTs: maxTs},
		},
	}
	rec := get(t, ClusterHandler(meta, store), "/v1/admin/cluster")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if store.gotLimit != recentRunsLimit {
		t.Fatalf("recent runs limit = %d; want %d", store.gotLimit, recentRunsLimit)
	}
	var got struct {
		Components struct {
			Process struct {
				Version       string  `json:"version"`
				PID           int     `json:"pid"`
				StartedAt     string  `json:"started_at"`
				UptimeSeconds float64 `json:"uptime_seconds"`
				ListenAddr    string  `json:"listen_addr"`
			} `json:"process"`
			DB struct {
				OK        bool    `json:"ok"`
				LatencyMS float64 `json:"latency_ms"`
			} `json:"db"`
			Pipeline struct {
				Counts struct {
					Running   int64 `json:"running"`
					Succeeded int64 `json:"succeeded"`
					Failed    int64 `json:"failed"`
				} `json:"counts"`
				RecentRuns []map[string]any `json:"recent_runs"`
			} `json:"pipeline"`
			DataPlane struct {
				BarsCoverage []struct {
					Symbol    string `json:"symbol"`
					Timeframe string `json:"timeframe"`
					Count     int64  `json:"count"`
					MinTs     string `json:"min_ts"`
					MaxTs     string `json:"max_ts"`
				} `json:"bars_coverage"`
			} `json:"data_plane"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	p := got.Components.Process
	if p.Version != meta.Version || p.PID != os.Getpid() || p.ListenAddr != meta.ListenAddr {
		t.Fatalf("process = %+v; want injected meta (version=%s pid=%d listen=%s)", p, meta.Version, os.Getpid(), meta.ListenAddr)
	}
	if p.StartedAt != meta.StartedAt.Format(time.RFC3339) {
		t.Fatalf("started_at = %q; want %s", p.StartedAt, meta.StartedAt.Format(time.RFC3339))
	}
	if p.UptimeSeconds < 1.5 || p.UptimeSeconds > 3.5 {
		t.Fatalf("uptime_seconds = %v; want ~2", p.UptimeSeconds)
	}
	if !got.Components.DB.OK || got.Components.DB.LatencyMS <= 0 {
		t.Fatalf("db = %+v; want ok:true with latency", got.Components.DB)
	}
	c := got.Components.Pipeline.Counts
	if c.Running != 1 || c.Succeeded != 9 || c.Failed != 0 {
		t.Fatalf("counts = %+v; want running=1 succeeded=9 failed=0", c)
	}
	runs := got.Components.Pipeline.RecentRuns
	if len(runs) != 2 {
		t.Fatalf("recent_runs len = %d; want 2", len(runs))
	}
	if runs[0]["id"] != float64(2) || runs[0]["source"] != "cli-mock" || runs[0]["status"] != "succeeded" {
		t.Fatalf("recent_runs[0] = %v; want id 2 cli-mock succeeded", runs[0])
	}
	if runs[0]["started_at"] != start.Format(time.RFC3339) || runs[0]["finished_at"] != finished.Format(time.RFC3339) {
		t.Fatalf("recent_runs[0] times = %v; want %s..%s", runs[0], start.Format(time.RFC3339), finished.Format(time.RFC3339))
	}
	if runs[1]["finished_at"] != nil {
		t.Fatalf("recent_runs[1] finished_at = %v; want null", runs[1]["finished_at"])
	}
	cov := got.Components.DataPlane.BarsCoverage
	if len(cov) != 1 {
		t.Fatalf("bars_coverage len = %d; want 1", len(cov))
	}
	if cov[0].Symbol != "DEMO.US" || cov[0].Timeframe != "1d" || cov[0].Count != 3 {
		t.Fatalf("coverage[0] = %+v; want DEMO.US 1d count 3", cov[0])
	}
	if cov[0].MinTs != minTs.Format(time.RFC3339) || cov[0].MaxTs != maxTs.Format(time.RFC3339) {
		t.Fatalf("coverage[0] ts = %s..%s; want %s..%s", cov[0].MinTs, cov[0].MaxTs, minTs.Format(time.RFC3339), maxTs.Format(time.RFC3339))
	}
}

// TestClusterDBDown: ping failure reports db ok:false with 200 and must not mask other components.
func TestClusterDBDown(t *testing.T) {
	meta := testMeta()
	store := &fakeClusterStore{
		runs:     []ingest.RunStatus{{ID: 1, Source: "cli-mock", Status: "succeeded", StartedAt: time.Now().Add(-time.Hour)}},
		counts:   ingest.RunCounts{Succeeded: 1},
		coverage: []ingest.BarCoverage{{Symbol: "DEMO.US", Timeframe: "1d", Count: 1}},
		pingErr:  errors.New("connection refused"),
	}
	rec := get(t, ClusterHandler(meta, store), "/v1/admin/cluster")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	comp, ok := got["components"].(map[string]any)
	if !ok {
		t.Fatalf("components = %v; want object", got["components"])
	}
	db, ok := comp["db"].(map[string]any)
	if !ok || db["ok"] != false {
		t.Fatalf("db = %v; want ok:false", comp["db"])
	}
	if _, ok := db["latency_ms"]; ok {
		t.Fatalf("db = %v; want latency_ms omitted when down", comp["db"])
	}
	if _, ok := comp["pipeline"].(map[string]any); !ok {
		t.Fatalf("pipeline missing when db down: %v", comp)
	}
	if _, ok := comp["data_plane"].(map[string]any); !ok {
		t.Fatalf("data_plane missing when db down: %v", comp)
	}
}

func TestClusterQueryError(t *testing.T) {
	for name, store := range map[string]*fakeClusterStore{
		"runs":     {runsErr: errors.New("db down")},
		"counts":   {countsErr: errors.New("db down")},
		"coverage": {covErr: errors.New("db down")},
	} {
		rec := get(t, ClusterHandler(testMeta(), store), "/v1/admin/cluster")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s error: status = %d; want 500 (body %s)", name, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s error: body %q; want JSON error", name, rec.Body)
		}
	}
}

func TestClusterMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/cluster", nil)
	rec := httptest.NewRecorder()
	ClusterHandler(testMeta(), &fakeClusterStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestClusterUnknownPath(t *testing.T) {
	for _, path := range []string{"/v1/admin/cluster/", "/v1/admin/cluster/extra"} {
		rec := get(t, ClusterHandler(testMeta(), &fakeClusterStore{}), path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d; want 404 (body %s)", path, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s: body %q; want JSON error", path, rec.Body)
		}
	}
}

// TestClusterComposedHandler mirrors serve's wiring: exact cluster pattern beats the admin subtree.
func TestClusterComposedHandler(t *testing.T) {
	store := &fakeStore{
		runs:     []ingest.RunStatus{{ID: 1, Source: "cli-mock", Status: "succeeded", StartedAt: time.Now().Add(-time.Hour)}},
		counts:   ingest.RunCounts{Succeeded: 1},
		coverage: []ingest.BarCoverage{{Symbol: "DEMO.US", Timeframe: "1d", Count: 3}},
	}
	top := http.NewServeMux()
	top.Handle("/v1/admin/", AdminHandler(testMeta(), &fakePinger{}))
	top.Handle("/v1/admin/cluster", ClusterHandler(testMeta(), store))
	top.Handle("/", Handler(store))

	rec := get(t, top, "/v1/admin/cluster")
	if rec.Code != http.StatusOK {
		t.Fatalf("cluster = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	rec = get(t, top, "/v1/admin/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
}
