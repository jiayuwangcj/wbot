package httpapi

// Contract tests for the /v1/backtests endpoints (draft 2026-08-02 S1):
// list summaries, detail with equity_curve/trades, error body {code,message,action}.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
)

var errBoom = errors.New("boom")

// fakeBacktestStore is a scriptable BacktestStore for success and error-path tests.
type fakeBacktestStore struct {
	recs        []backtest.ResultRecord
	rec         *backtest.ResultRecord
	listErr     error
	getErr      error
	gotSymbol   string
	gotStrategy string
	gotLimit    int
	gotSortKey  string
	gotDesc     bool
	gotID       int64
}

func (f *fakeBacktestStore) List(_ context.Context, symbol, strategy string, limit int, sortKey string, desc bool) ([]backtest.ResultRecord, error) {
	f.gotSymbol, f.gotStrategy, f.gotLimit, f.gotSortKey, f.gotDesc = symbol, strategy, limit, sortKey, desc
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.recs, nil
}

func (f *fakeBacktestStore) Get(_ context.Context, id int64) (*backtest.ResultRecord, error) {
	f.gotID = id
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.rec, nil
}

func sampleRecord(id int64) backtest.ResultRecord {
	ts := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	return backtest.ResultRecord{
		ID: id, Strategy: "buy-hold", Symbol: "DEMO.US",
		Params:    map[string]any{"cash": 10000.0, "fee": 0.0},
		Metrics:   map[string]any{"equity": 10500.0, "total_return": 0.05, "max_drawdown": 0.02, "bars": 2},
		StartTs:   ts,
		EndTs:     ts.Add(24 * time.Hour),
		CreatedAt: ts.Add(48 * time.Hour),
		EquityCurve: []backtest.EquityPoint{
			{Ts: ts, Equity: 10000},
			{Ts: ts.Add(24 * time.Hour), Equity: 10500},
		},
		Trades: []backtest.Trade{
			{Ts: ts, Action: "buy", Symbol: "DEMO.US", Size: 100, Price: 100, CashAfter: 0},
		},
	}
}

func TestBacktestsList(t *testing.T) {
	fake := &fakeBacktestStore{recs: []backtest.ResultRecord{sampleRecord(7), sampleRecord(8)}}
	rec := get(t, BacktestsHandler(fake), "/v1/backtests?symbol=DEMO.US&strategy=buy-hold&limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	if fake.gotSymbol != "DEMO.US" || fake.gotStrategy != "buy-hold" || fake.gotLimit != 2 {
		t.Fatalf("filters = (%q, %q, %d); want (DEMO.US, buy-hold, 2)", fake.gotSymbol, fake.gotStrategy, fake.gotLimit)
	}
	var got []backtestSummaryJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
	first := got[0]
	if first.ID != 7 || first.Strategy != "buy-hold" || first.Symbol != "DEMO.US" {
		t.Fatalf("summary = %+v; want id=7 buy-hold DEMO.US", first)
	}
	if first.Params["cash"] != 10000.0 || first.Metrics["equity"] != 10500.0 {
		t.Fatalf("summary = %+v; want params/metrics passthrough", first)
	}
	for _, ts := range []string{first.StartTs, first.EndTs, first.CreatedAt} {
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Fatalf("summary ts %q: %v", ts, err)
		}
	}
	if strings.Contains(rec.Body.String(), "equity_curve") {
		t.Fatalf("list must stay curve-free: %s", rec.Body)
	}
}

func TestBacktestsListDefaults(t *testing.T) {
	fake := &fakeBacktestStore{}
	rec := get(t, BacktestsHandler(fake), "/v1/backtests")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if fake.gotLimit != 50 {
		t.Fatalf("default limit = %d; want 50", fake.gotLimit)
	}
	if got := rec.Body.String(); got != "[]\n" && got != "[]" {
		t.Fatalf("empty list body = %q; want []", got)
	}
}

// TestBacktestsListSort: 跨页排序参数契约(2026-08-02):sort 白名单键
// 透传到 store,order asc/desc 解析;非法 sort/order 400。
func TestBacktestsListSort(t *testing.T) {
	fake := &fakeBacktestStore{}
	get(t, BacktestsHandler(fake), "/v1/backtests?sort=total_return&order=desc")
	if fake.gotSortKey != "total_return" || !fake.gotDesc {
		t.Fatalf("sort = (%q, %v); want (total_return, true)", fake.gotSortKey, fake.gotDesc)
	}
	fake = &fakeBacktestStore{}
	get(t, BacktestsHandler(fake), "/v1/backtests?sort=created_at")
	if fake.gotSortKey != "created_at" || fake.gotDesc {
		t.Fatalf("sort = (%q, %v); want (created_at, asc default)", fake.gotSortKey, fake.gotDesc)
	}
	fake = &fakeBacktestStore{}
	get(t, BacktestsHandler(fake), "/v1/backtests")
	if fake.gotSortKey != "" || fake.gotDesc {
		t.Fatalf("no-sort = (%q, %v); want newest-first passthrough", fake.gotSortKey, fake.gotDesc)
	}
	for _, q := range []string{"sort=weird", "sort=id'--", "sort=id&order=sideways", "sort=id&order=DESC2"} {
		rec := get(t, BacktestsHandler(&fakeBacktestStore{}), "/v1/backtests?"+q)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("q=%s status = %d; want 400 (body %s)", q, rec.Code, rec.Body)
		}
		assertErrorShape(t, rec, "invalid_request")
	}
	for _, key := range backtest.SortKeyNames() {
		if !backtest.ValidSortKey(key) {
			t.Fatalf("SortKeyNames listed %q but ValidSortKey says no", key)
		}
	}
}

func TestBacktestsListInvalidLimit(t *testing.T) {
	for _, limit := range []string{"0", "-1", "abc"} {
		rec := get(t, BacktestsHandler(&fakeBacktestStore{}), "/v1/backtests?limit="+limit)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit=%s status = %d; want 400 (body %s)", limit, rec.Code, rec.Body)
		}
		assertErrorShape(t, rec, "invalid_request")
	}
}

func TestBacktestsListInternalError(t *testing.T) {
	rec := get(t, BacktestsHandler(&fakeBacktestStore{listErr: errBoom}), "/v1/backtests")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	assertErrorShape(t, rec, "internal_error")
}

func TestBacktestsDetail(t *testing.T) {
	rec := sampleRecord(3)
	got := get(t, BacktestsHandler(&fakeBacktestStore{rec: &rec}), "/v1/backtests/3")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", got.Code, got.Body)
	}
	var detail struct {
		backtestSummaryJSON
		EquityCurve []struct {
			Ts     string  `json:"ts"`
			Equity float64 `json:"equity"`
		} `json:"equity_curve"`
		Trades []struct {
			Ts        string  `json:"ts"`
			Action    string  `json:"action"`
			Symbol    string  `json:"symbol"`
			Size      float64 `json:"size"`
			Price     float64 `json:"price"`
			CashAfter float64 `json:"cash_after"`
		} `json:"trades"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, got.Body)
	}
	if detail.ID != 3 || detail.Metrics["equity"] != 10500.0 {
		t.Fatalf("detail = %+v; want id 3 with metrics", detail.backtestSummaryJSON)
	}
	if len(detail.EquityCurve) != 2 || detail.EquityCurve[1].Equity != 10500 {
		t.Fatalf("equity_curve = %+v; want 2 points ending at 10500", detail.EquityCurve)
	}
	if len(detail.Trades) != 1 {
		t.Fatalf("trades = %+v; want 1", detail.Trades)
	}
	tr := detail.Trades[0]
	if tr.Action != "buy" || tr.Symbol != "DEMO.US" || tr.Size != 100 || tr.Price != 100 || tr.CashAfter != 0 {
		t.Fatalf("trade = %+v; want buy 100 @100 cash_after 0", tr)
	}
	if _, err := time.Parse(time.RFC3339, detail.Trades[0].Ts); err != nil {
		t.Fatalf("trade ts %q: %v", detail.Trades[0].Ts, err)
	}
}

func TestBacktestsDetailNotFound(t *testing.T) {
	rec := get(t, BacktestsHandler(&fakeBacktestStore{getErr: backtest.ErrResultNotFound}), "/v1/backtests/42")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
	var errBody errorJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Code != "not_found" || errBody.Message == "" || errBody.Action == "" {
		t.Fatalf("error body = %+v; want not_found with message and action", errBody)
	}
}

func TestBacktestsDetailInvalidID(t *testing.T) {
	for _, id := range []string{"abc", "0", "-1", "1.5", "99999999999999999999"} {
		rec := get(t, BacktestsHandler(&fakeBacktestStore{}), "/v1/backtests/"+id)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id=%s status = %d; want 400 (body %s)", id, rec.Code, rec.Body)
		}
		assertErrorShape(t, rec, "invalid_request")
	}
}

func TestBacktestsDetailInternalError(t *testing.T) {
	rec := get(t, BacktestsHandler(&fakeBacktestStore{getErr: errBoom}), "/v1/backtests/1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500 (body %s)", rec.Code, rec.Body)
	}
	assertErrorShape(t, rec, "internal_error")
}

func TestBacktestsMethodNotAllowed(t *testing.T) {
	h := BacktestsHandler(&fakeBacktestStore{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		for _, path := range []string{"/v1/backtests", "/v1/backtests/1"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s: status = %d; want 405 (body %s)", method, path, rec.Code, rec.Body)
			}
			assertErrorShape(t, rec, "method_not_allowed")
		}
	}
}

func TestBacktestsUnknownSubpath(t *testing.T) {
	rec := get(t, BacktestsHandler(&fakeBacktestStore{}), "/v1/backtests/1/extra")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
	assertErrorShape(t, rec, "not_found")
}

// --- GET /v1/backtests/{id}/export (draft 2026-08-02: result export) ---

func TestBacktestsExportCSV(t *testing.T) {
	rec := sampleRecord(3)
	got := get(t, BacktestsHandler(&fakeBacktestStore{rec: &rec}), "/v1/backtests/3/export")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", got.Code, got.Body)
	}
	if ct := got.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type = %q; want text/csv; charset=utf-8", ct)
	}
	// CreatedAt = 2026-08-03 in sampleRecord: filename carries id-strategy-date.
	if cd := got.Header().Get("Content-Disposition"); cd != `attachment; filename="backtest-3-buy-hold-2026-08-03.csv"` {
		t.Fatalf("content-disposition = %q; want attachment filename backtest-3-buy-hold-2026-08-03.csv", cd)
	}
	want := "equity_curve\nts,equity\n2026-08-01T00:00:00Z,10000\n2026-08-02T00:00:00Z,10500\n\ntrades\nts,action,symbol,size,price,cash_after\n2026-08-01T00:00:00Z,buy,DEMO.US,100,100,0\n"
	if got.Body.String() != want {
		t.Fatalf("csv body = %q; want %q", got.Body, want)
	}
}

func TestBacktestsExportCSVExplicitFormat(t *testing.T) {
	rec := sampleRecord(3)
	got := get(t, BacktestsHandler(&fakeBacktestStore{rec: &rec}), "/v1/backtests/3/export?format=csv")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "equity_curve\nts,equity\n") {
		t.Fatalf("status = %d body %s; want 200 with equity section", got.Code, got.Body)
	}
}

// Roundtrip: export?format=json is byte-identical to the detail endpoint
// (same serializer, same source record).
func TestBacktestsExportJSONMatchesDetail(t *testing.T) {
	rec := sampleRecord(3)
	h := BacktestsHandler(&fakeBacktestStore{rec: &rec})
	detail := get(t, h, "/v1/backtests/3")
	exp := get(t, h, "/v1/backtests/3/export?format=json")
	if detail.Code != http.StatusOK || exp.Code != http.StatusOK {
		t.Fatalf("detail=%d export=%d; want 200/200", detail.Code, exp.Code)
	}
	if ct := exp.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	if cd := exp.Header().Get("Content-Disposition"); !strings.HasSuffix(cd, `backtest-3-buy-hold-2026-08-03.json"`) {
		t.Fatalf("content-disposition = %q; want .json attachment", cd)
	}
	if exp.Body.String() != detail.Body.String() {
		t.Fatalf("export json != detail: export %q detail %q", exp.Body, detail.Body)
	}
}

func TestBacktestsExportNoTraceStillServed(t *testing.T) {
	rec := sampleRecord(5)
	rec.EquityCurve, rec.Trades = nil, nil
	h := BacktestsHandler(&fakeBacktestStore{rec: &rec})
	csv := get(t, h, "/v1/backtests/5/export")
	if csv.Code != http.StatusOK {
		t.Fatalf("csv status = %d; want 200 (body %s)", csv.Code, csv.Body)
	}
	// Compatibility: 200 with empty curve/trades sections (headers only).
	if !strings.Contains(csv.Body.String(), "equity_curve\nts,equity\n\ntrades\nts,action,symbol,size,price,cash_after\n") {
		t.Fatalf("no-trace csv = %q; want empty sections after headers", csv.Body)
	}
	jsonGot := get(t, h, "/v1/backtests/5/export?format=json")
	if jsonGot.Code != http.StatusOK {
		t.Fatalf("json status = %d; want 200 (body %s)", jsonGot.Code, jsonGot.Body)
	}
	var detail backtest.DetailJSON
	if err := json.Unmarshal(jsonGot.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, jsonGot.Body)
	}
	if len(detail.EquityCurve) != 0 || len(detail.Trades) != 0 {
		t.Fatalf("no-trace json = curve %d trades %d; want empty arrays", len(detail.EquityCurve), len(detail.Trades))
	}
}

func TestBacktestsExportNotFound(t *testing.T) {
	rec := get(t, BacktestsHandler(&fakeBacktestStore{getErr: backtest.ErrResultNotFound}), "/v1/backtests/42/export")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
	assertErrorShape(t, rec, "not_found")
}

func TestBacktestsExportInvalidFormat(t *testing.T) {
	rec := sampleRecord(3)
	got := get(t, BacktestsHandler(&fakeBacktestStore{rec: &rec}), "/v1/backtests/3/export?format=xml")
	if got.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (body %s)", got.Code, got.Body)
	}
	assertErrorShape(t, got, "invalid_request")
}

func TestBacktestsExportInvalidID(t *testing.T) {
	for _, id := range []string{"abc", "0", "-1"} {
		rec := get(t, BacktestsHandler(&fakeBacktestStore{}), "/v1/backtests/"+id+"/export")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id=%s status = %d; want 400 (body %s)", id, rec.Code, rec.Body)
		}
		assertErrorShape(t, rec, "invalid_request")
	}
}

func TestBacktestsExportMethodNotAllowed(t *testing.T) {
	h := BacktestsHandler(&fakeBacktestStore{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/v1/backtests/1/export", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s export: status = %d; want 405 (body %s)", method, rec.Code, rec.Body)
		}
		assertErrorShape(t, rec, "method_not_allowed")
	}
}

// assertErrorShape checks the new error contract {code,message,action}.
func assertErrorShape(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var errBody errorJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("error body %q not JSON: %v", rec.Body, err)
	}
	if errBody.Code != wantCode || errBody.Message == "" {
		t.Fatalf("error body = %+v; want code %q with message", errBody, wantCode)
	}
}
