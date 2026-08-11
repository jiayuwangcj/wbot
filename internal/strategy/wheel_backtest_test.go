package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

func wheelBacktestConfig() wheel.Config {
	return wheel.Config{
		Strategy: "wheel", PricePositionCurve: []wheel.PricePoint{{Price: 90, TargetInventory: 1000}, {Price: 110, TargetInventory: 0}},
		MaxInventory: 1000, LotSize: 100, MinDTE: 5, MaxDTE: 10, MinOptionQuality: 0,
		MaxDailyOrders: 1, ExtremeMaxDailyOrders: 2, NoTradeGap: 50, StrategicState: wheel.StateNormal,
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
