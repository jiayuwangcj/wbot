package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// fakeStore is a scriptable Store for unit tests.
type fakeStore struct {
	bars     []ingest.Bar
	runs     []ingest.RunStatus
	counts   ingest.RunCounts
	coverage []ingest.BarCoverage
	err      error

	gotSymbol   string
	gotTimefram string
	gotLimit    int
}

func (f *fakeStore) QueryBars(_ context.Context, symbol, timeframe, adjust string, _, _ time.Time, limit int) ([]ingest.Bar, error) {
	f.gotSymbol = symbol
	f.gotTimefram = timeframe
	f.gotLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.bars, nil
}

func (f *fakeStore) RecentRuns(_ context.Context, limit int) ([]ingest.RunStatus, error) {
	f.gotLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

func (f *fakeStore) RunStatusCounts(context.Context) (ingest.RunCounts, error) {
	if f.err != nil {
		return ingest.RunCounts{}, f.err
	}
	return f.counts, nil
}

func (f *fakeStore) BarCoverage(context.Context) ([]ingest.BarCoverage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.coverage, nil
}

func (f *fakeStore) Ping(context.Context) error {
	return f.err
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBarsOK(t *testing.T) {
	ts1 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(24 * time.Hour)
	store := &fakeStore{bars: []ingest.Bar{
		{Ts: ts1, Open: 100, High: 101, Low: 99.5, Close: 100.5, Volume: 1000},
		{Ts: ts2, Open: 100.5, High: 102, Low: 100, Close: 101.25, Volume: 1100},
	}}

	rec := get(t, Handler(store), "/v1/bars?symbol=DEMO.US&timeframe=1d&limit=5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var got []barJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2 (body %s)", len(got), rec.Body)
	}
	for i, want := range []time.Time{ts1, ts2} {
		parsed, err := time.Parse(time.RFC3339, got[i].Ts)
		if err != nil {
			t.Fatalf("bar %d: ts %q not RFC3339: %v", i, got[i].Ts, err)
		}
		if !parsed.Equal(want) {
			t.Fatalf("bar %d: ts = %s; want %s", i, parsed, want)
		}
	}
	want := barJSON{Ts: ts1.Format(time.RFC3339), Open: 100, High: 101, Low: 99.5, Close: 100.5, Volume: 1000}
	if got[0] != want {
		t.Fatalf("bar 0 = %+v; want %+v", got[0], want)
	}
	if store.gotSymbol != "DEMO.US" || store.gotTimefram != "1d" || store.gotLimit != 5 {
		t.Fatalf("store got symbol=%q timeframe=%q limit=%d", store.gotSymbol, store.gotTimefram, store.gotLimit)
	}
}

func TestBarsDefaultLimit(t *testing.T) {
	store := &fakeStore{}
	rec := get(t, Handler(store), "/v1/bars?symbol=DEMO.US&timeframe=1d")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if store.gotLimit != 100 {
		t.Fatalf("default limit = %d; want 100", store.gotLimit)
	}
}

func TestBarsMissingSymbol(t *testing.T) {
	for _, path := range []string{"/v1/bars", "/v1/bars?timeframe=1d"} {
		rec := get(t, Handler(&fakeStore{}), path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d; want 400", path, rec.Code)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s: body %q; want JSON error", path, rec.Body)
		}
	}
}

func TestBarsMissingTimeframe(t *testing.T) {
	rec := get(t, Handler(&fakeStore{}), "/v1/bars?symbol=DEMO.US")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (body %s)", rec.Code, rec.Body)
	}
}

func TestBarsBadTime(t *testing.T) {
	for _, param := range []string{"from", "to"} {
		rec := get(t, Handler(&fakeStore{}), "/v1/bars?symbol=DEMO.US&timeframe=1d&"+param+"=not-a-time")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d; want 400 (body %s)", param, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s: body %q; want JSON error", param, rec.Body)
		}
	}
}

func TestBarsBadLimit(t *testing.T) {
	for _, limit := range []string{"abc", "0", "-5"} {
		rec := get(t, Handler(&fakeStore{}), "/v1/bars?symbol=DEMO.US&timeframe=1d&limit="+limit)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit=%s: status = %d; want 400 (body %s)", limit, rec.Code, rec.Body)
		}
	}
}

func TestBarsStoreError(t *testing.T) {
	rec := get(t, Handler(&fakeStore{err: errors.New("db down")}), "/v1/bars?symbol=DEMO.US&timeframe=1d")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
		t.Fatalf("body %q; want JSON error", rec.Body)
	}
}

func TestRunsOK(t *testing.T) {
	start := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	finished := time.Date(2024, 6, 1, 12, 0, 5, 0, time.UTC)
	store := &fakeStore{runs: []ingest.RunStatus{
		{ID: 1, Source: "cli-mock", Status: "succeeded", StartedAt: start, FinishedAt: &finished},
		{ID: 2, Source: "cli-file", Status: "running", StartedAt: start.Add(24 * time.Hour), FinishedAt: nil},
	}}

	rec := get(t, Handler(store), "/v1/runs?limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if store.gotLimit != 2 {
		t.Fatalf("limit = %d; want 2", store.gotLimit)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2 (body %s)", len(got), rec.Body)
	}
	if got[0]["id"] != float64(1) || got[0]["source"] != "cli-mock" || got[0]["status"] != "succeeded" {
		t.Fatalf("run 0 = %v; want id/source/status fields", got[0])
	}
	if got[0]["started_at"] != start.Format(time.RFC3339) {
		t.Fatalf("run 0 started_at = %v; want %s", got[0]["started_at"], start.Format(time.RFC3339))
	}
	if got[0]["finished_at"] != finished.Format(time.RFC3339) {
		t.Fatalf("run 0 finished_at = %v; want %s", got[0]["finished_at"], finished.Format(time.RFC3339))
	}
	if got[1]["finished_at"] != nil {
		t.Fatalf("run 1 finished_at = %v; want null", got[1]["finished_at"])
	}
}

func TestRunsDefaultLimit(t *testing.T) {
	store := &fakeStore{}
	rec := get(t, Handler(store), "/v1/runs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if store.gotLimit != 10 {
		t.Fatalf("default limit = %d; want 10", store.gotLimit)
	}
}

func TestRunsStoreError(t *testing.T) {
	rec := get(t, Handler(&fakeStore{err: errors.New("db down")}), "/v1/runs")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
}

func TestUnknownPath(t *testing.T) {
	for _, path := range []string{"/", "/v1/nope", "/v1/bars/extra"} {
		rec := get(t, Handler(&fakeStore{}), path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d; want 404 (body %s)", path, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s: body %q; want JSON error", path, rec.Body)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/bars", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	Handler(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestHealthOK(t *testing.T) {
	rec := get(t, Handler(&fakeStore{}), "/v1/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var got healthJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got.Status != "ok" {
		t.Fatalf("status field = %q; want ok (body %s)", got.Status, rec.Body)
	}
}

func TestHealthPingError(t *testing.T) {
	rec := get(t, Handler(&fakeStore{err: errors.New("db down")}), "/v1/health")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", rec.Code, rec.Body)
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
		t.Fatalf("body %q; want JSON error", rec.Body)
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/health", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	Handler(&fakeStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}
