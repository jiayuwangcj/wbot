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
	gotKind                            string // "bars" | "option"
	fail                               error
}

func (f *fakeIngestRunner) RunBars(_ context.Context, symbol, timeframe, adjust string) error {
	f.gotKind, f.gotSymbol, f.gotTimeframe, f.gotAdjust = "bars", symbol, timeframe, adjust
	return f.fail
}

func (f *fakeIngestRunner) RunOptions(_ context.Context, underlying, adjust string) error {
	f.gotKind, f.gotSymbol, f.gotTimeframe, f.gotAdjust = "option", underlying, "", adjust
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
		wantKind   string
		useGet     bool
	}{
		{name: "ok with defaults", body: `{"symbol":"HK.00700"}`, wantStatus: http.StatusCreated, wantCall: true, wantKind: "bars"},
		{name: "ok full body", body: `{"symbol":"US.AAPL","timeframe":"K_DAY","adjust":"none"}`, wantStatus: http.StatusCreated, wantCall: true, wantKind: "bars"},
		{name: "ok option kind", body: `{"kind":"option","symbol":"HK.00700"}`, wantStatus: http.StatusCreated, wantCall: true, wantKind: "option"},
		{name: "ok option full body", body: `{"kind":"option","symbol":"HK.00700","adjust":"none"}`, wantStatus: http.StatusCreated, wantCall: true, wantKind: "option"},
		{name: "method not allowed", body: "", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed", useGet: true},
		{name: "invalid json", body: `{`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "empty symbol", body: `{"symbol":"  "}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "option empty symbol", body: `{"kind":"option","symbol":""}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "ingest failed", body: `{"symbol":"HK.00700"}`, fail: errors.New("ingest: no bars from source"), wantStatus: http.StatusServiceUnavailable, wantCode: "ingest_failed", wantCall: true},
		{name: "option failed", body: `{"kind":"option","symbol":"HK.00700"}`, fail: errors.New("ingest: futu-option: no future expiries listed"), wantStatus: http.StatusServiceUnavailable, wantCode: "ingest_failed", wantCall: true, wantKind: "option"},
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
					t.Fatalf("ingest not called with symbol: %q", runner.gotSymbol)
				}
				if runner.gotAdjust == "" {
					t.Fatalf("default adjust not applied: %q", runner.gotAdjust)
				}
				if tc.wantKind != "" && runner.gotKind != tc.wantKind {
					t.Fatalf("kind = %q, want %q", runner.gotKind, tc.wantKind)
				}
				if runner.gotKind == "bars" && runner.gotTimeframe == "" {
					t.Fatalf("default timeframe not applied: %q", runner.gotTimeframe)
				}
				if (tc.name == "ok full body" || tc.name == "ok option full body") && runner.gotAdjust != "none" {
					t.Fatalf("adjust = %q, want none", runner.gotAdjust)
				}
			} else if runner.gotSymbol != "" {
				t.Fatalf("ingest called for %s, want no call", runner.gotSymbol)
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

func TestIngestOptionCreatedBody(t *testing.T) {
	runner := &fakeIngestRunner{}
	mux := IngestHandler(runner)
	req := httptest.NewRequest(http.MethodPost, ingestPath, strings.NewReader(`{"kind":"option","symbol":"HK.00700"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Kind   string `json:"kind"`
		Symbol string `json:"symbol"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body.Kind != "option" || body.Symbol != "HK.00700" || body.Status != "ok" {
		t.Fatalf("body = %+v", body)
	}
	if runner.gotKind != "option" || runner.gotAdjust != "fwd" {
		t.Fatalf("runner got kind=%q adjust=%q; want option/fwd", runner.gotKind, runner.gotAdjust)
	}
}
