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

// buybackRow is a decaying premium quote for the held short put: bid shrinks
// daily so the captured ratio (received − ask)/received climbs to the exit.
func buybackRow(ts time.Time, bid, ask float64) wheelstore.QuoteSnapshotRecord {
	r := snapshotRow(ts, &bid)
	r.Ask = &ask
	return r
}

func TestWheelBacktestProfitTakeClosesShortLeg(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []ingest.Bar{testBar(ts), testBar(ts.Add(24 * time.Hour)), testBar(ts.Add(48 * time.Hour))}
	bid2 := 2.0
	rows := []wheelstore.QuoteSnapshotRecord{
		snapshotRow(ts, &bid2),                     // sell at 2.00
		buybackRow(ts.Add(24*time.Hour), 1.6, 1.5), // ratio 0.25: decay not reached
		buybackRow(ts.Add(48*time.Hour), 1.0, 1.0), // ratio 0.50: exit threshold
	}
	data, err := backtest.OptionsDataFromQuoteSnapshots(rows)
	if err != nil {
		t.Fatal(err)
	}
	cfg := wheelBacktestConfig()
	cfg.ProfitTakePct = 0.5
	cfg.TradeGap = 470 // after one short put the gap (500−30) sits inside the band
	res, err := backtest.RunOptions(context.Background(), bars, 20000, 0, &WheelStrategy{Config: cfg}, data)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, tr := range res.Trades {
		actions = append(actions, tr.Action)
	}
	if len(actions) != 2 || actions[0] != "sell-put" || actions[1] != "buy-put" {
		t.Fatalf("trades = %v; want [sell-put buy-put]", actions)
	}
	closeTrade := res.Trades[1]
	if closeTrade.Symbol != "P95" || closeTrade.Size != 1 {
		t.Fatalf("close trade = %+v; want P95 qty 1", closeTrade)
	}
	if got, want := res.Attribution.OptionCloseCostAmount, 100.0; got != want {
		t.Fatalf("OptionCloseCost = %v; want %v (1 lot × mark 1.00)", got, want)
	}
	if got, want := res.Attribution.PremiumIncomeAmount, 200.0; got != want {
		t.Fatalf("PremiumIncome = %v; want %v", got, want)
	}
	if got := res.Terminal.OpenOptionLegCount; got != 0 {
		t.Fatalf("terminal open legs = %v; want closed flat", got)
	}
	// The buy-back bar must carry the exit signal contract, not a candidate.
	last := res.Signals[len(res.Signals)-1]
	if last.Action != wheel.ActionAlert || !strings.Contains(last.Reason, "profit_take_pct") || last.CandidateCode != "P95" {
		t.Fatalf("last signal = %+v; want profit_take_pct ALERT", last)
	}
}

func TestWheelBacktestProfitTakeHoldsWithoutDecay(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	row := func(day int) wheelstore.QuoteSnapshotRecord {
		return buybackRow(ts.Add(time.Duration(day)*24*time.Hour), 2.0, 2.1)
	}
	data, err := backtest.OptionsDataFromQuoteSnapshots([]wheelstore.QuoteSnapshotRecord{row(0), row(1), row(2)})
	if err != nil {
		t.Fatal(err)
	}
	cfg := wheelBacktestConfig()
	cfg.ProfitTakePct = 0.5
	cfg.TradeGap = 470
	res, err := backtest.RunOptions(context.Background(),
		[]ingest.Bar{testBar(ts), testBar(ts.Add(24 * time.Hour)), testBar(ts.Add(48 * time.Hour))},
		20000, 0, &WheelStrategy{Config: cfg}, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Trades) != 1 || res.Trades[0].Action != "sell-put" {
		t.Fatalf("trades = %+v; want only the opening sell-put (ask never decays below basis)", res.Trades)
	}
	if got := res.Attribution.OptionCloseCostAmount; got != 0 {
		t.Fatalf("OptionCloseCost = %v; want 0 without exit", got)
	}
}

func TestWheelBacktestDeltaCapFiltersCandidates(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		delta    float64
		putDelta float64
		wantSell bool
	}{
		"deep delta blocked":  {delta: -0.45, putDelta: 0.30, wantSell: false},
		"delta under cap":     {delta: -0.25, putDelta: 0.30, wantSell: true},
		"cap off allows deep": {delta: -0.45, putDelta: 0, wantSell: true},
	} {
		t.Run(name, func(t *testing.T) {
			bid := 2.0
			r := snapshotRow(ts, &bid)
			r.Delta = &tc.delta
			data, err := backtest.OptionsDataFromQuoteSnapshots([]wheelstore.QuoteSnapshotRecord{r})
			if err != nil {
				t.Fatal(err)
			}
			cfg := wheelBacktestConfig()
			cfg.PutDeltaMax = tc.putDelta
			res, err := backtest.RunOptions(context.Background(), []ingest.Bar{testBar(ts)}, 20000, 0, &WheelStrategy{Config: cfg, TraceCandidates: true}, data)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(res.Trades) == 1 && res.Trades[0].Action == "sell-put"; got != tc.wantSell {
				t.Fatalf("trades = %+v; want sell=%v", res.Trades, tc.wantSell)
			}
			if !tc.wantSell && len(res.Signals) == 1 {
				var reasons []string
				for _, c := range res.Signals[0].CandidateDetails {
					reasons = append(reasons, c.Reasons...)
				}
				if !strings.Contains(strings.Join(reasons, " "), "delta") {
					t.Fatalf("signal = %+v; want delta cap reject reason", res.Signals[0])
				}
			}
		})
	}
}

func ivRow(ts time.Time, iv float64) wheelstore.QuoteSnapshotRecord {
	bid := 2.0
	r := snapshotRow(ts, &bid)
	r.IV = &iv
	return r
}

func TestWheelBacktestIVRankGate(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var rows []wheelstore.QuoteSnapshotRecord
	var bars []ingest.Bar
	for day := 0; day < 19; day++ { // 19 days: window < 20 → unknown rank
		rows = append(rows, ivRow(ts.Add(time.Duration(day)*24*time.Hour), 0.20))
		bars = append(bars, testBar(ts.Add(time.Duration(day)*24*time.Hour)))
	}
	rows = append(rows, ivRow(ts.Add(19*24*time.Hour), 0.30)) // rank 1.0: high IV day
	bars = append(bars, testBar(ts.Add(19*24*time.Hour)))
	rows = append(rows, ivRow(ts.Add(20*24*time.Hour), 0.05)) // rank 1/21: low IV day
	bars = append(bars, testBar(ts.Add(20*24*time.Hour)))
	data, err := backtest.OptionsDataFromQuoteSnapshots(rows)
	if err != nil {
		t.Fatal(err)
	}
	cfg := wheelBacktestConfig()
	cfg.MinIVRank = 0.5
	res, err := backtest.RunOptions(context.Background(), bars, 20000, 0, &WheelStrategy{Config: cfg}, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Signals) != 21 {
		t.Fatalf("signals = %d; want 21", len(res.Signals))
	}
	if s := res.Signals[0]; s.Action != wheel.ActionHold || !strings.Contains(s.Reason, "IV rank is unavailable") {
		t.Fatalf("day-0 signal = %+v; want HOLD with unavailable IV rank", s)
	}
	if s := res.Signals[19]; s.Action != wheel.ActionAlert {
		t.Fatalf("high-IV day signal = %+v; want ALERT (rank 1.0 ≥ 0.5)", s)
	}
	if s := res.Signals[20]; s.Action != wheel.ActionHold || !strings.Contains(s.Reason, "IV rank") {
		// 严格小于百分位:rank=0(已知但最低)或 unavailable 均走 IV 闸门 HOLD。
		t.Fatalf("low-IV day signal = %+v; want HOLD with IV rank gate reason", s)
	}
}
