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
	if res.Terminal.ExpiryCount != 1 || res.Terminal.ShortExpiryCount != 1 || res.Terminal.AssignmentCount != 1 || res.Terminal.AssignmentRate == nil || *res.Terminal.AssignmentRate != 1 || res.Terminal.BrokerAssignmentCount != nil {
		t.Fatalf("terminal assignment stats = %+v; want one mechanical short-leg assignment and null broker fact", res.Terminal)
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

func TestFilledOptionTradeDeductsConfiguredFee(t *testing.T) {
	chain := map[string]OptionContract{"P95": {Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellPut},
		sizes:   []float64{1},
		pending: []*OptionPosition{{Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2), Lot: 100, AvgPremium: 3}},
	}
	opts := mkOptionsData(chain, map[string][]float64{"P95": {3}})
	opts.RunSeed = 0
	res, err := RunOptions(context.Background(), mkBars(100), 10000, 7.5, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if len(res.Trades) != 1 || !res.Trades[0].Filled || res.Trades[0].Fee != 7.5 || res.Trades[0].CashAfter != 10292.5 {
		t.Fatalf("option trade = %+v; want premium 300 less fee 7.5", res.Trades)
	}
	if !res.Fees.Included || res.Fees.PerTrade != 7.5 || res.Fees.TotalAmount != 7.5 || res.Fees.OptionAmount != 7.5 || res.Fees.StockAmount != 0 || res.Fees.ChargedTradeCount != 1 {
		t.Fatalf("fees = %+v; want one charged option fill", res.Fees)
	}
	if res.Terminal.OpenOptionLegCount != 1 || res.Terminal.SettlementStatus != SettlementOpenOptionLegs ||
		res.Terminal.OptionMarketValueAmount == nil || *res.Terminal.OptionMarketValueAmount != -300 ||
		res.Terminal.RealizedPnLAmount == nil || *res.Terminal.RealizedPnLAmount != -7.5 ||
		res.Terminal.UnrealizedPnLAmount == nil || *res.Terminal.UnrealizedPnLAmount != 0 {
		t.Fatalf("terminal open-leg accounting = %+v", res.Terminal)
	}
	if math.Abs(res.Equity-9992.5) > 1e-9 {
		t.Fatalf("equity = %v; want premium liability marked and fee deducted", res.Equity)
	}
}

func TestTypedFeeModelChargesOptionRollAndExerciseDelivery(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5)}}
	roll := &scriptStrategy{
		actions: []Action{ActionSellCall, ActionBuyCall},
		sizes:   []float64{1, 1},
		pending: []*OptionPosition{
			{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
			{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(5), Lot: 100, AvgPremium: 2},
		},
	}
	model := HKFeeModel(21, 70, 100)
	rollOpts := mkOptionsData(chain, map[string][]float64{"C105": {2, 2}})
	rollOpts.RunSeed = 1
	rollResult, err := RunOptionsWithFeeModel(context.Background(), mkBars(100, 100), 10000, model, roll, rollOpts)
	if err != nil {
		t.Fatalf("roll run error: %v", err)
	}
	if math.Abs(rollResult.Equity-9958) > 1e-9 || rollResult.Fees.TotalAmount != 42 || rollResult.Fees.OptionAmount != 42 || rollResult.Fees.OptionContracts != 2 || rollResult.Fees.OptionTradeCount != 2 {
		t.Fatalf("roll result = %+v; want two option fees totaling 42", rollResult)
	}

	assignment := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{100, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}
	assignmentOpts := mkOptionsData(map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}, map[string][]float64{"C105": {2, 2}})
	assignmentOpts.RunSeed = 1
	assignmentResult, err := RunOptionsWithFeeModel(context.Background(), mkBars(100, 110, 120), 11000, model, assignment, assignmentOpts)
	if err != nil {
		t.Fatalf("assignment run error: %v", err)
	}
	if math.Abs(assignmentResult.Equity-11539) > 1e-9 || assignmentResult.Fees.TotalAmount != 161 || assignmentResult.Fees.StockAmount != 140 || assignmentResult.Fees.OptionAmount != 21 || assignmentResult.Fees.ExerciseDeliveryAmount != 70 || assignmentResult.Fees.ExerciseDeliveryLots != 1 || assignmentResult.Fees.ExerciseDeliveryTradeCount != 1 {
		t.Fatalf("assignment result = %+v; want stock 70 + option 21 + delivery 70", assignmentResult)
	}
	if len(assignmentResult.Trades) != 3 || assignmentResult.Trades[2].Action != "exercise-call" || assignmentResult.Trades[2].Fee != 70 {
		t.Fatalf("assignment trades = %+v; want charged exercise delivery event", assignmentResult.Trades)
	}
	second, err := RunOptionsWithFeeModel(context.Background(), mkBars(100, 110, 120), 11000, model, &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{100, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}, assignmentOpts)
	if err != nil || second.Equity != assignmentResult.Equity || second.Fees != assignmentResult.Fees {
		t.Fatalf("typed fee replay is not deterministic: first=%+v second=%+v err=%v", assignmentResult.Fees, second, err)
	}
}

func TestAttributionLedgerClosesOverFullWheelLifecycle(t *testing.T) {
	// Sell put → assigned at expiry → stock sold below basis. The attribution
	// identity realized = premium − close cost + stock realized − fees must
	// agree with the terminal residual on every leg of the wheel lifecycle.
	chain := map[string]OptionContract{"P95": {Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellPut, ActionHold, ActionHold, ActionSell},
		sizes:   []float64{1, 0, 0, 100},
		pending: []*OptionPosition{{Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2), Lot: 100, AvgPremium: 3}, nil, nil, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"P95": {3, 3, 3, 3}})
	opts.RunSeed = 0
	res, err := RunOptionsWithFeeModel(context.Background(), mkBars(100, 90, 90, 90), 10000, HKFeeModel(21, 70, 100), sc, opts)
	if err != nil {
		t.Fatalf("lifecycle run error: %v", err)
	}
	attr := res.Attribution
	want := PnLAttribution{
		PremiumIncomeAmount:    300, // 1 contract × 3 premium × lot 100
		OptionCloseCostAmount:  0,
		StockRealizedPnLAmount: -500, // 100 shares sold at 90 vs basis 95
		FeesAmount:             161,  // option 21 + exercise delivery 70 + stock sell 70
		RealizedPnLAmount:      -361,
	}
	if attr.PremiumIncomeAmount != want.PremiumIncomeAmount || attr.StockRealizedPnLAmount != want.StockRealizedPnLAmount ||
		attr.FeesAmount != want.FeesAmount || attr.RealizedPnLAmount != want.RealizedPnLAmount ||
		math.Abs(attr.RealizedPnLAmount-(attr.PremiumIncomeAmount-attr.OptionCloseCostAmount+attr.StockRealizedPnLAmount-attr.FeesAmount)) > 1e-9 {
		t.Fatalf("attribution = %+v; want %+v with identity", attr, want)
	}
	if res.Terminal.RealizedPnLAmount == nil || math.Abs(*res.Terminal.RealizedPnLAmount-attr.RealizedPnLAmount) > 1e-9 {
		t.Fatalf("terminal realized %v disagrees with attribution %v", res.Terminal.RealizedPnLAmount, attr.RealizedPnLAmount)
	}
	if attr.UnfilledAttemptCount != 0 || attr.UnfilledAttemptPremium != 0 {
		t.Fatalf("attribution unfilled = %d/%v; want zero on a fully-filled run", attr.UnfilledAttemptCount, attr.UnfilledAttemptPremium)
	}
}

func TestUnfilledAttemptPremiumIsOpportunityCost(t *testing.T) {
	// No quote row for the attempted contract ⇒ maximally illiquid ⇒ failProb
	// clamped to 0.95. The unfilled attempt must book zero P&L but ledger the
	// premium it would have collected.
	chain := map[string]OptionContract{"P95": {Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2)}}
	opts := &OptionsData{Chain: OptionChain{}, Bars: OptionBars{}, RunSeed: 0}
	opts.Chain["P95"] = chain["P95"]
	opts.Bars["P95"] = []ingest.Bar{{Ts: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Open: 3, High: 3, Low: 3, Close: 3, Volume: 100}}
	// Quote batch carries only an unrelated contract: fillQuote(P95) misses.
	opts.QuoteBatches = []QuoteSnapshotBatch{{ObservedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), SnapshotKey: "fixture", Underlying: "U.US", UnderlyingPrice: 100,
		Quotes: []wheel.OptionQuote{{Symbol: "P90", Code: "P90", Bid: 1, Ask: 1.001, Volume: 100_000, OpenInterest: 1_000_000}}}}
	opts.Snapshots = opts.QuoteBatches
	opts.QuoteSnapshots = opts.QuoteBatches
	sc := &scriptStrategy{
		actions: []Action{ActionSellPut, ActionHold},
		sizes:   []float64{1, 0},
		pending: []*OptionPosition{{Code: "P95", Kind: OptionPut, Strike: 95, Expiry: expiryAt(2), Lot: 100, AvgPremium: 3}, nil},
	}
	res, err := RunOptionsWithFeeModel(context.Background(), mkBars(100, 100), 10000, HKFeeModel(21, 70, 100), sc, opts)
	if err != nil {
		t.Fatalf("unfilled run error: %v", err)
	}
	if res.Unfilled.UnfilledCount != 1 || len(res.Trades) != 1 || res.Trades[0].Filled {
		t.Fatalf("trades = %+v; want one unfilled attempt", res.Trades)
	}
	attr := res.Attribution
	if attr.UnfilledAttemptCount != 1 || attr.UnfilledAttemptPremium != 300 || attr.PremiumIncomeAmount != 0 || attr.RealizedPnLAmount != 0 {
		t.Fatalf("attribution = %+v; want unfilled premium 300, zero booked P&L", attr)
	}
}

func TestCoveredCallExerciseDeliversStockAtStrike(t *testing.T) {
	// wheel 既有退出机制:持正股 → 卖 covered call → 到期 ITM 被行权,按行权价
	// 卖出股票,相对持仓成本实现盈亏(行权价 100 vs 成本 90 → +1000)。
	chain := map[string]OptionContract{"C100": {Code: "C100", Kind: OptionCall, Strike: 100, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{100, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C100", Kind: OptionCall, Strike: 100, Expiry: expiryAt(2), Lot: 100, AvgPremium: 3}, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"C100": {3, 3, 3}})
	opts.RunSeed = 0
	res, err := RunOptionsWithFeeModel(context.Background(), mkBars(90, 90, 110), 10000, HKFeeModel(21, 70, 100), sc, opts)
	if err != nil {
		t.Fatalf("covered call run error: %v", err)
	}
	var got []string
	for _, tr := range res.Trades {
		got = append(got, tr.Action)
	}
	want := []string{"buy", "sell-call", "exercise-call"}
	if len(got) != len(want) {
		t.Fatalf("trades = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("trades = %v; want %v", got, want)
		}
	}
	if res.Terminal.StockShares != 0 {
		t.Fatalf("terminal stock shares = %v; want 0 (called away)", res.Terminal.StockShares)
	}
	attr := res.Attribution
	wantAttr := PnLAttribution{
		PremiumIncomeAmount:    300, // 1 张 × 3 权利金 × lot 100
		OptionCloseCostAmount:  0,
		StockRealizedPnLAmount: 1000, // 100 股 × (行权价 100 − 成本 90)
		FeesAmount:             161,  // 买股 70 + 卖 call 21 + 行权交割 70
		RealizedPnLAmount:      1139,
	}
	if attr.PremiumIncomeAmount != wantAttr.PremiumIncomeAmount || attr.StockRealizedPnLAmount != wantAttr.StockRealizedPnLAmount ||
		attr.FeesAmount != wantAttr.FeesAmount || attr.RealizedPnLAmount != wantAttr.RealizedPnLAmount ||
		math.Abs(attr.RealizedPnLAmount-(attr.PremiumIncomeAmount-attr.OptionCloseCostAmount+attr.StockRealizedPnLAmount-attr.FeesAmount)) > 1e-9 {
		t.Fatalf("attribution = %+v; want %+v with identity", attr, wantAttr)
	}
	if res.RealizedReturnAmount != wantAttr.RealizedPnLAmount || math.Abs(res.RealizedReturnPct-0.1139) > 1e-9 {
		t.Fatalf("realized return = %v / %v; want 1139 / 0.1139", res.RealizedReturnAmount, res.RealizedReturnPct)
	}
}

func TestTypedFeeModelChargesStockByLots(t *testing.T) {
	result, err := RunWithFeeModel(context.Background(), mkBars(100, 100), 100000, HKFeeModel(21, 70, 100), &scriptStrategy{
		actions: []Action{ActionBuy, ActionSell},
		sizes:   []float64{500, 500},
		pending: []*OptionPosition{nil, nil},
	})
	if err != nil {
		t.Fatalf("stock lot run error: %v", err)
	}
	if math.Abs(result.Equity-99300) > 1e-9 || result.Fees.TotalAmount != 700 || result.Fees.StockAmount != 700 || result.Fees.StockLots != 10 || result.Fees.StockTradeCount != 2 {
		t.Fatalf("stock lot result = %+v; want 5 lots per side at 70", result)
	}
}

func TestUnfilledOptionTradeDoesNotChargeFee(t *testing.T) {
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellCall},
		sizes:   []float64{1},
		pending: []*OptionPosition{{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}},
	}
	opts := mkOptionsData(chain, map[string][]float64{"C105": {2}})
	opts.QuoteBatches[0].Quotes[0].Bid = 0
	opts.QuoteBatches[0].Quotes[0].Ask = 0
	opts.QuoteBatches[0].Quotes[0].Volume = 0
	opts.QuoteBatches[0].Quotes[0].OpenInterest = 0
	opts.Snapshots = opts.QuoteBatches
	opts.QuoteSnapshots = opts.QuoteBatches
	opts.RunSeed = 1
	res, err := RunOptions(context.Background(), mkBars(100), 10000, 7.5, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if len(res.Trades) != 1 || res.Trades[0].Filled || res.Trades[0].Fee != 0 || res.Trades[0].CashAfter != 10000 {
		t.Fatalf("unfilled option trade = %+v; want no booking and no fee", res.Trades)
	}
	if res.Fees.TotalAmount != 0 || res.Fees.ChargedTradeCount != 0 {
		t.Fatalf("fees = %+v; want no charge for an unfilled attempt", res.Fees)
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
	// long put: paid 150, exercise at 95 on close 90 -> sell 100 shares. With
	// no stock held the whole delivery is bought in at market (90) first, then
	// delivered at strike; the position never goes short. Realized 100*(95-90).
	res, err := RunOptions(context.Background(), mkBars(100, 95, 90), 20000, 0, sc, mkOptionsData(chain, map[string][]float64{"P95": {1.5, 1.5}}))
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if math.Abs(res.Equity-20350) > 1e-9 {
		t.Fatalf("RunOptions() = %+v; want Equity 20350 (cash 20350, no naked short)", res)
	}
	if sc.st.Position != 0 || math.Abs(sc.st.Cash-20350) > 1e-9 || math.Abs(sc.st.StockRealizedPnL-500) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want position 0, cash 20350, stock realized 500", sc.st)
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

func TestShortCallAssignedUndercoveredBuysInShortfall(t *testing.T) {
	// 50 shares held, 1 short call (100 shares) assigned ITM at 120: the
	// shortfall is bought in at market (close 120) before the strike delivery,
	// never opening a naked short. Realized = covered 50*(105-100) + gap
	// 50*(105-120) = -500; cash 10000-5000+200-6000+10500 = 9700.
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{50, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"C105": {2, 2}})
	opts.RunSeed = 1
	res, err := RunOptions(context.Background(), mkBars(100, 110, 120), 10000, 0, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if sc.st.Position != 0 || math.Abs(sc.st.Cash-9700) > 1e-9 || math.Abs(sc.st.StockRealizedPnL+500) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want position 0, cash 9700, stock realized -500", sc.st)
	}
	want := []string{"buy", "sell-call", "exercise-buyin", "exercise-call"}
	if len(res.Trades) != len(want) {
		t.Fatalf("trades = %+v; want %v", res.Trades, want)
	}
	for i, action := range want {
		if res.Trades[i].Action != action {
			t.Fatalf("trade[%d] action = %q; want %q", i, res.Trades[i].Action, action)
		}
	}
	buyIn, deliver := res.Trades[2], res.Trades[3]
	if math.Abs(buyIn.Size-50) > 1e-9 || math.Abs(buyIn.Price-120) > 1e-9 || math.Abs(deliver.Size+100) > 1e-9 || math.Abs(deliver.Price-105) > 1e-9 {
		t.Fatalf("buy-in/delivery legs = %+v / %+v; want 50@120 then -100@105", buyIn, deliver)
	}
	if res.Terminal.AssignmentCount != 1 || sc.st.Position != 0 {
		t.Fatalf("terminal assignment = %+v; want one assignment, no naked short", res.Terminal)
	}
}

func TestShortCallAssignedBareBuysInWholeDelivery(t *testing.T) {
	// No stock at all: the assigned call buys in the entire 100 shares at
	// market before delivering at strike. Realized = 100*(105-120) = -1500.
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionSellCall, ActionHold},
		sizes:   []float64{1, 0},
		pending: []*OptionPosition{{Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"C105": {2, 2}})
	opts.RunSeed = 1
	res, err := RunOptions(context.Background(), mkBars(100, 110, 120), 10000, 0, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if sc.st.Position != 0 || math.Abs(sc.st.Cash-8700) > 1e-9 || math.Abs(sc.st.StockRealizedPnL+1500) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want position 0, cash 8700 (10000+200-12000+10500), stock realized -1500", sc.st)
	}
	if len(res.Trades) != 3 || res.Trades[1].Action != "exercise-buyin" || res.Trades[2].Action != "exercise-call" {
		t.Fatalf("trades = %+v; want buy-in then exercise-call", res.Trades)
	}
}

func TestLongPutExerciseUndercoveredBuysInShortfall(t *testing.T) {
	// Long put exercised at 105 on close 90 while only 50 shares are held: the
	// holder's right to sell 100 must deliver, so the 50-share shortfall is
	// bought in at market first. Realized = 100*(105-95) = 1000, position 0,
	// cash 10000-5000-200+10500-4500 = 10800.
	chain := map[string]OptionContract{"P105": {Code: "P105", Kind: OptionPut, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuy, ActionBuyPut, ActionHold},
		sizes:   []float64{50, 1, 0},
		pending: []*OptionPosition{nil, {Code: "P105", Kind: OptionPut, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"P105": {2, 2}})
	opts.RunSeed = 0
	res, err := RunOptions(context.Background(), mkBars(100, 95, 90), 10000, 0, sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	if sc.st.Position != 0 || math.Abs(sc.st.Cash-10800) > 1e-9 || math.Abs(sc.st.StockRealizedPnL-1000) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want position 0, cash 10800, stock realized 1000", sc.st)
	}
	if len(res.Trades) != 4 || res.Trades[2].Action != "exercise-buyin" || res.Trades[3].Action != "exercise-put" {
		t.Fatalf("trades = %+v; want buy-in then exercise-put", res.Trades)
	}
}

func TestShortCallAssignedUndercoveredChargesDeliveryFees(t *testing.T) {
	// Typed fee model: buy-in is a stock trade (70/lot) and the strike delivery
	// charges exercise delivery (70/lot) on top of the 21/contract option fee.
	chain := map[string]OptionContract{"C105": {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2)}}
	sc := &scriptStrategy{
		actions: []Action{ActionBuy, ActionSellCall, ActionHold},
		sizes:   []float64{50, 1, 0},
		pending: []*OptionPosition{nil, {Code: "C105", Kind: OptionCall, Strike: 105, Expiry: expiryAt(2), Lot: 100, AvgPremium: 2}, nil},
	}
	opts := mkOptionsData(chain, map[string][]float64{"C105": {2, 2}})
	opts.RunSeed = 1
	res, err := RunOptionsWithFeeModel(context.Background(), mkBars(100, 110, 120), 10000, HKFeeModel(21, 70, 100), sc, opts)
	if err != nil {
		t.Fatalf("RunOptions() error: %v", err)
	}
	// cash 10000 -5000 -70(buy) -21 +200 -6000 -70(buy-in) +10500 -70(delivery) = 9469.
	if sc.st.Position != 0 || math.Abs(sc.st.Cash-9469) > 1e-9 {
		t.Fatalf("post-settle state = %+v; want position 0, cash 9469", sc.st)
	}
	if res.Fees.TotalAmount != 231 || res.Fees.OptionAmount != 21 || res.Fees.StockAmount != 210 || res.Fees.ExerciseDeliveryAmount != 140 || res.Fees.ExerciseDeliveryLots != 2 || res.Fees.ExerciseDeliveryTradeCount != 2 {
		t.Fatalf("fees = %+v; want option 21 + stock 70 + two delivery lots 140", res.Fees)
	}
}
