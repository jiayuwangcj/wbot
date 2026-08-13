package backtest

// Option-leg runner tests: mark-to-market equity, expiry settlement (ITM
// exercise / OTM void, long and short), CSP cash reserve, trade validation.

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
)

// scriptStrategy replays a fixed action/size/pending per bar (index-aligned),
// holding when the script runs out; captures the final State pointer.
type scriptStrategy struct {
	actions []Action
	sizes   []float64
	pending []*OptionPosition
	st      *State
	bar     int
}

func (s *scriptStrategy) OnBar(_ context.Context, _ ingest.Bar, st *State) (Action, float64, error) {
	s.st = st
	i := s.bar
	s.bar++
	if i >= len(s.actions) {
		return ActionHold, 0, nil
	}
	if s.pending[i] != nil {
		st.Pending = s.pending[i]
	}
	return s.actions[i], s.sizes[i], nil
}

// mkOptionsData builds an OptionsData from per-code close lists (bars on the
// same days as mkBars: Jan 1+i 2024) and a chain, plus one liquid quote batch
// at day 0 (tight ask, high vol/oi) so sell attempts sample the 0.05 floor.
func mkOptionsData(chain map[string]OptionContract, closes map[string][]float64) *OptionsData {
	data := &OptionsData{Chain: OptionChain{}, Bars: OptionBars{}}
	for code, c := range chain {
		data.Chain[code] = c
	}
	for code, cs := range closes {
		for i, v := range cs {
			data.Bars[code] = append(data.Bars[code], ingest.Bar{
				Ts:   time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC),
				Open: v, High: v, Low: v, Close: v, Volume: 100,
			})
		}
	}
	quotes := make([]wheel.OptionQuote, 0, len(chain))
	for code, cs := range closes {
		quotes = append(quotes, wheel.OptionQuote{
			Symbol: code, Code: code, Bid: cs[0], Ask: cs[0] * 1.001,
			Volume: 100_000, OpenInterest: 1_000_000,
		})
	}
	data.QuoteBatches = []QuoteSnapshotBatch{{
		ObservedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		SnapshotKey: "fixture", Underlying: "U.US", UnderlyingPrice: 100, Quotes: quotes,
	}}
	data.Snapshots = data.QuoteBatches
	data.QuoteSnapshots = data.QuoteBatches
	return data
}

// expiryAt returns a UTC midnight expiry N bars after the first bar (Jan 1).
func expiryAt(barIdx int) time.Time {
	return time.Date(2024, 1, 1+barIdx, 0, 0, 0, 0, time.UTC)
}

func TestStateEquityWithOptions(t *testing.T) {
	st := &State{
		Cash: 500, Position: 100, Price: 110,
		OptPrice: map[string]float64{"C1": 1.5, "P1": 0.4},
		Options: map[string]OptionPosition{
			"C1": {Code: "C1", Kind: OptionCall, Contracts: -1, Lot: 100}, // short call: liability
			"P1": {Code: "P1", Kind: OptionPut, Contracts: 2, Lot: 100},   // long puts: asset
		},
	}
	// 500 + 100*110 - 1*100*1.5 + 2*100*0.4 = 11430.
	if eq := st.Equity(110); math.Abs(eq-11430) > 1e-9 {
		t.Fatalf("Equity() = %v; want 11430", eq)
	}
	// Legs without a price mark to 0 (unknown).
	st.OptPrice = map[string]float64{}
	if eq := st.Equity(110); math.Abs(eq-500-11000) > 1e-9 {
		t.Fatalf("Equity() unpriced = %v; want 11500", eq)
	}
}

func TestStatePriceAt(t *testing.T) {
	st := &State{OptBars: OptionBars{
		"X": {
			{Ts: expiryAt(0), Close: 10},
			{Ts: expiryAt(2), Close: 12},
			{Ts: expiryAt(4), Close: 9},
		},
	}}
	tests := []struct {
		day  int // bars offset from Jan 1
		want float64
		ok   bool
	}{
		{0, 10, true}, // exact day
		{1, 10, true}, // gap: latest <= ts
		{2, 12, true},
		{5, 9, true}, // after last bar
		{-1, 0, false},
	}
	for _, tt := range tests {
		ts := expiryAt(tt.day)
		got, ok := st.PriceAt("X", ts)
		if ok != tt.ok || (ok && math.Abs(got-tt.want) > 1e-9) {
			t.Fatalf("PriceAt(X, day %d) = (%v, %v); want (%v, %v)", tt.day, got, ok, tt.want, tt.ok)
		}
	}
	if _, ok := st.PriceAt("missing", expiryAt(0)); ok {
		t.Fatal("PriceAt(missing) = ok; want false")
	}
}

func TestShortCallITMExercise(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{100, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}
	// 100 shares in @100, premium 200, then ITM at 120: shares out at strike 105.
	// RunSeed 1 fills the single sell attempt (unfilled.go draw outcome).
	opts := mkOptionsData(chain, map[string][]float64{"C105": {2, 2}})
	opts.RunSeed = 1
	res, err := RunOptions(context.Background(), mkBars(100, 110, 120), 10000, 0, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if math.Abs(res.Equity-10700) > 1e-9 || math.Abs(res.TotalReturn-0.07) > 1e-9 {
		t.Fatalf("RunOptions() = %+v; want Equity 10700, TotalReturn 0.07", res)
	}
	if len(sc.st.Options) != 0 || sc.st.Position != 0 || math.Abs(sc.st.Cash-10700) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want no legs, position 0, cash 10700", sc.st)
	}
}

func TestShortCallOTMExpiry(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{100, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"C105": {2, 2}})
	opts.RunSeed = 1
	res, err := RunOptions(context.Background(), mkBars(100, 102, 101), 10000, 0, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	// OTM: leg voided, stock kept, premium kept: 200 + 100*101.
	if math.Abs(res.Equity-10300) > 1e-9 {
		t.Fatalf("RunOptions() = %+v; want Equity 10300", res)
	}
	if len(sc.st.Options) != 0 || sc.st.Position != 100 {
		t.Fatalf("post-settle state = %+v; want no legs, position 100", sc.st)
	}
}

func TestShortPutITMExercise(t *testing.T) {
	chain := map[string]OptionContract{"P95": {Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellPut, ActionHold, ActionHold},
		sizes:   []float64{1, 0, 0},
		pending: []*OptionPosition{{Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2), Lot: 100, AvgPremium: 3}, nil, nil},
	}
	// premium 300, assigned at strike 95 on close 85: cash 10000+300-9500, stock 100.
	opts := mkOptionsData(chain, map[string][]float64{"P95": {3, 3}})
	opts.RunSeed = 0
	res, err := RunOptions(context.Background(), mkBars(100, 90, 85), 10000, 0, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if math.Abs(res.Equity-9300) > 1e-9 {
		t.Fatalf("RunOptions() = %+v; want Equity 9300 (cash 800 + 100*85)", res)
	}
	if len(sc.st.Options) != 0 || sc.st.Position != 100 || math.Abs(sc.st.Cash-800) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want no legs, position 100, cash 800", sc.st)
	}
}

func TestShortPutOTMExpiry(t *testing.T) {
	chain := map[string]OptionContract{"P95": {Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellPut, ActionHold, ActionHold},
		sizes:   []float64{1, 0, 0},
		pending: []*OptionPosition{{Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2), Lot: 100, AvgPremium: 3}, nil, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"P95": {3, 3}})
	opts.RunSeed = 0
	res, err := RunOptions(context.Background(), mkBars(100, 103, 102), 10000, 0, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	// OTM: leg voided, premium kept: 10300.
	if math.Abs(res.Equity-10300) > 1e-9 {
		t.Fatalf("RunOptions() = %+v; want Equity 10300", res)
	}
	if len(sc.st.Options) != 0 || sc.st.Position != 0 {
		t.Fatalf("post-settle state = %+v; want no legs, position 0", sc.st)
	}
}

func TestLongCallExercise(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuyCall, ActionHold, ActionHold},
		sizes:   []float64{1, 0, 0},
		pending: []*OptionPosition{{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil, nil},
	}
	// long call: paid 200, exercise at 105 on close 120 -> buy 100 shares.
	res, err := RunOptions(context.Background(), mkBars(100, 105, 120), 20000, 0, sc, mkOptionsData(chain, map[string][]float64{"C105": {2, 2}}))
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if math.Abs(res.Equity-21300) > 1e-9 {
		t.Fatalf("RunOptions() = %+v; want Equity 21300 (cash 9300 + 100*120)", res)
	}
	if sc.st.Position != 100 || math.Abs(sc.st.Cash-9300) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want position 100, cash 9300", sc.st)
	}
}

func TestLongPutExercise(t *testing.T) {
	chain := map[string]OptionContract{"P95": {Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuyPut, ActionHold, ActionHold},
		sizes:   []float64{1, 0, 0},
		pending: []*OptionPosition{{Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2), Lot: 100, AvgPremium: 1.5}, nil, nil},
	}
	// long put: paid 150, exercise at 95 on close 90 -> sell 100 shares short.
	res, err := RunOptions(context.Background(), mkBars(100, 95, 90), 20000, 0, sc, mkOptionsData(chain, map[string][]float64{"P95": {1.5, 1.5}}))
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if math.Abs(res.Equity-20350) > 1e-9 {
		t.Fatalf("RunOptions() = %+v; want Equity 20350 (cash 29350 - 100*90)", res)
	}
	if sc.st.Position != -100 || math.Abs(sc.st.Cash-29350) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want position -100, cash 29350", sc.st)
	}
}

func TestSellPutCashReserveError(t *testing.T) {
	chain := map[string]OptionContract{"P105": {Code: "P105", Kind: OptionPut, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellPut},
		sizes:   []float64{1},
		pending: []*OptionPosition{{Code: "P105", Kind: OptionPut, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}},
	}
	// cash 10000 + premium 200 < 10500 required.
	_, err := RunOptions(context.Background(), mkBars(100, 101), 10000, 0, sc, mkOptionsData(chain, map[string][]float64{"P105": {2}}))
	if err == nil || !strings.Contains(err.Error(), "needs cash reserve") {
		t.Fatalf("RunOptions() error = %v; want cash reserve error", err)
	}
}

func TestSellPutCashReserveIncludesExistingShortPuts(t *testing.T) {
	chain := map[string]OptionContract{
		"P95":  {Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2)},
		"P100": {Code: "P100", Kind: OptionPut, Strike: 100, Expiry: expiryAt(2)},
	}
	sc := &scriptStrategy{
		actions: []Action{ActionSellPut, ActionSellPut},
		sizes:   []float64{1, 1},
		pending: []*OptionPosition{
			{Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2},
			{Code: "P100", Kind: OptionPut, Strike: 100, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2},
		},
	}
	_, err := RunOptions(context.Background(), mkBars(100, 101), 15_000, 0, sc, mkOptionsData(chain, map[string][]float64{"P95": {2, 2}, "P100": {2, 2}}))
	if err == nil || !strings.Contains(err.Error(), "cumulative across open short puts") {
		t.Fatalf("RunOptions() error = %v; want cumulative cash reserve rejection", err)
	}
}

func TestOptionTradeValidation(t *testing.T) {
	chain := map[string]OptionContract{"C1": {Code: "C1", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	bars := mkOptionsData(chain, map[string][]float64{"C1": {2}})
	opt := func() *OptionPosition {
		return &OptionPosition{Code: "C1", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}
	}
	tests := []struct {
		name    string
		act     Action
		size    float64
		pending *OptionPosition
		opts    *OptionsData
		wantErr string
	}{
		{"sell-call without pending", ActionSellCall, 1, nil, bars, "without a pending option"},
		{"kind mismatch", ActionSellPut, 1, opt(), bars, "pending kind"},
		{"zero contracts", ActionSellCall, 0, opt(), bars, "want > 0 contracts"},
		{"negative contracts", ActionSellCall, -1, opt(), bars, "want > 0 contracts"},
		{"incomplete contract", ActionSellCall, 1, &OptionPosition{Kind: OptionCall}, bars, "incomplete"},
		{"no price data", ActionSellCall, 1, opt(), &OptionsData{Chain: chain}, "no option price data"},
		{"buy exceeds cash", ActionBuyCall, 1, &OptionPosition{Code: "C1", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 500}, bars, "exceeds cash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &scriptStrategy{actions: []Action{tt.act}, sizes: []float64{tt.size}, pending: []*OptionPosition{tt.pending}}
			_, err := RunOptions(context.Background(), mkBars(100), 10000, 0, sc, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("RunOptions() error = %v; want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestOptionLegMerge(t *testing.T) {
	chain := map[string]OptionContract{"C1": {Code: "C1", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellCall, ActionSellCall},
		sizes:   []float64{1, 1},
		pending: []*OptionPosition{
			{Code: "C1", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
			{Code: "C1", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2.2},
		},
	}
	opts := mkOptionsData(chain, map[string][]float64{"C1": {2, 2}})
	opts.RunSeed = 1 // fills both sell attempts (draw outcomes, unfilled.go)
	if _, err := RunOptions(context.Background(), mkBars(100, 101), 10000, 0, sc, opts); err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	pos, ok := sc.st.Options["C1"]
	if !ok || pos.Contracts != -2 || math.Abs(pos.AvgPremium-2.1) > 1e-9 || math.Abs(sc.st.Cash-10420) > 1e-9 {
		t.Fatalf("merged leg = %+v cash %v; want contracts -2, avg 210, cash 10420", pos, sc.st.Cash)
	}
}

func TestOptionsDataFromQuotes(t *testing.T) {
	day := func(i int) time.Time { return expiryAt(i) }
	rows := []ingest.OptionQuoteRow{
		{Symbol: "O1", Underlying: "U.US", OptionType: "call", Strike: 105, Expiry: day(2), Ts: day(0), Close: 2},
		{Symbol: "O1", Underlying: "U.US", OptionType: "call", Strike: 105, Expiry: day(2), Ts: day(1), Close: 1.5},
		{Symbol: "O2", Underlying: "U.US", OptionType: "put", Strike: 95, Expiry: day(3), Ts: day(0), Close: 3},
	}
	data, err := OptionsDataFromQuotes(rows)
	if err != nil {
		t.Fatalf("OptionsDataFromQuotes() error: %v", err)
	}
	if len(data.Chain) != 2 || data.Chain["O1"].Kind != OptionCall || data.Chain["O1"].Strike != 105 || !data.Chain["O1"].Expiry.Equal(day(2)) {
		t.Fatalf("chain = %+v; want O1 call 105 exp day2, O2 put", data.Chain)
	}
	if len(data.Bars["O1"]) != 2 || len(data.Bars["O2"]) != 1 || data.Bars["O1"][1].Close != 1.5 {
		t.Fatalf("bars = %+v; want O1 2 bars, O2 1 bar", data.Bars)
	}
	if _, err := OptionsDataFromQuotes(nil); err == nil {
		t.Fatal("OptionsDataFromQuotes(nil) = nil error; want error")
	}
	if _, err := OptionsDataFromQuotes([]ingest.OptionQuoteRow{{Symbol: "X", OptionType: "straddle"}}); err == nil {
		t.Fatal("OptionsDataFromQuotes(bad type) = nil error; want error")
	}
}
