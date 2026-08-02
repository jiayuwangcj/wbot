package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeIngestRunner records the last request and fails on demand.
type fakeIngestRunner struct {
	gotSymbol, gotTimeframe, gotAdjust string
	fail                               error
}

func (f *fakeIngestRunner) RunBars(_ context.Context, symbol, timeframe, adjust string) error {
	f.gotSymbol, f.gotTimeframe, f.gotAdjust = symbol, timeframe, adjust
	return f.fail
}

func TestIngestHandler(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		fail       error
		wantStatus int
		wantCode   string
		wantCall   bool
		useGet     bool
	}{
		{name: "ok with defaults", body: `{"symbol":"HK.00700"}`, wantStatus: http.StatusCreated, wantCall: true},
		{name: "ok full body", body: `{"symbol":"US.AAPL","timeframe":"K_DAY","adjust":"none"}`, wantStatus: http.StatusCreated, wantCall: true},
		{name: "method not allowed", body: "", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed", useGet: true},
		{name: "invalid json", body: `{`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "empty symbol", body: `{"symbol":"  "}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "ingest failed", body: `{"symbol":"HK.00700"}`, fail: errors.New("ingest: no bars from source"), wantStatus: http.StatusServiceUnavailable, wantCode: "ingest_failed", wantCall: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeIngestRunner{fail: tc.fail}
			mux := IngestHandler(runner)
			method := http.MethodPost
			if tc.useGet {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, ingestPath, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantCode != "" {
				var e errorJSON
				if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
					t.Fatalf("error body not json: %v", err)
				}
				if e.Code != tc.wantCode {
					t.Fatalf("code = %q, want %q", e.Code, tc.wantCode)
				}
			}
			if tc.wantCall {
				if !strings.Contains(runner.gotSymbol, ".") {
					t.Fatalf("RunBars not called with symbol: %q", runner.gotSymbol)
				}
				if runner.gotTimeframe == "" || runner.gotAdjust == "" {
					t.Fatalf("defaults not applied: timeframe=%q adjust=%q", runner.gotTimeframe, runner.gotAdjust)
				}
				if tc.name == "ok full body" && runner.gotAdjust != "none" {
					t.Fatalf("adjust = %q, want none", runner.gotAdjust)
				}
			} else if runner.gotSymbol != "" {
				t.Fatalf("RunBars called for %s, want no call", runner.gotSymbol)
			}
		})
	}
}

func TestIngestHandlerCreatedBody(t *testing.T) {
	runner := &fakeIngestRunner{}
	mux := IngestHandler(runner)
	req := httptest.NewRequest(http.MethodPost, ingestPath, strings.NewReader(`{"symbol":"HK.00700"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body struct {
		Symbol string `json:"symbol"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body.Symbol != "HK.00700" || body.Status != "ok" {
		t.Fatalf("body = %+v", body)
	}
}
