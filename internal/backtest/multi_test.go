package backtest

// Deterministic unit tests for the multi-symbol runner (RunMulti): alignment
// by ts intersection, equal cash split, per-symbol valuation, validation and
// state isolation between sub-accounts.

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// barAt builds one daily bar at day offset from 2024-01-01 with equal OHLC.
func barAt(dayOffset int, close float64) ingest.Bar {
	return ingest.Bar{
		Ts:     time.Date(2024, 1, 1+dayOffset, 0, 0, 0, 0, time.UTC),
		Open:   close,
		High:   close,
		Low:    close,
		Close:  close,
		Volume: 100,
	}
}

// mkSeries builds one symbol's contiguous daily bars starting at day offset.
func mkSeries(symbol string, startOffset int, closes []float64) SymbolBars {
	bars := make([]ingest.Bar, 0, len(closes))
	for i, c := range closes {
		bars = append(bars, barAt(startOffset+i, c))
	}
	return SymbolBars{Symbol: symbol, Bars: bars}
}

// buyHoldFactory returns a fresh buy-hold strategy per call.
func buyHoldFactory() StrategyFactory {
	return func() (Strategy, error) { return &BuyHoldStrategy{}, nil }
}

func TestRunMultiIntersection(t *testing.T) {
	// A spans d1..d5, B spans d2..d6 with d4 missing: intersection = {d2,d3,d5}.
	a := mkSeries("A.US", 0, []float64{100, 101, 102, 103, 104})
	b := SymbolBars{Symbol: "B.US", Bars: []ingest.Bar{barAt(1, 200), barAt(2, 201), barAt(4, 203), barAt(5, 204)}}
	res, err := RunMulti(context.Background(), []SymbolBars{a, b}, 10000, 0, buyHoldFactory())
	if err != nil {
		t.Fatalf("RunMulti() error: %v", err)
	}
	if res.Bars != 3 {
		t.Fatalf("RunMulti().Bars = %d; want 3 (intersection {d2,d3,d5})", res.Bars)
	}
	wantTs := []time.Time{
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	for i, ts := range wantTs {
		if !res.EquityCurve[i].Ts.Equal(ts) {
			t.Fatalf("RunMulti().EquityCurve[%d].Ts = %v; want %v", i, res.EquityCurve[i].Ts, ts)
		}
	}
	// Each sub-account buys at its first aligned bar (d2) and holds to d5:
	// A: 5000 @101 -> 5000*104/101; B: 5000 @200 -> 5000*203/200.
	want := 5000*104.0/101 + 5000*203.0/200
	if math.Abs(res.Equity-want) > 1e-9 {
		t.Fatalf("RunMulti().Equity = %v; want %v", res.Equity, want)
	}
	if len(res.PerSymbol) != 2 {
		t.Fatalf("RunMulti().PerSymbol len = %d; want 2", len(res.PerSymbol))
	}
	if res.PerSymbol[0].Symbol != "A.US" || res.PerSymbol[1].Symbol != "B.US" {
		t.Fatalf("RunMulti().PerSymbol order = %+v; want input order A.US,B.US", res.PerSymbol)
	}
	// Sub-accounts replay only the aligned bars (3, not the full series).
	for _, sub := range res.PerSymbol {
		if sub.Result.Bars != 3 {
			t.Fatalf("RunMulti().PerSymbol[%s].Bars = %d; want 3", sub.Symbol, sub.Result.Bars)
		}
	}
}

func TestRunMultiEqualWeightValuation(t *testing.T) {
	// Same window, different prices: each account gets cash/2 and is valued at
	// its own symbol's closes.
	a := mkSeries("A.US", 0, []float64{100, 110, 121})
	b := mkSeries("B.US", 0, []float64{200, 210, 220.5})
	res, err := RunMulti(context.Background(), []SymbolBars{a, b}, 10000, 0, buyHoldFactory())
	if err != nil {
		t.Fatalf("RunMulti() error: %v", err)
	}
	want := 5000*121.0/100 + 5000*220.5/200 // 6050 + 5512.5
	if math.Abs(res.Equity-want) > 1e-9 {
		t.Fatalf("RunMulti().Equity = %v; want %v", res.Equity, want)
	}
	if math.Abs(res.TotalReturn-(want-10000)/10000) > 1e-9 {
		t.Fatalf("RunMulti().TotalReturn = %v; want %v", res.TotalReturn, (want-10000)/10000)
	}
	if res.MaxDrawdown != 0 || res.Bars != 3 {
		t.Fatalf("RunMulti() = %+v; want MaxDrawdown 0, Bars 3", res)
	}
	// Equal split visible in the buys: 5000/100 = 50 and 5000/200 = 25 shares.
	if got := res.PerSymbol[0].Result.Trades[0]; got.Action != "buy" || math.Abs(got.Size-50) > 1e-9 {
		t.Fatalf("RunMulti().PerSymbol[A.US] buy = %+v; want 50 shares", got)
	}
	if got := res.PerSymbol[1].Result.Trades[0]; got.Action != "buy" || math.Abs(got.Size-25) > 1e-9 {
		t.Fatalf("RunMulti().PerSymbol[B.US] buy = %+v; want 25 shares", got)
	}
	// Valuation is per-symbol: each sub-account's final equity depends only on
	// its own closes (B's prices never touch A's account).
	if math.Abs(res.PerSymbol[0].Result.Equity-6050) > 1e-9 {
		t.Fatalf("RunMulti().PerSymbol[A.US].Equity = %v; want 6050", res.PerSymbol[0].Result.Equity)
	}
	if math.Abs(res.PerSymbol[1].Result.Equity-5512.5) > 1e-9 {
		t.Fatalf("RunMulti().PerSymbol[B.US].Equity = %v; want 5512.5", res.PerSymbol[1].Result.Equity)
	}
	// Combined curve is the pointwise sum: 10000, 10750, 11562.5.
	for i, wantEq := range []float64{10000, 10750, 11562.5} {
		if math.Abs(res.EquityCurve[i].Equity-wantEq) > 1e-9 {
			t.Fatalf("RunMulti().EquityCurve[%d].Equity = %v; want %v", i, res.EquityCurve[i].Equity, wantEq)
		}
	}
}

func TestRunMultiThreeSymbols(t *testing.T) {
	// B starts a day later, C two days later: intersection {d3,d4,d5}.
	a := mkSeries("A.US", 0, []float64{10, 11, 12, 13, 14})
	b := mkSeries("B.US", 1, []float64{20, 21, 22, 23})
	c := mkSeries("C.US", 2, []float64{30, 31, 32})
	res, err := RunMulti(context.Background(), []SymbolBars{a, b, c}, 10000, 0, func() (Strategy, error) {
		return HoldStrategy{}, nil
	})
	if err != nil {
		t.Fatalf("RunMulti() error: %v", err)
	}
	if res.Bars != 3 || len(res.PerSymbol) != 3 {
		t.Fatalf("RunMulti() = %+v; want Bars 3, 3 sub-accounts", res)
	}
	// Hold: every account keeps cash/3, so the combined equity is flat.
	if math.Abs(res.Equity-10000) > 1e-9 || res.TotalReturn != 0 || res.MaxDrawdown != 0 {
		t.Fatalf("RunMulti() = %+v; want flat equity 10000", res)
	}
}

func TestRunMultiFreshStrategyPerAccount(t *testing.T) {
	// Stateful buy-hold must get a fresh instance per account, or only the
	// first sub-account would buy (bought flag leaks across runs).
	calls := 0
	res, err := RunMulti(context.Background(),
		[]SymbolBars{mkSeries("A.US", 0, []float64{100, 110}), mkSeries("B.US", 0, []float64{50, 55})},
		10000, 0, func() (Strategy, error) {
			calls++
			return &BuyHoldStrategy{}, nil
		})
	if err != nil {
		t.Fatalf("RunMulti() error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("factory calls = %d; want 2 (one per sub-account)", calls)
	}
	for _, sub := range res.PerSymbol {
		if len(sub.Result.Trades) != 1 || sub.Result.Trades[0].Action != "buy" {
			t.Fatalf("RunMulti().PerSymbol[%s] trades = %+v; want one buy", sub.Symbol, sub.Result.Trades)
		}
	}
	// Both accounts invested: 5000*110/100 + 5000*55/50.
	want := 5000*110.0/100 + 5000*55.0/50
	if math.Abs(res.Equity-want) > 1e-9 {
		t.Fatalf("RunMulti().Equity = %v; want %v (both accounts bought)", res.Equity, want)
	}
}

func TestRunMultiDeterministic(t *testing.T) {
	series := []SymbolBars{
		mkSeries("A.US", 0, []float64{100, 101, 102, 103, 104}),
		{Symbol: "B.US", Bars: []ingest.Bar{barAt(1, 200), barAt(2, 201), barAt(4, 203), barAt(5, 204)}},
	}
	r1, err := RunMulti(context.Background(), series, 10000, 0, buyHoldFactory())
	if err != nil {
		t.Fatalf("RunMulti() error: %v", err)
	}
	r2, err := RunMulti(context.Background(), series, 10000, 0, buyHoldFactory())
	if err != nil {
		t.Fatalf("RunMulti() error: %v", err)
	}
	if r1.Equity != r2.Equity || r1.TotalReturn != r2.TotalReturn || len(r1.EquityCurve) != len(r2.EquityCurve) {
		t.Fatalf("runs differ: %+v vs %+v", r1, r2)
	}
	for i := range r1.EquityCurve {
		if !r1.EquityCurve[i].Ts.Equal(r2.EquityCurve[i].Ts) || r1.EquityCurve[i].Equity != r2.EquityCurve[i].Equity {
			t.Fatalf("curve point %d differs: %+v vs %+v", i, r1.EquityCurve[i], r2.EquityCurve[i])
		}
	}
}

func TestRunMultiValidation(t *testing.T) {
	valid := mkSeries("A.US", 0, []float64{100, 110})
	tests := []struct {
		name    string
		series  []SymbolBars
		cash    float64
		fee     float64
		factory StrategyFactory
		wantErr string
	}{
		{"empty series", nil, 10000, 0, buyHoldFactory(), "empty symbol series"},
		{"zero cash", []SymbolBars{valid}, 0, 0, buyHoldFactory(), "initial cash must be > 0"},
		{"negative cash", []SymbolBars{valid}, -1, 0, buyHoldFactory(), "initial cash must be > 0"},
		{"negative fee", []SymbolBars{valid}, 10000, -1, buyHoldFactory(), "negative fee"},
		{"nil factory", []SymbolBars{valid}, 10000, 0, nil, "nil strategy factory"},
		{"empty symbol name", []SymbolBars{{Symbol: "", Bars: valid.Bars}}, 10000, 0, buyHoldFactory(), "empty symbol"},
		{"duplicate symbol", []SymbolBars{valid, valid}, 10000, 0, buyHoldFactory(), "duplicate symbol A.US"},
		{"empty bars", []SymbolBars{valid, {Symbol: "B.US"}}, 10000, 0, buyHoldFactory(), "B.US: empty bars"},
		{"invalid bars", []SymbolBars{valid, {Symbol: "B.US", Bars: []ingest.Bar{{Ts: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Open: 1, High: 1, Low: 2, Close: 1}}}}, 10000, 0, buyHoldFactory(), "B.US: ingest: validate bars"},
		{"disjoint windows", []SymbolBars{valid, mkSeries("B.US", 5, []float64{100, 110})}, 10000, 0, buyHoldFactory(), "no common bars"},
		{"factory error", []SymbolBars{valid}, 10000, 0, func() (Strategy, error) { return nil, errors.New("boom") }, "strategy: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RunMulti(context.Background(), tt.series, tt.cash, tt.fee, tt.factory)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("RunMulti() error = %v; want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunMultiSubAccountError(t *testing.T) {
	// An over-buy in the second sub-account fails the whole run with its symbol.
	series := []SymbolBars{mkSeries("A.US", 0, []float64{100, 110}), mkSeries("B.US", 0, []float64{50, 55})}
	calls := 0
	_, err := RunMulti(context.Background(), series, 10000, 0, func() (Strategy, error) {
		calls++
		if calls == 1 {
			return HoldStrategy{}, nil
		}
		return stubStrategy{action: ActionBuy, size: 200}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "B.US") || !strings.Contains(err.Error(), "exceeds cash") {
		t.Fatalf("RunMulti() error = %v; want B.US exceeds cash", err)
	}
}

func TestRunMultiContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunMulti(ctx, []SymbolBars{mkSeries("A.US", 0, []float64{100, 110})}, 10000, 0, buyHoldFactory())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunMulti() error = %v; want context.Canceled", err)
	}
}
