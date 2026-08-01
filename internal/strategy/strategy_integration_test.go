package strategy

// Integration tests require WBOT_PG_DSN (see .github/workflows/ci.yml job
// db-integration): real PG bars + option_quotes feed a covered-call run.

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/ingest"
)

const testSymbol = "STRAT.US"

func TestCoveredCallIntegration(t *testing.T) {
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

	if _, err := database.Exec(`DELETE FROM bars WHERE symbol = $1`, testSymbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM option_quotes WHERE underlying = $1`, testSymbol); err != nil {
		t.Fatal(err)
	}
	day := func(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }
	closes := []float64{100, 103, 99, 106, 104}
	for i, c := range closes {
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 100, 'none', 'futu')`, testSymbol, day(i), c); err != nil {
			t.Fatal(err)
		}
	}
	quotes := []struct {
		code   string
		opt    string
		strike float64
		expiry int
		closes []float64
	}{
		{"O105C", "call", 105, 2, []float64{3, 2.5, 1}},
		{"O110C", "call", 110, 4, []float64{1, 0.8, 0.5, 0.2}},
	}
	for _, q := range quotes {
		for i, c := range q.closes {
			if _, err := database.Exec(`
INSERT INTO option_quotes (symbol, underlying, option_type, strike, expiry, ts, open, high, low, close, volume, implied_vol, adjust, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $7, $7, 10, NULL, 'none', 'futu')`,
				q.code, testSymbol, q.opt, q.strike, day(q.expiry), day(i), c); err != nil {
				t.Fatal(err)
			}
		}
	}

	bars, err := ingest.QueryBars(ctx, database, testSymbol, "1d", "none", time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ingest.QueryOptionQuotes(ctx, database, testSymbol, "none", time.Time{}, time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := backtest.OptionsDataFromQuotes(rows)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Factory("covered-call", map[string]any{"strike_pct_otm": 0.05})
	if err != nil {
		t.Fatal(err)
	}
	run := func() *backtest.Result {
		res, err := backtest.RunOptions(ctx, bars, 10000, 0, s, opts)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// day 0 buy 100 @100; day 1 sell C105 (exp Jan 3) for 250; day 2 OTM void;
	// day 3 sell C110 (exp Jan 5) for 20; day 4 OTM void: cash 270 + 100*104.
	first := run()
	if math.Abs(first.Equity-10670) > 1e-9 || math.Abs(first.TotalReturn-0.067) > 1e-9 {
		t.Fatalf("run = %+v; want Equity 10670, TotalReturn 0.067", first)
	}
	// Determinism: a second run yields the identical summary.
	second := run()
	if first.Equity != second.Equity || first.TotalReturn != second.TotalReturn || first.Bars != second.Bars {
		t.Fatalf("runs differ: %+v vs %+v", first, second)
	}
}
