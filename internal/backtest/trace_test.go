package backtest

// Deterministic trace tests (draft 2026-08-02 S1): Run must yield the exact
// equity curve and trade ledger a hand-replay computes, so persisted results
// and the read API can be verified against hand-checkable samples.

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
)

// eqPoint is a (dayOffset, equity) pair for curve assertions.
type eqPoint struct {
	day    int
	equity float64
}

// assertCurve checks the trace curve against day-offset/equity samples.
func assertCurve(t *testing.T, res *Result, want []eqPoint) {
	t.Helper()
	if len(res.EquityCurve) != len(want) {
		t.Fatalf("EquityCurve = %+v; want %d points %v", res.EquityCurve, len(want), want)
	}
	for i, w := range want {
		got := res.EquityCurve[i]
		if !got.Ts.Equal(expiryAt(w.day)) || math.Abs(got.Equity-w.equity) > 1e-9 {
			t.Fatalf("EquityCurve[%d] = %+v; want day %d equity %v", i, got, w.day, w.equity)
		}
	}
}

func TestRunTraceBuyHold(t *testing.T) {
	// All-in 100 shares @100: equity follows the close 100→110→121.
	res, err := Run(context.Background(), mkBars(100, 110, 121), 10000, 0, &BuyHoldStrategy{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	assertCurve(t, res, []eqPoint{{0, 10000}, {1, 11000}, {2, 12100}})
	if len(res.Trades) != 1 {
		t.Fatalf("Trades = %+v; want exactly the opening buy", res.Trades)
	}
	tr := res.Trades[0]
	if !tr.Ts.Equal(expiryAt(0)) || tr.Action != "buy" || tr.Size != 100 || tr.Price != 100 || tr.CashAfter != 0 {
		t.Fatalf("Trades[0] = %+v; want buy 100 @100 on day 0, cash_after 0", tr)
	}
}

func TestRunTraceBuyHoldFee(t *testing.T) {
	// fee=1 on the buy: cash_after -1, every equity point drops by 1.
	res, err := Run(context.Background(), mkBars(100, 110, 121), 10000, 1, &BuyHoldStrategy{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	assertCurve(t, res, []eqPoint{{0, 9999}, {1, 10999}, {2, 12099}})
	if tr := res.Trades[0]; tr.CashAfter != -1 || tr.Price != 100 {
		t.Fatalf("Trades[0] = %+v; want cash_after -1 at close 100", tr)
	}
}

func TestRunTraceHoldEmptyLedger(t *testing.T) {
	res, err := Run(context.Background(), mkBars(100, 110, 90), 10000, 0, HoldStrategy{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	assertCurve(t, res, []eqPoint{{0, 10000}, {1, 10000}, {2, 10000}})
	if len(res.Trades) != 0 {
		t.Fatalf("Trades = %+v; want none", res.Trades)
	}
}

func TestRunTraceExerciseCall(t *testing.T) {
	// Buy 100 @100, sell 1 C105 @2.5 (day 1), expiry day 2 with spot 110 > 105:
	// the short call exercises (position 100 -> 0, cash +10500).
	chain := map[string]OptionContract{"C": {Code: "C", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	opts := mkOptionsData(chain, map[string][]float64{"C": {3, 2.5, 1}})
	s := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{100, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2.5}, nil},
	}
	res, err := RunOptions(context.Background(), mkBars(100, 105, 110), 10000, 0, s, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	assertCurve(t, res, []eqPoint{{0, 10000}, {1, 10500}, {2, 10750}})
	if len(res.Trades) != 3 {
		t.Fatalf("Trades = %+v; want buy + sell-call + exercise-call", res.Trades)
	}
	open := res.Trades[1]
	if !open.Ts.Equal(expiryAt(1)) || open.Action != "sell-call" || open.Symbol != "C" || open.Size != 1 || open.Price != 2.5 || open.CashAfter != 250 {
		t.Fatalf("Trades[1] = %+v; want sell-call 1 @2.5 on day 1, cash_after 250", open)
	}
	ex := res.Trades[2]
	if !ex.Ts.Equal(expiryAt(2)) || ex.Action != "exercise-call" || ex.Symbol != "C" || ex.Size != -100 || ex.Price != 105 || ex.CashAfter != 10750 {
		t.Fatalf("Trades[2] = %+v; want exercise-call -100 @105 on day 2, cash_after 10750", ex)
	}
}

func TestRunTraceExpireOTM(t *testing.T) {
	// Same setup but spot 104 < 105 at expiry: the call voids (no cash flow).
	chain := map[string]OptionContract{"C": {Code: "C", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	opts := mkOptionsData(chain, map[string][]float64{"C": {3, 2.5, 1}})
	s := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{100, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2.5}, nil},
	}
	res, err := RunOptions(context.Background(), mkBars(100, 105, 104), 10000, 0, s, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	assertCurve(t, res, []eqPoint{{0, 10000}, {1, 10500}, {2, 10650}})
	if len(res.Trades) != 3 {
		t.Fatalf("Trades = %+v; want buy + sell-call + expire-otm", res.Trades)
	}
	void := res.Trades[2]
	if !void.Ts.Equal(expiryAt(2)) || void.Action != "expire-otm" || void.Symbol != "C" || void.Size != 0 || void.Price != 0 || void.CashAfter != 250 {
		t.Fatalf("Trades[2] = %+v; want expire-otm on day 2, cash_after 250", void)
	}
}

func TestRunTraceExercisePut(t *testing.T) {
	// CSP: sell 1 P95 @4 per share (contract premium 4×100),
	// expiry day 1 with spot 90 < 95: the short put exercises (buy shares at
	// 95: cash -9500, position +100).
	chain := map[string]OptionContract{"P": {Code: "P", Kind: OptionPut, Strike: 95, Expiry: expiryAt(1)}}
	opts := mkOptionsData(chain, map[string][]float64{"P": {4, 4}})
	s := &scriptStrategy{
		actions: []Action{ActionSellPut, ActionHold},
		sizes:   []float64{1, 0},
		pending: []*OptionPosition{{Code: "P", Kind: OptionPut, Strike: 95, Expiry: expiryAt(1), Lot: 100, AvgPremium: 4}, nil},
	}
	res, err := RunOptions(context.Background(), mkBars(100, 90), 10000, 0, s, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	// Day 0: cash 10400, short put marked -4×100 → 10000.
	// Day 1: exercise → cash 10400-9500 = 900, position +100 → 9900.
	assertCurve(t, res, []eqPoint{{0, 10000}, {1, 9900}})
	if len(res.Trades) != 2 {
		t.Fatalf("Trades = %+v; want sell-put + exercise-put", res.Trades)
	}
	if open := res.Trades[0]; open.Action != "sell-put" || open.Symbol != "P" || open.Size != 1 || open.Price != 4 || open.CashAfter != 10400 {
		t.Fatalf("Trades[0] = %+v; want sell-put 1 @4, cash_after 10400", open)
	}
	ex := res.Trades[1]
	if !ex.Ts.Equal(expiryAt(1)) || ex.Action != "exercise-put" || ex.Symbol != "P" || ex.Size != -100 || ex.Price != 95 || ex.CashAfter != 900 {
		t.Fatalf("Trades[1] = %+v; want exercise-put -100 @95 on day 1, cash_after 900", ex)
	}
}

func TestRunTraceSameDayExpiryDeterministic(t *testing.T) {
	// Two short calls (A strike 90, B strike 100) expire on the same bar;
	// settleExpired must book them in contract-code order so every run yields
	// the identical ledger (map iteration alone is random).
	chain := map[string]OptionContract{
		"A": {Code: "A", Kind: OptionCall, Strike: 90, Expiry: expiryAt(1)},
		"B": {Code: "B", Kind: OptionCall, Strike: 100, Expiry: expiryAt(1)},
	}
	opts := mkOptionsData(chain, map[string][]float64{"A": {1, 0.5}, "B": {0.5, 0.25}})
	newScript := func() *scriptStrategy {
		return &scriptStrategy{
			actions: []Action{ActionSellCall, ActionSellCall},
			sizes:   []float64{1, 1},
			pending: []*OptionPosition{
				{Code: "A", Kind: OptionCall, Strike: 90, Expiry: expiryAt(1), Lot: 100, AvgPremium: 1},
				{Code: "B", Kind: OptionCall, Strike: 100, Expiry: expiryAt(1), Lot: 100, AvgPremium: 0.25},
			},
		}
	}
	want := []Trade{
		{Ts: expiryAt(0), Action: "sell-call", Symbol: "A", Size: 1, Price: 1, CashAfter: 10100},
		{Ts: expiryAt(1), Action: "sell-call", Symbol: "B", Size: 1, Price: 0.25, CashAfter: 10125},
		{Ts: expiryAt(1), Action: "exercise-call", Symbol: "A", Size: -100, Price: 90, CashAfter: 19125},
		{Ts: expiryAt(1), Action: "exercise-call", Symbol: "B", Size: -100, Price: 100, CashAfter: 29125},
	}
	for i := 0; i < 20; i++ {
		res, err := RunOptions(context.Background(), mkBars(100, 110), 10000, 0, newScript(), opts)
		if err != nil {
			t.Fatalf("run %d: RunOptions() error: %v", i, err)
		}
		if !reflect.DeepEqual(res.Trades, want) {
			t.Fatalf("run %d: Trades = %+v; want %+v (stable contract-code order)", i, res.Trades, want)
		}
		if i == 0 {
			assertCurve(t, res, []eqPoint{{0, 10000}, {1, 7125}})
		}
	}
}

func TestEquityPointJSON(t *testing.T) {
	// Wire format check: JSONB round-trip of the trace keeps ts RFC3339.
	p := EquityPoint{Ts: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Equity: 10000}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ts":"2024-01-01T00:00:00Z","equity":10000}` {
		t.Fatalf("EquityPoint JSON = %s", data)
	}
}
