package backtest_test

// External-view tests for the unfilled-attempt model (S3): a real Wheel
// strategy run must never put HOLD/DATA_BLOCKED bars into the attempt
// denominator (the mapping lives in internal/strategy, which package backtest
// itself cannot import without a cycle).

import (
	"context"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/strategy"
)

func TestWheelDataBlockedNotAttempted(t *testing.T) {
	params := map[string]any{
		"price_position_curve": []any{
			map[string]any{"price": 90.0, "target_inventory": 100.0},
			map[string]any{"price": 110.0, "target_inventory": 100.0},
		},
		"max_inventory":      100.0,
		"min_option_quality": 0.0,
		"no_trade_gap":       0.0,
	}
	s, err := strategy.Factory("wheel", params)
	if err != nil {
		t.Fatalf("Factory(wheel) error: %v", err)
	}
	bars := []ingest.Bar{
		{Ts: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Open: 100, High: 100, Low: 100, Close: 100, Volume: 100},
		{Ts: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Open: 101, High: 101, Low: 101, Close: 101, Volume: 100},
	}
	// No quote snapshots: every bar is DATA_BLOCKED, the strategy must HOLD.
	res, err := backtest.RunOptions(context.Background(), bars, 10000, 0, s, nil)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if res.Unfilled.AttemptCount != 0 || res.Unfilled.FillCount != 0 || res.Unfilled.UnfilledCount != 0 {
		t.Fatalf("unfilled = %+v; want DATA_BLOCKED/HOLD bars to never enter the attempt denominator", res.Unfilled)
	}
	if res.Unfilled.UnfilledRatio != nil {
		t.Fatalf("ratio = %v; want nil (null) when no attempts", *res.Unfilled.UnfilledRatio)
	}
	if res.Unfilled.ModelKind != "heuristic" || res.Unfilled.ModelVersion != "heuristic-1.0" {
		t.Fatalf("model = %s/%s; want heuristic/heuristic-1.0", res.Unfilled.ModelKind, res.Unfilled.ModelVersion)
	}
	if len(res.Signals) != 2 || res.Signals[0].CapabilityStatus != "DATA_BLOCKED" {
		t.Fatalf("signals = %+v; want 2 DATA_BLOCKED bars", res.Signals)
	}
}
