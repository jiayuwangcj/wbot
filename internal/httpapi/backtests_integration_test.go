package httpapi

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job db-integration).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/db"
)

// TestBacktestsIntegration drives the real PG save → list → detail path:
// SaveResult with a trace, then the handler returns the same curve/trades.
func TestBacktestsIntegration(t *testing.T) {
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
	const symbol = "BTAPIHTTP.US"
	if _, err := database.Exec(`DELETE FROM backtest_results WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	id, err := backtest.SaveResult(ctx, database, "buy-hold", symbol,
		map[string]any{"cash": 10000.0, "fee": 0.0},
		&backtest.Result{
			Equity: 10500, TotalReturn: 0.05, MaxDrawdown: 0.02, Bars: 2,
			EquityCurve: []backtest.EquityPoint{
				{Ts: start, Equity: 10000},
				{Ts: end, Equity: 10500},
			},
			Trades: []backtest.Trade{{Ts: start, Action: "buy", Size: 100, Price: 100, CashAfter: 0}},
		}, start, end)
	if err != nil {
		t.Fatal(err)
	}

	// Serve wiring as in cmd/wbot: backtests handler + data API on one mux.
	top := http.NewServeMux()
	bt := BacktestsHandler(NewDBBacktestStore(database))
	top.Handle("/v1/backtests", bt)
	top.Handle("/v1/backtests/", bt)
	top.Handle("/", Handler(NewDBStore(database)))
	srv := httptest.NewServer(top)
	defer srv.Close()

	// List: filter by symbol, summary shape, no curve.
	resp, err := http.Get(srv.URL + "/v1/backtests?symbol=" + symbol)
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
	if len(list) != 1 {
		t.Fatalf("list len = %d; want 1 (body %v)", len(list), list)
	}
	if list[0]["id"] != float64(id) || list[0]["metrics"].(map[string]any)["equity"] != 10500.0 {
		t.Fatalf("list[0] = %v; want id %d with equity 10500", list[0], id)
	}

	// Detail: full trace matches what SaveResult wrote.
	resp, err = http.Get(srv.URL + "/v1/backtests/" + strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d; want 200", resp.StatusCode)
	}
	var detail struct {
		Metrics     map[string]any `json:"metrics"`
		EquityCurve []struct {
			Ts     string  `json:"ts"`
			Equity float64 `json:"equity"`
		} `json:"equity_curve"`
		Trades []struct {
			Action string  `json:"action"`
			Symbol string  `json:"symbol"`
			Size   float64 `json:"size"`
			Price  float64 `json:"price"`
		} `json:"trades"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.Metrics["equity"] != 10500.0 {
		t.Fatalf("detail metrics = %v; want equity 10500", detail.Metrics)
	}
	if len(detail.EquityCurve) != 2 || detail.EquityCurve[1].Equity != 10500 {
		t.Fatalf("detail curve = %+v; want 2 points ending 10500", detail.EquityCurve)
	}
	if len(detail.Trades) != 1 || detail.Trades[0].Action != "buy" || detail.Trades[0].Symbol != symbol {
		t.Fatalf("detail trades = %+v; want buy on %s", detail.Trades, symbol)
	}

	// Missing id: 404.
	resp, err = http.Get(srv.URL + "/v1/backtests/99999999")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing id status = %d; want 404", resp.StatusCode)
	}
}
