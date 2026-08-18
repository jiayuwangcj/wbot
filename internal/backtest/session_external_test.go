package backtest_test

// Session chunked-vs-single equivalence (external package so it can exercise
// the real WheelStrategy): the same bars/rows fed through one Process call must
// produce a byte-identical Result to the same data fed chunk by chunk, with
// overlapping snapshot lookbacks and the shared portfolio/strategy carried
// across chunks. This is the determinism iron rule behind `wbot backtest
// -chunk` (doc/BACKTEST.md).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/strategy"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

func sessionTestConfig() wheel.Config {
	return wheel.Config{
		Strategy: "wheel", FullPositionPrice: 90, ZeroPositionPrice: 110,
		MaxInventory: 1000, MinDTE: 5, MaxDTE: 30, MinOptionQuality: 0,
		TradeGap: 50, StrategicState: wheel.StateNormal,
	}
}

func sessionTestQuote(ts, expiry time.Time, sym string, strike float64, kind string) wheelstore.QuoteSnapshotRecord {
	bid := 2.0
	delta := -0.30
	if kind == "CALL" {
		delta = 0.30
	}
	ask, iv, theta, underlying := 2.10, 0.20, -0.10, 100.0
	volume, oi, lot := int64(1000), int64(2000), int64(100)
	return wheelstore.QuoteSnapshotRecord{
		Symbol: sym, Underlying: "U.US", OptionType: kind, Strike: strike, Expiry: expiry,
		Source: "test", SnapshotKey: "batch-1", UnderlyingPrice: &underlying, Delta: &delta,
		Bid: &bid, Ask: &ask, IV: &iv, Theta: &theta,
		Volume: &volume, OpenInterest: &oi, LotSize: &lot, ObservedAt: ts,
	}
}

func sessionTestData(days int) ([]ingest.Bar, []wheelstore.QuoteSnapshotRecord, time.Time) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := start.AddDate(0, 0, 30)
	var bars []ingest.Bar
	var rows []wheelstore.QuoteSnapshotRecord
	for day := 0; day < days; day++ {
		ts := start.AddDate(0, 0, day)
		bars = append(bars, ingest.Bar{Ts: ts, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1, Source: "test", Adjusted: "none"})
		rows = append(rows,
			sessionTestQuote(ts, expiry, "P95", 95, "PUT"),
			sessionTestQuote(ts, expiry, "P90", 90, "PUT"),
			sessionTestQuote(ts, expiry, "C105", 105, "CALL"),
		)
	}
	return bars, rows, start
}

func runSessionSingle(t *testing.T, bars []ingest.Bar, rows []wheelstore.QuoteSnapshotRecord, cfg wheel.Config, cash float64) *backtest.Result {
	t.Helper()
	opts, err := backtest.OptionsDataFromQuoteSnapshots(rows)
	if err != nil {
		t.Fatal(err)
	}
	s := &strategy.WheelStrategy{Config: cfg}
	sess, err := backtest.NewSession(cash, backtest.LegacyFeeModel(0), 0, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Process(context.Background(), bars, opts, time.Time{}); err != nil {
		t.Fatal(err)
	}
	res, err := sess.Result(cash, bars, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func runSessionChunked(t *testing.T, bars []ingest.Bar, rows []wheelstore.QuoteSnapshotRecord, cfg wheel.Config, cash float64, chunkDays int, lookback time.Duration) *backtest.Result {
	t.Helper()
	s := &strategy.WheelStrategy{Config: cfg}
	sess, err := backtest.NewSession(cash, backtest.LegacyFeeModel(0), 0, s)
	if err != nil {
		t.Fatal(err)
	}
	ownedFrom := time.Time{}
	for start := 0; start < len(bars); start += chunkDays {
		end := start + chunkDays
		if end > len(bars) {
			end = len(bars)
		}
		chunkBars := bars[start:end]
		chunkFrom := chunkBars[0].Ts.Add(-lookback)
		chunkTo := chunkBars[len(chunkBars)-1].Ts
		var chunkRows []wheelstore.QuoteSnapshotRecord
		for _, r := range rows {
			if !r.ObservedAt.Before(chunkFrom) && !r.ObservedAt.After(chunkTo) {
				chunkRows = append(chunkRows, r)
			}
		}
		var opts *backtest.OptionsData
		if len(chunkRows) > 0 {
			opts, err = backtest.OptionsDataFromQuoteSnapshots(chunkRows)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := sess.Process(context.Background(), chunkBars, opts, ownedFrom); err != nil {
			t.Fatal(err)
		}
		ownedFrom = chunkBars[0].Ts
	}
	res, err := sess.Result(cash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSessionChunkedMatchesSingle(t *testing.T) {
	const cash = 20000.0
	bars, rows, _ := sessionTestData(90)
	single := runSessionSingle(t, bars, rows, sessionTestConfig(), cash)
	for _, chunkDays := range []int{3, 7, 30} {
		chunked := runSessionChunked(t, bars, rows, sessionTestConfig(), cash, chunkDays, 24*time.Hour)
		a, _ := json.Marshal(single)
		b, _ := json.Marshal(chunked)
		if string(a) != string(b) {
			t.Fatalf("chunkDays=%d: chunked Result differs from single\n single trades=%d\n chunk trades=%d\n", chunkDays, len(single.Trades), len(chunked.Trades))
		}
	}
}

func TestSessionChunkedMatchesSingleNoOptionData(t *testing.T) {
	const cash = 20000.0
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var bars []ingest.Bar
	for day := 0; day < 10; day++ {
		ts := start.AddDate(0, 0, day)
		bars = append(bars, ingest.Bar{Ts: ts, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1, Source: "test", Adjusted: "none"})
	}
	cfg := sessionTestConfig()
	s := &strategy.WheelStrategy{Config: cfg}
	sess, err := backtest.NewSession(cash, backtest.LegacyFeeModel(0), 0, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Process(context.Background(), bars, nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	single, err := sess.Result(cash, bars, nil)
	if err != nil {
		t.Fatal(err)
	}

	s2 := &strategy.WheelStrategy{Config: cfg}
	sess2, err := backtest.NewSession(cash, backtest.LegacyFeeModel(0), 0, s2)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess2.Process(context.Background(), bars[:5], nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := sess2.Process(context.Background(), bars[5:], nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	chunked, err := sess2.Result(cash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(single)
	b, _ := json.Marshal(chunked)
	if string(a) != string(b) {
		t.Fatalf("chunked (no option data) differs from single:\n%+v\n%+v", single, chunked)
	}
}
