package backtest

// Unfilled-attempt model tests (S3, doc/tasks/2026-08-13-s3-unfilled-model):
// failProb bounds and monotonicity, per-attempt draw determinism, seed
// separation, liquidity ordering, missing-quote high-fail, hold/buy exclusion
// from the attempt denominator, and Trade Filled/UnfilledModel trace flags.
//
// The seeds below are hard-coded outcomes of the pure attemptDraw function:
// e.g. seed 1 fills the C105 day-1 one-shot attempt, seed 8 unfills exactly
// one of the three deterministic-scenario attempts, seed 31 unfills attempt 1
// and fills attempt 2. Changing attemptDraw's hash requires re-deriving them.

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/jiayu/wbot/internal/wheel"
)

func liquidQuote(code string, bid float64) wheel.OptionQuote {
	return wheel.OptionQuote{Symbol: code, Code: code, Bid: bid, Ask: bid * 1.001, Volume: 100_000, OpenInterest: 1_000_000}
}

// quotedOptions wraps mkOptionsData with one liquid quote batch at day 0
// (underlying "U.US"), so sell attempts sample the 0.05 floor.
func quotedOptions(chain map[string]OptionContract, closes map[string][]float64, runSeed int64) *OptionsData {
	data := mkOptionsData(chain, closes)
	quotes := make([]wheel.OptionQuote, 0, len(chain))
	for code, cs := range closes {
		quotes = append(quotes, liquidQuote(code, cs[0]))
	}
	data.QuoteBatches = []QuoteSnapshotBatch{{
		ObservedAt: expiryAt(0), SnapshotKey: "fixture", Underlying: "U.US", UnderlyingPrice: 100, Quotes: quotes,
	}}
	data.Snapshots = data.QuoteBatches
	data.QuoteSnapshots = data.QuoteBatches
	data.RunSeed = runSeed
	return data
}

func TestUnfilledFailProb(t *testing.T) {
	tests := []struct {
		name    string
		bid     float64
		ask     float64
		vol     int64
		oi      int64
		want    float64
		compare func(got, want float64) bool
	}{
		{"no market", 0, 0, 0, 0, failCap, func(got, want float64) bool { return got == want }},
		{"wide market", 1, 9, 0, 0, failCap, func(got, want float64) bool { return got == want }},
		{"crossed market", 3, 2, 0, 0, 0.45, func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }},
		{"one-sided ask zero", 2, 0, 0, 0, 0.45, func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }},
		{"tight market missing vol/oi", 3, 3.01, 0, 0, 0.4518, func(got, want float64) bool { return math.Abs(got-want) < 1e-4 }},
		{"tight liquid", 3, 3.01, 100_000, 1_000_000, failFloor, func(got, want float64) bool { return got == want }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failProb(tt.bid, tt.ask, tt.vol, tt.oi)
			if got < failFloor || got > failCap {
				t.Fatalf("failProb = %v; want within [%v, %v]", got, failFloor, failCap)
			}
			if !tt.compare(got, tt.want) {
				t.Fatalf("failProb = %v; want %v", got, tt.want)
			}
		})
	}
	// Monotonicity: more volume or tighter spread never increases the fail.
	base := failProb(3, 3.01, 100, 1000)
	for v := int64(1000); v < 1_000_000; v *= 10 {
		if p := failProb(3, 3.01, v, 1000); p > base {
			t.Fatalf("failProb vol=%d = %v; want <= %v", v, p, base)
		}
	}
	if p := failProb(2, 9, 100, 1000); p <= base {
		t.Fatalf("failProb wide = %v; want > liquid %v", p, base)
	}
}

func TestAttemptDrawDeterministicPerInput(t *testing.T) {
	ts := expiryAt(0)
	d := attemptDraw(42, "U.US", "C105", ts, 1)
	if attemptDraw(42, "U.US", "C105", ts, 1) != d {
		t.Fatal("same inputs gave different draws")
	}
	seen := map[float64]bool{}
	for i := int64(1); i <= 16; i++ {
		draw := attemptDraw(42, "U.US", "C105", ts, i)
		if draw < 0 || draw >= 1 {
			t.Fatalf("attempt %d: draw %v outside [0,1)", i, draw)
		}
		if seen[draw] {
			t.Fatalf("attempt %d: draw %v collides with an earlier attempt", i, draw)
		}
		seen[draw] = true
	}
	// Each input participates: changing symbol, contract, bar or seed moves the draw.
	others := []float64{
		attemptDraw(42, "OTHER", "C105", ts, 1),
		attemptDraw(42, "U.US", "P95", ts, 1),
		attemptDraw(42, "U.US", "C105", expiryAt(1), 1),
		attemptDraw(7, "U.US", "C105", ts, 1),
	}
	for _, o := range others {
		if o == d {
			t.Fatalf("draw %v unchanged after changing an input", o)
		}
	}
}

// detScript sells one C105 call on bars 0, 1 and 3 (expiry day 5).
func detScript() *scriptStrategy {
	return &scriptStrategy{
		actions: []Action{ActionSellCall, ActionSellCall, ActionHold, ActionSellCall, ActionHold},
		sizes:   []float64{1, 1, 0, 1, 0},
		pending: []*OptionPosition{
			{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
			{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
			nil,
			{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
			nil,
		},
	}
}

func runDet(t *testing.T, seed int64) *Result {
	t.Helper()
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5)}}
	res, err := RunOptions(context.Background(), mkBars(100, 101, 102, 103, 104), 10000, 0, detScript(), quotedOptions(chain, map[string][]float64{"C105": {2, 2, 2, 2}}, seed))
	if err != nil {
		t.Fatalf("RunOptions(seed %d) error: %v", seed, err)
	}
	return res
}

func TestUnfilledSameSeedSameTrace(t *testing.T) {
	a, b := runDet(t, 42), runDet(t, 42)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different results: %+v vs %+v", a, b)
	}
	if a.Unfilled.AttemptCount != 3 || a.Unfilled.AttemptCount != a.Unfilled.FillCount+a.Unfilled.UnfilledCount {
		t.Fatalf("unfilled = %+v; want 3 attempts split between fills and unfilled", a.Unfilled)
	}
}

func TestUnfilledSeedSeparation(t *testing.T) {
	// Seed 8 unfills exactly one of the three deterministic attempts; seed 42
	// fills all three (hard-coded draw outcomes).
	a, b := runDet(t, 42), runDet(t, 8)
	if reflect.DeepEqual(a, b) {
		t.Fatal("different seeds produced identical results")
	}
	if a.Unfilled.UnfilledCount != 0 || b.Unfilled.UnfilledCount != 1 {
		t.Fatalf("unfilled counts = %d (seed 42) / %d (seed 8); want 0 / 1", a.Unfilled.UnfilledCount, b.Unfilled.UnfilledCount)
	}
}

func TestUnfilledDefaultSeedIs42(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5)}}
	closes := map[string][]float64{"C105": {2, 2, 2, 2}}
	zero := quotedOptions(chain, closes, 0) // 0 -> defaultRunSeed
	fortyTwo := quotedOptions(chain, closes, 42)
	a, err := RunOptions(context.Background(), mkBars(100, 101, 102, 103, 104), 10000, 0, detScript(), zero)
	if err != nil {
		t.Fatalf("RunOptions(seed 0) error: %v", err)
	}
	b, err := RunOptions(context.Background(), mkBars(100, 101, 102, 103, 104), 10000, 0, detScript(), fortyTwo)
	if err != nil {
		t.Fatalf("RunOptions(seed 42) error: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("RunSeed 0 (default 42) differs from explicit 42: %+v vs %+v", a, b)
	}
}

func TestUnfilledLiquidityOrdering(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(20)}}
	closes := map[string][]float64{}
	barCloses := make([]float64, 20)
	for i := 0; i < 20; i++ {
		closes["C105"] = append(closes["C105"], 2)
		barCloses[i] = 100 + float64(i)
	}
	bars := mkBars(barCloses...)
	run := func(quotes []wheel.OptionQuote) *Result {
		data := quotedOptions(chain, closes, 42)
		data.QuoteBatches = []QuoteSnapshotBatch{{
			ObservedAt: expiryAt(0), SnapshotKey: "fixture", Underlying: "U.US", UnderlyingPrice: 100, Quotes: quotes,
		}}
		data.Snapshots = data.QuoteBatches
		data.QuoteSnapshots = data.QuoteBatches
		sc := &scriptStrategy{}
		for i := 0; i < 20; i++ {
			sc.actions = append(sc.actions, ActionSellCall)
			sc.sizes = append(sc.sizes, 1)
			sc.pending = append(sc.pending, &OptionPosition{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(20), Lot: 100, AvgPremium: 2})
		}
		res, err := RunOptions(context.Background(), bars, 10000, 0, sc, data)
		if err != nil {
			t.Fatalf("RunOptions() error: %v", err)
		}
		return res
	}
	liquid := make([]wheel.OptionQuote, 0, 1)
	liquid = append(liquid, liquidQuote("C105", 5))
	illiquid := []wheel.OptionQuote{{Symbol: "C105", Code: "C105", Bid: 1, Ask: 9, Volume: 0, OpenInterest: 0}}
	liq, ilq := run(liquid), run(illiquid)
	if liq.Unfilled.AttemptCount != 20 || ilq.Unfilled.AttemptCount != 20 {
		t.Fatalf("attempts = %d / %d; want 20 / 20", liq.Unfilled.AttemptCount, ilq.Unfilled.AttemptCount)
	}
	if liq.Unfilled.UnfilledRatio == nil || ilq.Unfilled.UnfilledRatio == nil {
		t.Fatalf("ratios = %v / %v; want non-nil with 20 attempts", liq.Unfilled.UnfilledRatio, ilq.Unfilled.UnfilledRatio)
	}
	// Same seed, same draws: only the market inputs differ.
	if *liq.Unfilled.UnfilledRatio >= *ilq.Unfilled.UnfilledRatio {
		t.Fatalf("liquid ratio %v >= illiquid ratio %v; want liquid < illiquid", *liq.Unfilled.UnfilledRatio, *ilq.Unfilled.UnfilledRatio)
	}
	if *liq.Unfilled.UnfilledRatio >= 0.5 || *ilq.Unfilled.UnfilledRatio <= 0.5 {
		t.Fatalf("ratios = %v / %v; want liquid < 0.5 and illiquid > 0.5", *liq.Unfilled.UnfilledRatio, *ilq.Unfilled.UnfilledRatio)
	}
}

func TestUnfilledMissingQuoteHighFail(t *testing.T) {
	// No quote batch: no market info, p_fail clamps to 0.95. Seed 0 unfills
	// the single attempt (hard-coded draw outcome).
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5)}}
	data := &OptionsData{Chain: chain, Bars: OptionBars{"C105": {{
		Ts: expiryAt(0), Open: 2, High: 2, Low: 2, Close: 2, Volume: 100,
	}}}, RunSeed: 0}
	sc := &scriptStrategy{
		actions: []Action{ActionSellCall},
		sizes:   []float64{1},
		pending: []*OptionPosition{{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2}},
	}
	res, err := RunOptions(context.Background(), mkBars(100), 10000, 0, sc, data)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if res.Unfilled.AttemptCount != 1 || res.Unfilled.FillCount != 0 || res.Unfilled.UnfilledCount != 1 {
		t.Fatalf("unfilled = %+v; want 1 attempt, 0 fills, 1 unfilled", res.Unfilled)
	}
	if res.Unfilled.UnfilledRatio == nil || *res.Unfilled.UnfilledRatio != 1 {
		t.Fatalf("ratio = %v; want 1.0", res.Unfilled.UnfilledRatio)
	}
	if len(res.Trades) != 1 {
		t.Fatalf("trades = %+v; want exactly the unfilled attempt", res.Trades)
	}
	tr := res.Trades[0]
	if tr.Filled || tr.UnfilledModel != unfilledModelLabel() || tr.CashAfter != 10000 || tr.Action != "sell-call" {
		t.Fatalf("unfilled trade = %+v; want Filled:false %s cash unchanged", tr, unfilledModelLabel())
	}
	if len(sc.st.Options) != 0 || sc.st.Cash != 10000 {
		t.Fatalf("state = cash %v options %v; want no booking after an unfilled attempt", sc.st.Cash, sc.st.Options)
	}
	if res.Unfilled.ModelKind != modelKind || res.Unfilled.ModelVersion != modelVersion {
		t.Fatalf("model = %s/%s; want %s/%s", res.Unfilled.ModelKind, res.Unfilled.ModelVersion, modelKind, modelVersion)
	}
}

func TestUnfilledTraceFlags(t *testing.T) {
	// Seed 31: attempt 1 (day 0) unfills, attempt 2 (day 1) fills.
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5)}}
	closes := map[string][]float64{"C105": {2, 2}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellCall, ActionSellCall},
		sizes:   []float64{1, 1},
		pending: []*OptionPosition{
			{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
			{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
		},
	}
	res, err := RunOptions(context.Background(), mkBars(100, 101), 10000, 0, sc, quotedOptions(chain, closes, 31))
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if len(res.Trades) != 2 || res.Unfilled.UnfilledCount != 1 || res.Unfilled.FillCount != 1 {
		t.Fatalf("trades %+v unfilled %+v; want one unfilled + one filled", res.Trades, res.Unfilled)
	}
	if res.Trades[0].Filled || res.Trades[0].UnfilledModel != unfilledModelLabel() {
		t.Fatalf("trades[0] = %+v; want unfilled attempt with model %s", res.Trades[0], unfilledModelLabel())
	}
	if !res.Trades[1].Filled || res.Trades[1].UnfilledModel != "" {
		t.Fatalf("trades[1] = %+v; want filled trade without a model label", res.Trades[1])
	}
}

func TestUnfilledHoldAndBuysNotAttempted(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5)}}
	sc := &scriptStrategy{
		actions: []Action{ActionHold, ActionBuyCall},
		sizes:   []float64{0, 1},
		pending: []*OptionPosition{nil, {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2}},
	}
	res, err := RunOptions(context.Background(), mkBars(100, 101), 20000, 0, sc, quotedOptions(chain, map[string][]float64{"C105": {2, 2}}, 42))
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if res.Unfilled.AttemptCount != 0 || res.Unfilled.FillCount != 0 || res.Unfilled.UnfilledCount != 0 {
		t.Fatalf("unfilled = %+v; want HOLD and buys to never enter the attempt denominator", res.Unfilled)
	}
	if res.Unfilled.UnfilledRatio != nil {
		t.Fatalf("ratio = %v; want nil (null) when no attempts", *res.Unfilled.UnfilledRatio)
	}
	if res.Unfilled.ModelKind != modelKind || res.Unfilled.ModelVersion != modelVersion {
		t.Fatalf("model = %s/%s; want %s/%s", res.Unfilled.ModelKind, res.Unfilled.ModelVersion, modelKind, modelVersion)
	}
	if len(res.Trades) != 1 || !res.Trades[0].Filled || res.Trades[0].Action != "buy-call" {
		t.Fatalf("trades = %+v; want the buy-call fill only", res.Trades)
	}
}
