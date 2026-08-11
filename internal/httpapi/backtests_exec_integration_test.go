package httpapi

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/watchlist"
)

func integrationWheelParams() map[string]any {
	return map[string]any{
		"price_position_curve": []any{
			map[string]any{"price": 90.0, "target_inventory": 100.0},
			map[string]any{"price": 130.0, "target_inventory": 100.0},
		},
		"max_inventory":      100.0,
		"min_option_quality": 0.0,
	}
}

func integrationWheelExecBody(symbol string) string {
	return `{"symbol":"` + symbol + `","strategy":"wheel","params":{"price_position_curve":[{"price":90,"target_inventory":100},{"price":130,"target_inventory":100}],"max_inventory":100,"min_option_quality":0}}`
}

// execTestServer wires the serve topology as in cmd/wbot: backtests GET mux +
// POST execute handler + data API on one top mux.
func execTestServer(database *sql.DB) *httptest.Server {
	top := http.NewServeMux()
	bt := BacktestsHandler(NewDBBacktestStore(database))
	top.Handle("/v1/backtests", bt)
	top.Handle("/v1/backtests/", bt)
	top.Handle("POST /v1/backtests", BacktestExecuteHandler(NewDBBacktestExecutor(database), NewDBWatchlistStore(database)))
	top.Handle("/", Handler(NewDBStore(database)))
	return httptest.NewServer(top)
}

// TestBacktestExecuteIntegration drives the real PG POST → list → detail path:
// a manual run saves the deterministic trace, the batch mode runs each
// watchlist row, and a symbol without bars maps to 503 no_data.
func TestBacktestExecuteIntegration(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const (
		symbol    = "BTEXEC.US"
		batchA    = "BTEXECA.US"
		batchB    = "BTEXECB.US"
		noDataSym = "BTEXECNODATA.US"
	)
	for _, s := range []string{symbol, batchA, batchB, noDataSym} {
		if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, s); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, s); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`DELETE FROM option_quote_snapshots WHERE underlying = $1`, s); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`DELETE FROM watchlist WHERE symbol = $1`, s); err != nil {
			t.Fatal(err)
		}
	}
	day := func(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }
	for _, s := range []string{symbol, batchA, batchB} {
		// adjust 'fwd' matches the execute endpoint's documented default.
		for i, c := range []float64{100, 110, 121} {
			if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'fwd', 'futu')`, s, day(i), c); err != nil {
				t.Fatal(err)
			}
		}
		for i, bid := range []float64{3, 2.5, 2} {
			if _, err := database.Exec(`
INSERT INTO option_quote_snapshots
  (symbol, underlying, option_type, strike, expiry, source, snapshot_key,
   underlying_price, delta, bid, ask, iv, theta, volume, open_interest, lot_size, observed_at)
VALUES ($1, $2, 'PUT', 95, $3, 'fixture', $4, $5, -0.30, $6, $7, 0.30, -0.10, 1000, 10000, 100, $8)`,
				s+"-P95", s, day(7), "batch-"+strconv.Itoa(i), []float64{100, 110, 121}[i], bid, bid+0.1, day(i)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The batch runs every watchlist row, so the test needs a clean list.
	if _, err := database.Exec(`DELETE FROM watchlist`); err != nil {
		t.Fatal(err)
	}
	if _, err := watchlist.Upsert(ctx, database, batchA, "wheel", integrationWheelParams()); err != nil {
		t.Fatal(err)
	}
	if _, err := watchlist.Upsert(ctx, database, batchB, "wheel", integrationWheelParams()); err != nil {
		t.Fatal(err)
	}

	srv := execTestServer(database)
	defer srv.Close()

	// Manual run: same input, same output as `wbot backtest -dsn -save`.
	resp, err := http.Post(srv.URL+"/v1/backtests", "application/json",
		bytes.NewBufferString(integrationWheelExecBody(symbol)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d; want 201", resp.StatusCode)
	}
	var detail struct {
		ID          int64          `json:"id"`
		Params      map[string]any `json:"params"`
		Metrics     map[string]any `json:"metrics"`
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
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID <= 0 || detail.Metrics["equity"] != 10100.0 {
		t.Fatalf("detail = %+v; want positive id with equity 10100", detail)
	}
	wantParams := map[string]any{"cash": 10000.0, "fee": 0.0, "timeframe": "1d", "adjust": "fwd"}
	for k, v := range wantParams {
		if detail.Params[k] != v {
			t.Fatalf("params = %v; want %v", detail.Params, wantParams)
		}
	}
	wantCurve := []float64{10000, 10050, 10100}
	if len(detail.EquityCurve) != len(wantCurve) {
		t.Fatalf("equity_curve = %+v; want %v", detail.EquityCurve, wantCurve)
	}
	for i, eq := range wantCurve {
		if detail.EquityCurve[i].Equity != eq {
			t.Fatalf("equity_curve[%d] = %v; want %v", i, detail.EquityCurve[i].Equity, eq)
		}
	}
	if len(detail.Trades) != 1 || detail.Trades[0].Action != "sell-put" ||
		detail.Trades[0].Symbol != symbol+"-P95" || detail.Trades[0].Size != 1 || detail.Trades[0].Price != 3 {
		t.Fatalf("trades = %+v; want sell-put %s-P95 1 @3", detail.Trades, symbol)
	}

	// List: the run appears with the same metrics.
	resp, err = http.Get(srv.URL + "/v1/backtests?symbol=" + symbol)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d; want 200", resp.StatusCode)
	}
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0]["id"] != float64(detail.ID) || list[0]["metrics"].(map[string]any)["equity"] != 10100.0 {
		t.Fatalf("list = %v; want the created run with equity 10100", list)
	}

	// Detail: the GET endpoint returns the same trace the POST returned.
	resp, err = http.Get(srv.URL + "/v1/backtests/" + strconv.FormatInt(detail.ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d; want 200", resp.StatusCode)
	}
	var got struct {
		Metrics     map[string]any `json:"metrics"`
		EquityCurve []struct {
			Equity float64 `json:"equity"`
		} `json:"equity_curve"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Metrics["equity"] != 10100.0 || len(got.EquityCurve) != 3 {
		t.Fatalf("detail = %+v; want equity 10100 with 3 curve points", got)
	}

	// Batch: from_watchlist runs each row serially and saves each.
	resp, err = http.Post(srv.URL+"/v1/backtests", "application/json",
		bytes.NewBufferString(`{"from_watchlist":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("batch status = %d; want 201 (body %s)", resp.StatusCode, readAll(resp))
	}
	var batch struct {
		Runs []struct {
			ID      int64          `json:"id"`
			Symbol  string         `json:"symbol"`
			Metrics map[string]any `json:"metrics"`
		} `json:"runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Runs) != 2 {
		t.Fatalf("batch runs = %+v; want 2", batch.Runs)
	}
	for _, run := range batch.Runs {
		if run.ID <= 0 || run.Metrics["equity"] != 10100.0 {
			t.Fatalf("batch run = %+v; want saved run with equity 10100", run)
		}
	}
	if batch.Runs[0].Symbol == batch.Runs[1].Symbol {
		t.Fatalf("batch runs share symbol %s; want one per watchlist row", batch.Runs[0].Symbol)
	}
	// Each batch row was saved under its own symbol.
	for _, s := range []string{batchA, batchB} {
		resp, err := http.Get(srv.URL + "/v1/backtests?symbol=" + s)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list %s status = %d; want 200", s, resp.StatusCode)
		}
		var list []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0]["metrics"].(map[string]any)["equity"] != 10100.0 {
			t.Fatalf("list %s = %v; want one run with equity 10100", s, list)
		}
	}

	// Missing input data: 503 no_data with an ingest action.
	resp, err = http.Post(srv.URL+"/v1/backtests", "application/json",
		bytes.NewBufferString(integrationWheelExecBody(noDataSym)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no-data status = %d; want 503 (body %s)", resp.StatusCode, readAll(resp))
	}
	var errBody errorJSON
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Code != "no_data" || errBody.Action == "" {
		t.Fatalf("error body = %+v; want no_data with an action", errBody)
	}

	// Cleanup watchlist rows this test added.
	for _, s := range []string{batchA, batchB} {
		if _, err := watchlist.Delete(ctx, database, s); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBacktestExecuteOptionIntegration loads trusted Wheel snapshots and
// returns the same deterministic result as the CLI adapter.
func TestBacktestExecuteOptionIntegration(t *testing.T) {
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.MigrateUp(database); err != nil {
		t.Fatal(err)
	}

	const symbol = "BTEXECOPT.US"
	if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM option_quote_snapshots WHERE underlying = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	day := func(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }
	for i, c := range []float64{100, 103, 99, 106, 104} {
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'fwd', 'futu')`, symbol, day(i), c); err != nil {
			t.Fatal(err)
		}
	}
	for i, bid := range []float64{3, 2.5, 2, 1.5, 1} {
		if _, err := database.Exec(`
INSERT INTO option_quote_snapshots
  (symbol, underlying, option_type, strike, expiry, source, snapshot_key,
   underlying_price, delta, bid, ask, iv, theta, volume, open_interest, lot_size, observed_at)
VALUES ('BTEXECOPT-P95', $1, 'PUT', 95, $2, 'fixture', $3, $4, -0.30, $5, $6, 0.30, -0.10, 1000, 10000, 100, $7)`,
			symbol, day(7), "api-option-"+strconv.Itoa(i), []float64{100, 103, 99, 106, 104}[i], bid, bid+0.1, day(i)); err != nil {
			t.Fatal(err)
		}
	}

	srv := execTestServer(database)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/backtests", "application/json",
		bytes.NewBufferString(integrationWheelExecBody(symbol)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d; want 201 (body %s)", resp.StatusCode, readAll(resp))
	}
	var detail struct {
		Metrics map[string]any `json:"metrics"`
		Params  map[string]any `json:"params"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	// One short P95 receives 300 and is marked at the final bid 1. Existing
	// assignment commitment prevents repeated puts beyond max_inventory.
	if detail.Metrics["equity"] != 10200.0 || detail.Metrics["bars"] != 5.0 {
		t.Fatalf("metrics = %v; want equity 10200 over 5 bars", detail.Metrics)
	}
	if detail.Params["adjust"] != "fwd" || detail.Params["timeframe"] != "1d" {
		t.Fatalf("params = %v; want timeframe 1d adjust fwd", detail.Params)
	}
}

func readAll(resp *http.Response) string {
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return buf.String()
}
