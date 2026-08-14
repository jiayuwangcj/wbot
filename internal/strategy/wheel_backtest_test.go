package strategy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

func wheelBacktestConfig() wheel.Config {
	return wheel.Config{
		Strategy: "wheel", FullPositionPrice: 90, ZeroPositionPrice: 110,
		MaxInventory: 1000, MinDTE: 5, MaxDTE: 10, MinOptionQuality: 0,
		TradeGap: 50, StrategicState: wheel.StateNormal,
	}
}

func snapshotRow(ts time.Time, bid *float64) wheelstore.QuoteSnapshotRecord {
	delta, ask, iv, theta, underlying := -0.30, 2.10, 0.20, -0.10, 100.0
	volume, oi, lot := int64(1000), int64(2000), int64(100)
	return wheelstore.QuoteSnapshotRecord{
		Symbol: "P95", Underlying: "U.US", OptionType: "PUT", Strike: 95, Expiry: ts.AddDate(0, 0, 7),
		Source: "test", SnapshotKey: "batch-1", UnderlyingPrice: &underlying, Delta: &delta, Bid: bid, Ask: &ask, IV: &iv, Theta: &theta,
		Volume: &volume, OpenInterest: &oi, LotSize: &lot, ObservedAt: ts,
	}
}

func testBar(ts time.Time) ingest.Bar {
	return ingest.Bar{Ts: ts, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1}
}

func TestWheelBacktestSnapshotAlertAndSignalTrace(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bid := 2.0
	data, err := backtest.OptionsDataFromQuoteSnapshots([]wheelstore.QuoteSnapshotRecord{snapshotRow(ts, &bid)})
	if err != nil {
		t.Fatal(err)
	}
	s := &WheelStrategy{Config: wheelBacktestConfig()}
	res, err := backtest.RunOptions(context.Background(), []ingest.Bar{testBar(ts)}, 20000, 0, s, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Trades) != 1 || res.Trades[0].Action != "sell-put" || res.Trades[0].Symbol != "P95" {
		t.Fatalf("trades = %+v; want one sell-put P95", res.Trades)
	}
	if len(res.Signals) != 1 {
		t.Fatalf("signals = %+v; want one per bar", res.Signals)
	}
	sig := res.Signals[0]
	if sig.Action != wheel.ActionAlert || sig.Direction != wheel.DirectionPut || sig.CandidateCode != "P95" || sig.Quantity != 1 || sig.Reason == "" || sig.CapabilityStatus != wheel.CapabilityReady || sig.SnapshotKey != "batch-1" || sig.SnapshotObservedAt == nil || !sig.SnapshotObservedAt.Equal(ts) {
		t.Fatalf("signal = %+v; want ALERT PUT P95 qty 1 with reason", sig)
	}
}

func TestWheelBacktestMissingAndStaleSnapshotHold(t *testing.T) {
	for name, tc := range map[string]struct {
		row   wheelstore.QuoteSnapshotRecord
		barTS time.Time
	}{
		"missing bid": {row: snapshotRow(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), nil), barTS: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		"stale":       {row: snapshotRow(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), func() *float64 { x := 2.0; return &x }()), barTS: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := backtest.OptionsDataFromQuoteSnapshots([]wheelstore.QuoteSnapshotRecord{tc.row})
			if err != nil {
				t.Fatal(err)
			}
			cfg := wheelBacktestConfig()
			cfg.MaxQuoteAgeSeconds = 3600
			s := &WheelStrategy{Config: cfg}
			res, err := backtest.RunOptions(context.Background(), []ingest.Bar{testBar(tc.barTS)}, 20000, 0, s, data)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Trades) != 0 || len(res.Signals) != 1 || res.Signals[0].Action != wheel.ActionHold || res.Signals[0].CapabilityStatus != wheel.CapabilityDataBlocked || len(res.Signals[0].BlockedBy) == 0 {
				t.Fatalf("trades=%+v signals=%+v; want safe HOLD", res.Trades, res.Signals)
			}
		})
	}
}

func TestWheelBacktestStockSwitchSuggestionExecutesStockTrade(t *testing.T) {
	// 急涨急跌直接买卖正股(wheel 既有机制):stock_switch_pct 触发时 Evaluate
	// 只给正股建议,线下人工处置;回测将其机械化为正股买卖,不能 HOLD 掉。
	// 上次有效成交价来自 bar 0 的卖 put 成交(适配器经 FillCount 增量追踪)。
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bid := 2.0
	row := func(day int, underlying float64) wheelstore.QuoteSnapshotRecord {
		r := snapshotRow(ts.Add(time.Duration(day)*24*time.Hour), &bid)
		r.UnderlyingPrice = &underlying
		return r
	}
	// 急跌日 underlying 94.6(-5.4% ≥ stock_switch 5%);急涨日 underlying 200。
	quotes := []wheelstore.QuoteSnapshotRecord{row(0, 100), row(1, 94.6), row(2, 200)}
	data, err := backtest.OptionsDataFromQuoteSnapshots(quotes)
	if err != nil {
		t.Fatal(err)
	}
	cfg := wheelBacktestConfig()
	cfg.FullPositionPrice, cfg.ZeroPositionPrice, cfg.MaxInventory, cfg.TradeGap = 100, 200, 100, 0
	cfg.StockSwitchPct = 0.05
	bar := func(day int, close float64) ingest.Bar {
		return ingest.Bar{Ts: ts.Add(time.Duration(day) * 24 * time.Hour), Open: close, High: close, Low: close, Close: close, Volume: 1}
	}
	res, err := backtest.RunOptions(context.Background(),
		[]ingest.Bar{bar(0, 100), bar(1, 94.6), bar(2, 200)}, 20000, 0, &WheelStrategy{Config: cfg}, data)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, tr := range res.Trades {
		actions = append(actions, tr.Action)
	}
	want := []string{"sell-put", "buy", "sell"}
	if len(actions) != len(want) {
		t.Fatalf("trades = %v; want %v (stock switch must execute, not HOLD)", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("trades = %v; want %v", actions, want)
		}
	}
	if res.Trades[1].Size != 70 { // 目标 100 − 期权 delta 库存 30
		t.Fatalf("buy size = %v; want 70 (gap after option delta)", res.Trades[1].Size)
	}
	// 急涨卖出:建议量 100 含 30 期权 delta 折算,实际持仓 70 → 只卖 70(不裸卖)。
	if res.Trades[2].Size != 70 || res.Terminal.StockShares != 0 {
		t.Fatalf("sell size = %v, terminal shares = %v; want 70 / 0 (clamped, no naked short)", res.Trades[2].Size, res.Terminal.StockShares)
	}
}

func TestWheelBacktestStockSwitchKeepsStockBelowCost(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bid := 2.0
	row := snapshotRow(ts, &bid)
	underlying := 100.0
	row.UnderlyingPrice = &underlying
	data, err := backtest.OptionsDataFromQuoteSnapshots([]wheelstore.QuoteSnapshotRecord{row})
	if err != nil {
		t.Fatal(err)
	}
	cfg := wheelBacktestConfig()
	cfg.FullPositionPrice, cfg.ZeroPositionPrice, cfg.MaxInventory, cfg.TradeGap = 50, 90, 100, 0
	cfg.StockSwitchPct = 0.05
	strategy := &WheelStrategy{Config: cfg, lastFillPrice: 110}
	state := &backtest.State{Cash: 10000, Position: 100, StockAverageCost: 120, Options: map[string]backtest.OptionPosition{}, QuoteBatch: &data.QuoteBatches[0]}
	action, size, err := strategy.OnBar(context.Background(), testBar(ts), state)
	if err != nil || action != backtest.ActionHold || size != 0 || !strings.Contains(strategy.LastSignal.Reason, "below average cost") {
		t.Fatalf("action=%v size=%v signal=%+v err=%v; want protected HOLD", action, size, strategy.LastSignal, err)
	}
}

func TestWheelSnapshotBatchSortingAndAtomicSelection(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bid := 2.0
	newer := snapshotRow(ts.Add(24*time.Hour), &bid)
	newer.SnapshotKey = "z"
	older := snapshotRow(ts, &bid)
	older.SnapshotKey = "a"
	data, err := backtest.OptionsDataFromQuoteSnapshots([]wheelstore.QuoteSnapshotRecord{newer, older})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.QuoteBatches) != 2 || data.QuoteBatches[0].SnapshotKey != "a" || data.QuoteBatches[1].SnapshotKey != "z" {
		t.Fatalf("batches = %+v; want observed_at/snapshot_key order", data.QuoteBatches)
	}
	res, err := backtest.RunOptions(context.Background(), []ingest.Bar{testBar(ts.Add(24 * time.Hour))}, 20000, 0, &WheelStrategy{Config: wheelBacktestConfig()}, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Signals) != 1 || res.Signals[0].CandidateCode != "P95" {
		t.Fatalf("signals = %+v; want latest one atomic batch", res.Signals)
	}
}
