package httpapi

// Contract tests for the POST /v1/backtests execution endpoint (draft
// 2026-08-02 S4): 201 detail, 409 busy, 422 validation, 503 dependency
// failure, 405, and the from_watchlist batch semantics.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/backtestexec"
	"github.com/jiayu/wbot/internal/watchlist"
)

const validWheelParamsJSON = `"full_position_price":400,"zero_position_price":550,"max_inventory":1200`

func wheelExecBody(symbol string) string {
	return `{"symbol":"` + symbol + `","strategy":"wheel","params":{` + validWheelParamsJSON + `}}`
}

func validWheelParams() map[string]any {
	return map[string]any{
		"full_position_price": 400.0,
		"zero_position_price": 550.0,
		"max_inventory":       1200.0,
	}
}

// fakeExecutor is a scriptable BacktestExecutor for the execute-endpoint tests.
type fakeExecutor struct {
	rec         *backtest.ResultRecord
	err         error
	waitCtx     bool          // when set, RunOne blocks until ctx done and returns ctx.Err()
	block       chan struct{} // when set, RunOne waits for a close before returning
	enterOnce   sync.Once     // closes entered on the first RunOne call
	entered     chan struct{}
	gotSymbol   string
	gotStrategy string
	gotParams   map[string]any
	callsLog    []string
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{entered: make(chan struct{})}
}

func (f *fakeExecutor) RunOne(ctx context.Context, symbol, strategy string, params map[string]any) (*backtest.ResultRecord, error) {
	f.enterOnce.Do(func() { close(f.entered) })
	f.gotSymbol, f.gotStrategy, f.gotParams = symbol, strategy, params
	f.callsLog = append(f.callsLog, symbol)
	if f.waitCtx {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.block != nil {
		<-f.block
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.rec, nil
}

func TestBacktestExecuteManual(t *testing.T) {
	rec := sampleRecord(9)
	fake := newFakeExecutor()
	fake.rec = &rec
	h := BacktestExecuteHandler(fake, &fakeWatchlistStore{})
	got := postExec(t, h, wheelExecBody("DEMO.US"))
	if got.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 (body %s)", got.Code, got.Body)
	}
	if ct := got.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	var detail backtest.DetailJSON
	if err := json.Unmarshal(got.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, got.Body)
	}
	if detail.ID != 9 || detail.Metrics["equity"] != 10500.0 || detail.Symbol != "DEMO.US" {
		t.Fatalf("detail = %+v; want id 9 with equity 10500", detail)
	}
	if len(detail.EquityCurve) != 2 || len(detail.Trades) != 1 {
		t.Fatalf("detail trace = curve %d trades %d; want 2/1", len(detail.EquityCurve), len(detail.Trades))
	}
	if fake.gotSymbol != "DEMO.US" || fake.gotStrategy != "wheel" {
		t.Fatalf("executor got (%q, %q); want (DEMO.US, wheel)", fake.gotSymbol, fake.gotStrategy)
	}
	if fake.gotParams == nil || fake.gotParams["full_position_price"] != 400.0 || fake.gotParams["zero_position_price"] != 550.0 {
		t.Fatalf("executor params = %v; want required Wheel inputs", fake.gotParams)
	}
}

func TestBacktestExecuteManualParams(t *testing.T) {
	rec := sampleRecord(9)
	fake := newFakeExecutor()
	fake.rec = &rec
	got := postExec(t, BacktestExecuteHandler(fake, &fakeWatchlistStore{}),
		`{"symbol":"HK.00700","strategy":"wheel","params":{`+validWheelParamsJSON+`,"strategic_state":"CAUTION"}}`)
	if got.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 (body %s)", got.Code, got.Body)
	}
	if fake.gotParams["strategic_state"] != "CAUTION" {
		t.Fatalf("executor params = %v; want Wheel params with CAUTION", fake.gotParams)
	}
}

func TestBacktestExecuteValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		code    string
		wantMsg string
	}{
		{"bad json", `{`, "invalid_request", "invalid JSON body"},
		{"empty body", ``, "invalid_request", "invalid JSON body"},
		{"no fields", `{}`, "invalid_request", "missing symbol"},
		{"only symbol", `{"symbol":"DEMO.US"}`, "invalid_request", "missing strategy"},
		{"only strategy", `{"strategy":"wheel"}`, "invalid_request", "missing symbol"},
		{"unknown strategy", `{"symbol":"DEMO.US","strategy":"nope"}`, "invalid_request", "unknown template"},
		{"benchmark hidden", `{"symbol":"DEMO.US","strategy":"hold"}`, "invalid_request", "unknown template"},
		{"missing strategic inputs", `{"symbol":"DEMO.US","strategy":"wheel","params":{}}`, "invalid_request", "required"},
		{"bad param type", `{"symbol":"DEMO.US","strategy":"wheel","params":{` + validWheelParamsJSON + `,"move_interval_pct":"0.01"}}`, "invalid_request", "want a number"},
		{"param out of range", `{"symbol":"DEMO.US","strategy":"wheel","params":{` + validWheelParamsJSON + `,"min_option_quality":-1}}`, "invalid_request", "want in"},
		{"unknown param", `{"symbol":"DEMO.US","strategy":"wheel","params":{` + validWheelParamsJSON + `,"bogus":1}}`, "invalid_request", "unknown param"},
		{"from_watchlist exclusive", `{"from_watchlist":true,"symbol":"DEMO.US"}`, "invalid_request", "mutually exclusive"},
		{"from_watchlist with params", `{"from_watchlist":true,"params":{}}`, "invalid_request", "mutually exclusive"},
		{"empty watchlist", `{"from_watchlist":true}`, "empty_watchlist", "watchlist is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeExecutor()
			h := BacktestExecuteHandler(fake, &fakeWatchlistStore{})
			got := postExec(t, h, tt.body)
			if got.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d; want 422 (body %s)", got.Code, got.Body)
			}
			var errBody errorJSON
			if err := json.Unmarshal(got.Body.Bytes(), &errBody); err != nil {
				t.Fatalf("error body %q not JSON: %v", got.Body, err)
			}
			if errBody.Code != tt.code || !strings.Contains(errBody.Message, tt.wantMsg) {
				t.Fatalf("error body = %+v; want code %q with message containing %q", errBody, tt.code, tt.wantMsg)
			}
			if errBody.Action == "" {
				t.Fatalf("error body = %+v; want a non-empty action", errBody)
			}
			if len(fake.callsLog) != 0 {
				t.Fatalf("executor called %v; want no call on validation failure", fake.callsLog)
			}
		})
	}
}

func TestBacktestExecuteBusy(t *testing.T) {
	rec := sampleRecord(9)
	fake := newFakeExecutor()
	fake.rec = &rec
	fake.block = make(chan struct{})
	h := BacktestExecuteHandler(fake, &fakeWatchlistStore{})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postExec(t, h, wheelExecBody("DEMO.US"))
	}()
	// Wait until the first run holds the mutex (inside RunOne).
	select {
	case <-fake.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first run never entered the executor")
	}

	got := postExec(t, h, wheelExecBody("DEMO.US"))
	if got.Code != http.StatusConflict {
		t.Fatalf("status = %d; want 409 (body %s)", got.Code, got.Body)
	}
	assertErrorShape(t, got, "busy")

	close(fake.block)
	if first := <-done; first.Code != http.StatusCreated {
		t.Fatalf("first run status = %d; want 201 (body %s)", first.Code, first.Body)
	}
}

func TestBacktestExecuteTimeout(t *testing.T) {
	fake := newFakeExecutor()
	fake.waitCtx = true
	h := newBacktestExecuteHandler(fake, &fakeWatchlistStore{}, 50*time.Millisecond)
	got := postExec(t, h, wheelExecBody("DEMO.US"))
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", got.Code, got.Body)
	}
	assertErrorShape(t, got, "timeout")
}

func TestBacktestExecuteNoData(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{"no bars", fmt.Errorf("%w: DEMO.US", backtestexec.ErrNoBars), "no bars"},
		{"no options", fmt.Errorf("%w: DEMO.US", backtestexec.ErrNoOptionData), "no complete atomic option quote snapshots"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeExecutor()
			fake.err = tt.err
			got := postExec(t, BacktestExecuteHandler(fake, &fakeWatchlistStore{}),
				wheelExecBody("DEMO.US"))
			if got.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d; want 503 (body %s)", got.Code, got.Body)
			}
			var errBody errorJSON
			if err := json.Unmarshal(got.Body.Bytes(), &errBody); err != nil {
				t.Fatal(err)
			}
			if errBody.Code != "no_data" || !strings.Contains(errBody.Message, tt.wantMsg) || errBody.Action == "" {
				t.Fatalf("error body = %+v; want no_data with %q and an action", errBody, tt.wantMsg)
			}
			if tt.name == "no options" && (errBody.CapabilityStatus != "DATA_BLOCKED" || len(errBody.BlockedBy) != 1 || errBody.BlockedBy[0] != "option_quote_snapshots" || strings.Contains(errBody.Action, "ingest futu-option")) {
				t.Fatalf("error body = %+v; want explicit DATA_BLOCKED without a legacy ingestion fallback", errBody)
			}
		})
	}
}

func TestBacktestExecuteDependencyError(t *testing.T) {
	fake := newFakeExecutor()
	fake.err = errBoom
	got := postExec(t, BacktestExecuteHandler(fake, &fakeWatchlistStore{}),
		wheelExecBody("DEMO.US"))
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", got.Code, got.Body)
	}
	assertErrorShape(t, got, "dependency_failed")
}

func TestBacktestExecuteFromWatchlist(t *testing.T) {
	items := []watchlist.Item{
		{Symbol: "HK.00700", Strategy: "wheel", Params: validWheelParams()},
		{Symbol: "HK.09988", Strategy: "wheel", Params: validWheelParams()},
	}
	wstore := &fakeWatchlistStore{items: items}
	rec := sampleRecord(3)
	fake := newFakeExecutor()
	fake.rec = &rec

	got := postExec(t, BacktestExecuteHandler(fake, wstore), `{"from_watchlist":true}`)
	if got.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 (body %s)", got.Code, got.Body)
	}
	var body struct {
		Runs []backtest.DetailJSON `json:"runs"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, got.Body)
	}
	if len(body.Runs) != 2 {
		t.Fatalf("runs len = %d; want 2 (body %s)", len(body.Runs), got.Body)
	}
	for _, run := range body.Runs {
		if run.ID != 3 || len(run.EquityCurve) != 2 {
			t.Fatalf("run = %+v; want id 3 with curve", run)
		}
	}
	wantOrder := []string{"HK.00700", "HK.09988"}
	if len(fake.callsLog) != 2 || fake.callsLog[0] != wantOrder[0] || fake.callsLog[1] != wantOrder[1] {
		t.Fatalf("executor call order = %v; want %v (serial)", fake.callsLog, wantOrder)
	}
	if fake.gotStrategy != "wheel" {
		t.Fatalf("last run strategy = %q; want wheel", fake.gotStrategy)
	}
}

func TestBacktestExecuteWatchlistDependencyError(t *testing.T) {
	wstore := &fakeWatchlistStore{listErr: errBoom}
	fake := newFakeExecutor()
	got := postExec(t, BacktestExecuteHandler(fake, wstore), `{"from_watchlist":true}`)
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", got.Code, got.Body)
	}
	assertErrorShape(t, got, "dependency_failed")
}

func TestBacktestExecuteWatchlistRunFailureAborts(t *testing.T) {
	items := []watchlist.Item{
		{Symbol: "HK.00700", Strategy: "wheel", Params: validWheelParams()},
		{Symbol: "HK.09988", Strategy: "wheel", Params: validWheelParams()},
	}
	rec := sampleRecord(3)
	fake := newFakeExecutor()
	fake.rec = &rec
	fake.err = errBoom // fails on the first row; the second must never run
	got := postExec(t, BacktestExecuteHandler(fake, &fakeWatchlistStore{items: items}), `{"from_watchlist":true}`)
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", got.Code, got.Body)
	}
	if len(fake.callsLog) != 1 || fake.callsLog[0] != "HK.00700" {
		t.Fatalf("executor calls = %v; want exactly the first row then abort", fake.callsLog)
	}
}

func TestBacktestExecuteMethodNotAllowed(t *testing.T) {
	h := BacktestExecuteHandler(newFakeExecutor(), &fakeWatchlistStore{})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/v1/backtests", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: status = %d; want 405 (body %s)", method, rec.Code, rec.Body)
		}
		assertErrorShape(t, rec, "method_not_allowed")
	}
}

func TestBacktestExecuteUnknownSubpath(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/backtests/1/extra", nil)
	rec := httptest.NewRecorder()
	BacktestExecuteHandler(newFakeExecutor(), &fakeWatchlistStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
	assertErrorShape(t, rec, "not_found")
}

// postExec sends one POST /v1/backtests request to the execute handler.
func postExec(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/backtests", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
