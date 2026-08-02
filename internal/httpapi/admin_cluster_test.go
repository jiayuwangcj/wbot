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
	opts      []ingest.OptionFreshness
	pingErr   error
	runsErr   error
	countsErr error
	covErr    error
	optsErr   error

	queryCalls int
	gotLimit   int
}

func (f *fakeClusterStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeClusterStore) RecentRuns(_ context.Context, limit int) ([]ingest.RunStatus, error) {
	f.queryCalls++
	f.gotLimit = limit
	if f.runsErr != nil {
		return nil, f.runsErr
	}
	return f.runs, nil
}

func (f *fakeClusterStore) RunStatusCounts(context.Context) (ingest.RunCounts, error) {
	f.queryCalls++
	if f.countsErr != nil {
		return ingest.RunCounts{}, f.countsErr
	}
	return f.counts, nil
}

func (f *fakeClusterStore) BarCoverage(context.Context) ([]ingest.BarCoverage, error) {
	f.queryCalls++
	if f.covErr != nil {
		return nil, f.covErr
	}
	return f.coverage, nil
}

func (f *fakeClusterStore) OptionFreshness(context.Context) ([]ingest.OptionFreshness, error) {
	f.queryCalls++
	if f.optsErr != nil {
		return nil, f.optsErr
	}
	return f.opts, nil
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
		opts: []ingest.OptionFreshness{
			{Underlying: "HK.00700", Source: "futu", MaxTs: maxTs, AgeSeconds: 3600},
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
				OptionsFreshness []struct {
					Underlying string `json:"underlying"`
					Source     string `json:"source"`
					MaxTs      string `json:"max_ts"`
				} `json:"options_freshness"`
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
	of := got.Components.DataPlane.OptionsFreshness
	if len(of) != 1 {
		t.Fatalf("options_freshness len = %d; want 1", len(of))
	}
	if of[0].Underlying != "HK.00700" || of[0].Source != "futu" || of[0].MaxTs != maxTs.Format(time.RFC3339) {
		t.Fatalf("options_freshness[0] = %+v; want HK.00700 futu with injected max_ts", of[0])
	}
}

// TestClusterFreshnessFields: bars_coverage entries carry max_ts_age_seconds and
// fresh (fresh/stale by the per-timeframe default threshold), and the pre-existing
// fields are unchanged (backward compatibility).
func TestClusterFreshnessFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	freshTs := now.Add(-2 * time.Hour)   // 1d default threshold 72h → fresh
	staleTs := now.Add(-100 * time.Hour) // 1d default threshold 72h → stale
	store := &fakeClusterStore{
		coverage: []ingest.BarCoverage{
			{Symbol: "FRESH.US", Timeframe: "1d", Adjust: "none", Count: 3, MinTs: freshTs.Add(-48 * time.Hour), MaxTs: freshTs},
			{Symbol: "STALE.US", Timeframe: "1d", Adjust: "none", Count: 5, MinTs: staleTs.Add(-48 * time.Hour), MaxTs: staleTs},
		},
	}
	rec := get(t, ClusterHandler(testMeta(), store), "/v1/admin/cluster")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got struct {
		Components struct {
			DataPlane struct {
				BarsCoverage []struct {
					Symbol          string `json:"symbol"`
					Timeframe       string `json:"timeframe"`
					Count           int64  `json:"count"`
					MinTs           string `json:"min_ts"`
					MaxTs           string `json:"max_ts"`
					MaxTsAgeSeconds int64  `json:"max_ts_age_seconds"`
					Fresh           string `json:"fresh"`
				} `json:"bars_coverage"`
			} `json:"data_plane"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	cov := got.Components.DataPlane.BarsCoverage
	if len(cov) != 2 {
		t.Fatalf("bars_coverage len = %d; want 2", len(cov))
	}
	fresh, stale := cov[0], cov[1]
	// New fields: fresh entry age ≈ 7200s and status fresh; stale entry status stale.
	if fresh.Fresh != "fresh" || fresh.MaxTsAgeSeconds < 7199 || fresh.MaxTsAgeSeconds > 7201 {
		t.Fatalf("fresh entry = %+v; want fresh with max_ts_age_seconds ≈ 7200", fresh)
	}
	if stale.Fresh != "stale" || stale.MaxTsAgeSeconds < 359999 || stale.MaxTsAgeSeconds > 360001 {
		t.Fatalf("stale entry = %+v; want stale with max_ts_age_seconds ≈ 360000", stale)
	}
	// Backward compatibility: old fields keep their values.
	if fresh.Symbol != "FRESH.US" || fresh.Timeframe != "1d" || fresh.Count != 3 ||
		fresh.MinTs != freshTs.Add(-48*time.Hour).Format(time.RFC3339) || fresh.MaxTs != freshTs.Format(time.RFC3339) {
		t.Fatalf("fresh entry old fields = %+v; want FRESH.US 1d count 3 with injected ts", fresh)
	}
	if stale.Symbol != "STALE.US" || stale.Timeframe != "1d" || stale.Count != 5 ||
		stale.MinTs != staleTs.Add(-48*time.Hour).Format(time.RFC3339) || stale.MaxTs != staleTs.Format(time.RFC3339) {
		t.Fatalf("stale entry old fields = %+v; want STALE.US 1d count 5 with injected ts", stale)
	}
}

// TestClusterOptionFreshnessFields: options_freshness entries carry
// max_ts_age_seconds and fresh judged by MaxAgeForOptions (4h default) —
// 2h old → fresh, 100h old → stale; old fields stay unchanged.
func TestClusterOptionFreshnessFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	freshTs := now.Add(-2 * time.Hour)    // 4h option threshold → fresh
	staleTs := now.Add(-100 * time.Hour)  // 4h option threshold → stale
	store := &fakeClusterStore{
		coverage: []ingest.BarCoverage{},
		opts: []ingest.OptionFreshness{
			{Underlying: "OPTFRESH.US", Source: "futu", MaxTs: freshTs, AgeSeconds: 7200},
			{Underlying: "OPTSTALE.US", Source: "futu", MaxTs: staleTs, AgeSeconds: 360000},
		},
	}
	rec := get(t, ClusterHandler(testMeta(), store), "/v1/admin/cluster")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got struct {
		Components struct {
			DataPlane struct {
				OptionsFreshness []struct {
					Underlying      string `json:"underlying"`
					Source          string `json:"source"`
					MaxTs           string `json:"max_ts"`
					MaxTsAgeSeconds int64  `json:"max_ts_age_seconds"`
					Fresh           string `json:"fresh"`
				} `json:"options_freshness"`
			} `json:"data_plane"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	opts := got.Components.DataPlane.OptionsFreshness
	if len(opts) != 2 {
		t.Fatalf("options_freshness len = %d; want 2", len(opts))
	}
	fresh, stale := opts[0], opts[1]
	if fresh.Underlying != "OPTFRESH.US" || fresh.Source != "futu" || fresh.MaxTs != freshTs.Format(time.RFC3339) ||
		fresh.Fresh != "fresh" || fresh.MaxTsAgeSeconds < 7199 || fresh.MaxTsAgeSeconds > 7201 {
		t.Fatalf("fresh option entry = %+v; want OPTFRESH.US futu fresh ≈7200s", fresh)
	}
	if stale.Underlying != "OPTSTALE.US" || stale.Source != "futu" || stale.MaxTs != staleTs.Format(time.RFC3339) ||
		stale.Fresh != "stale" || stale.MaxTsAgeSeconds < 359999 || stale.MaxTsAgeSeconds > 360001 {
		t.Fatalf("stale option entry = %+v; want OPTSTALE.US futu stale ≈360000s", stale)
	}
}

// TestClusterDBDown: ping failure reports 200 + db ok:false and skips the
// DB-backed queries, leaving pipeline/data plane empty (⑥-A degraded semantics).
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
	if store.queryCalls != 0 {
		t.Fatalf("query calls = %d; want 0 when ping fails", store.queryCalls)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	comp, ok := got["components"].(map[string]any)
	if !ok {
		t.Fatalf("components = %v; want object", got["components"])
	}
	process, ok := comp["process"].(map[string]any)
	if !ok || process["version"] != meta.Version || process["listen_addr"] != meta.ListenAddr {
		t.Fatalf("process = %v; want injected meta on db down", comp["process"])
	}
	db, ok := comp["db"].(map[string]any)
	if !ok || db["ok"] != false {
		t.Fatalf("db = %v; want ok:false", comp["db"])
	}
	if _, ok := db["latency_ms"]; ok {
		t.Fatalf("db = %v; want latency_ms omitted when down", comp["db"])
	}
	pipeline, ok := comp["pipeline"].(map[string]any)
	if !ok {
		t.Fatalf("pipeline missing: %v", comp)
	}
	counts, ok := pipeline["counts"].(map[string]any)
	if !ok || counts["running"] != float64(0) || counts["succeeded"] != float64(0) || counts["failed"] != float64(0) {
		t.Fatalf("pipeline counts = %v; want all zero when db down", pipeline["counts"])
	}
	runs, ok := pipeline["recent_runs"].([]any)
	if !ok || len(runs) != 0 {
		t.Fatalf("recent_runs = %v; want empty array when db down", pipeline["recent_runs"])
	}
	dp, ok := comp["data_plane"].(map[string]any)
	if !ok {
		t.Fatalf("data_plane missing: %v", comp)
	}
	cov, ok := dp["bars_coverage"].([]any)
	if !ok || len(cov) != 0 {
		t.Fatalf("bars_coverage = %v; want empty array when db down", dp["bars_coverage"])
	}
	of, ok := dp["options_freshness"].([]any)
	if !ok || len(of) != 0 {
		t.Fatalf("options_freshness = %v; want empty array when db down", dp["options_freshness"])
	}
}

func TestClusterQueryError(t *testing.T) {
	for name, store := range map[string]*fakeClusterStore{
		"runs":     {runsErr: errors.New("db down")},
		"counts":   {countsErr: errors.New("db down")},
		"coverage": {covErr: errors.New("db down")},
		"options":  {optsErr: errors.New("db down")},
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
