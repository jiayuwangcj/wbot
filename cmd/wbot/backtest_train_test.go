package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/backtestes"
	"github.com/jiayu/wbot/internal/backtestexec"
	"github.com/jiayu/wbot/internal/db"
)

func TestRequireTrainCoverageFailsFast(t *testing.T) {
	one := 1.0
	zero := 0.0
	filled := []backtest.Trade{{Action: "sell-put", Filled: true}}
	tests := []struct {
		name     string
		outcome  *backtestexec.Outcome
		wantFail bool
	}{
		{name: "nil outcome", wantFail: true},
		{name: "zero coverage", outcome: &backtestexec.Outcome{Result: &backtest.Result{Trades: filled, DataQuality: backtest.DataQualitySummary{ValidCoverageRatio: &zero}}}, wantFail: true},
		{name: "zero effective trades", outcome: &backtestexec.Outcome{Result: &backtest.Result{DataQuality: backtest.DataQualitySummary{ValidCoverageRatio: &one}}}, wantFail: true},
		{name: "covered with fill", outcome: &backtestexec.Outcome{Result: &backtest.Result{Trades: filled, DataQuality: backtest.DataQualitySummary{ValidCoverageRatio: &one}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireTrainCoverage(tt.outcome, "HK.00700", 10000)
			if tt.wantFail {
				if !errors.Is(err, backtestexec.ErrNoOptionData) {
					t.Fatalf("error = %v; want ErrNoOptionData", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecommendableCandidateRejectsZeroTradeNegativeBaseline(t *testing.T) {
	if recommendableCandidate([]backtestes.Metrics{{NetReturn: 0}, {NetReturn: 0}}, -.1) {
		t.Fatal("zero-trade candidate beat a negative baseline")
	}
	if recommendableCandidate([]backtestes.Metrics{{NetReturn: .02, EffectiveTrades: 1}, {NetReturn: .03}}, -.1) {
		t.Fatal("candidate with one zero-trade seed was recommendable")
	}
	if !recommendableCandidate([]backtestes.Metrics{{NetReturn: .02, EffectiveTrades: 1}, {NetReturn: .03, EffectiveTrades: 2}}, .01) {
		t.Fatal("stable filled candidate was rejected")
	}
}

func TestBacktestTrainZeroCoverageIntegration(t *testing.T) {
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
	const symbol = "TRAINZERO.US"
	cleanup := func() {
		_, _ = database.Exec(`DELETE FROM option_quote_snapshots WHERE underlying = $1`, symbol)
		_, _ = database.Exec(`DELETE FROM bars WHERE symbol = $1`, symbol)
	}
	cleanup()
	defer cleanup()
	for i, closePrice := range []float64{100, 99, 98, 97, 96} {
		ts := time.Date(2026, 8, 3+i, 0, 0, 0, 0, time.UTC)
		if _, err := database.Exec(`
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, adjust, source)
VALUES ($1, '1d', $2, $3, $3, $3, $3, 1000, 'none', 'train-zero-test')`, symbol, ts, closePrice); err != nil {
			t.Fatal(err)
		}
	}
	reportDir := t.TempDir()
	stdout, stderr, code := captureRun(t, []string{
		"wbot", "backtest", "-dsn", dsn, "-symbol", symbol, "-timeframe", "1d", "-adjust", "none",
		"-strategy", "wheel", "-params", `{"full_position_price":90,"zero_position_price":110,"max_inventory":100}`,
		"-train", `{"move_interval_pct":[0.005,0.03]}`, "-population", "16", "-budget", "32", "-report", "-report-dir", reportDir,
	})
	if code != 1 || !strings.Contains(stderr, backtestexec.ErrNoOptionData.Error()) || !strings.Contains(stderr, "valid_coverage=0.00%") || !strings.Contains(stderr, "effective_trades=0") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "预计评估次数=") || strings.Contains(stdout, "train_stop=") {
		t.Fatalf("training did not fail at probe: %q", stdout)
	}
	if matches, _ := filepath.Glob(filepath.Join(reportDir, "*.json")); len(matches) != 0 {
		t.Fatalf("zero-coverage training generated reports: %v", matches)
	}
}

const space13 = `{"move_interval_pct":[0.005,0.03],"min_premium_per_share":[0.005,0.05],"min_option_profit":[0.5,5],"stock_switch_pct":[0.005,0.05],"covered_call_pct":[0,0.3],"trade_gap":[0,20],"min_option_quality":[0.3,0.9],"min_dte":[7,30],"max_dte":[30,45],"profit_take_pct":[0.1,0.8],"put_delta_max":[0.1,0.5],"call_delta_max":[0.1,0.5],"min_iv_rank":[0,0.8]}`

var candidate13 = map[string]any{
	"move_interval_pct": 0.012, "min_premium_per_share": 0.02, "min_option_profit": 1.2, "stock_switch_pct": 0.01,
	"covered_call_pct": 0.0118, "trade_gap": 5.0, "min_option_quality": 0.6, "min_dte": 13, "max_dte": 43,
	"profit_take_pct": 0.7, "put_delta_max": 0.353, "call_delta_max": 0.2504, "min_iv_rank": 0.0,
	"full_position_price": 400, "zero_position_price": 600, "max_inventory": 1200,
}

func TestTacticalParamsCoversFull13KeySpace(t *testing.T) {
	space, err := backtestes.ParseSpace(space13, map[string]any{"min_dte": 13, "max_dte": 43})
	if err != nil {
		t.Fatal(err)
	}
	params := tacticalParams(candidate13, space)
	want := []string{"move_interval_pct", "min_premium_per_share", "min_option_profit", "stock_switch_pct", "covered_call_pct", "trade_gap", "min_option_quality", "min_dte", "max_dte", "profit_take_pct", "put_delta_max", "call_delta_max", "min_iv_rank"}
	if len(params) != len(want) {
		t.Fatalf("params keys = %d; want %d: %v", len(params), len(want), params)
	}
	for _, k := range want {
		if _, ok := params[k]; !ok {
			t.Fatalf("params missing %q: %v", k, params)
		}
	}
	if params["profit_take_pct"] != 0.7 || params["put_delta_max"] != 0.353 || params["call_delta_max"] != 0.2504 || params["min_iv_rank"] != 0.0 {
		t.Fatalf("new key values lost: %v", params)
	}
	if _, ok := params["full_position_price"]; ok {
		t.Fatalf("strategic key leaked into candidate params: %v", params)
	}
}

func TestBoundaryHitsCoversNewKeys(t *testing.T) {
	space, err := backtestes.ParseSpace(space13, map[string]any{"min_dte": 13, "max_dte": 43})
	if err != nil {
		t.Fatal(err)
	}
	at := map[string]any{
		"move_interval_pct": 0.005, "min_premium_per_share": 0.02, "min_option_profit": 1.2, "stock_switch_pct": 0.01,
		"covered_call_pct": 0.0118, "trade_gap": 5.0, "min_option_quality": 0.6, "min_dte": 13, "max_dte": 43,
		"profit_take_pct": 0.8, "put_delta_max": 0.1, "call_delta_max": 0.2504, "min_iv_rank": 0.0,
	}
	hits := boundaryHits(space, at)
	for _, k := range []string{"profit_take_pct", "put_delta_max", "min_iv_rank"} {
		if !hits[k] {
			t.Fatalf("boundary hit on %s not counted: %v", k, hits)
		}
	}
	if hits["call_delta_max"] {
		t.Fatalf("non-boundary key falsely hit: %v", hits)
	}
	without := map[string]any{}
	for k, v := range at {
		if k != "min_iv_rank" {
			without[k] = v
		}
	}
	if boundaryHits(space, without)["min_iv_rank"] {
		t.Fatal("missing key falsely hit its zero boundary")
	}
}

func TestTacticalParamsLegacyNineKeySpaceRegression(t *testing.T) {
	raw := `{"move_interval_pct":[0.005,0.03],"min_premium_per_share":[0.005,0.05],"min_option_profit":[0.5,5],"stock_switch_pct":[0.005,0.05],"covered_call_pct":[0,0.3],"trade_gap":[0,20],"min_option_quality":[0.3,0.9],"min_dte":[7,30],"max_dte":[30,45]}`
	space, err := backtestes.ParseSpace(raw, map[string]any{"min_dte": 13, "max_dte": 43})
	if err != nil {
		t.Fatal(err)
	}
	params := tacticalParams(candidate13, space)
	if len(params) != 9 {
		t.Fatalf("9-key space yielded %d keys: %v", len(params), params)
	}
	for _, k := range []string{"profit_take_pct", "put_delta_max", "call_delta_max", "min_iv_rank"} {
		if _, ok := params[k]; ok {
			t.Fatalf("new key %q leaked into 9-key space report: %v", k, params)
		}
	}
}
