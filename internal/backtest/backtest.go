package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
)

// Result summarizes one backtest run; EquityCurve/Trades are the deterministic
// per-bar trace (draft 2026-08-02 S1: same input, same trace). Unfilled
// reports the liquidity-heuristic fill accounting (unfilled.go).
//
// Evaluation basis (2026-08-14 老板指令): RealizedReturn* is the headline.
// Unrealized marks (Equity/TotalReturn) depend on the strategic position
// curve (满仓/清仓/最大持股 decide end-of-window holdings), so evaluation
// uses only booked P&L; the mark-to-market fields remain as audit.
type Result struct {
	Equity                    float64
	TotalReturn               float64
	RealizedReturnAmount      float64
	RealizedReturnPct         float64
	MaxDrawdown               float64
	MaxStockMarketValueAmount float64
	Bars                      int
	EquityCurve               []EquityPoint
	Trades                    []Trade
	Signals                   []SignalTrace
	Unfilled                  UnfilledStats
	Fees                      FeeSummary
	Attribution               PnLAttribution
	Terminal                  TerminalSummary
	DataQuality               DataQualitySummary
}

// FeeSummary is the fee ledger for fills produced by one run. Legacy runs use
// the historical fixed-per-trade amount; typed runs charge options by
// contract and stock/exercise delivery by lot. Unfilled attempts and OTM
// mechanical expiry events are never charged.
type FeeSummary struct {
	Included                   bool    `json:"included"`
	Legacy                     bool    `json:"legacy"`
	PerTrade                   float64 `json:"per_trade"`
	OptionPerContract          float64 `json:"option_per_contract"`
	StockPerLot                float64 `json:"stock_per_lot"`
	LotSize                    int     `json:"lot_size"`
	OptionContracts            float64 `json:"option_contracts"`
	StockLots                  float64 `json:"stock_lots"`
	ExerciseDeliveryLots       float64 `json:"exercise_delivery_lots"`
	TotalAmount                float64 `json:"total_amount"`
	StockAmount                float64 `json:"stock_amount"`
	OptionAmount               float64 `json:"option_amount"`
	ExerciseDeliveryAmount     float64 `json:"exercise_delivery_amount"`
	ChargedTradeCount          int64   `json:"charged_trade_count"`
	OptionTradeCount           int64   `json:"option_trade_count"`
	StockTradeCount            int64   `json:"stock_trade_count"`
	ExerciseDeliveryTradeCount int64   `json:"exercise_delivery_trade_count"`
}

// UnfilledStats is one run's fill accounting. UnfilledRatio is nil (null in
// JSON) when no attempt occurred — an empty denominator is never 0%. The
// model description is present even when no attempt was sampled.
type UnfilledStats struct {
	AttemptCount  int64    `json:"attempt_count"`
	FillCount     int64    `json:"fill_count"`
	UnfilledCount int64    `json:"unfilled_count"`
	UnfilledRatio *float64 `json:"unfilled_ratio"`
	ModelKind     string   `json:"model_kind"`
	ModelVersion  string   `json:"model_version"`
}

// SignalTrace is the deterministic per-bar decision trace. CandidateCode is
// empty for HOLD; Candidates preserves the accepted/considered contract codes
// when the strategy exposes a Wheel signal. Inventory fields are duplicated
// explicitly so the trace remains useful without decoding domain JSON.
type SignalTrace struct {
	Ts                 time.Time                   `json:"ts"`
	Action             string                      `json:"action"`
	Direction          string                      `json:"direction"`
	Reason             string                      `json:"reason"`
	CapabilityStatus   string                      `json:"capability_status"`
	BlockedBy          []string                    `json:"blocked_by"`
	SnapshotKey        string                      `json:"snapshot_key,omitempty"`
	SnapshotObservedAt *time.Time                  `json:"snapshot_observed_at,omitempty"`
	ActualInventory    float64                     `json:"actual_inventory"`
	EffectiveInventory float64                     `json:"effective_inventory"`
	OptionDeltaStock   float64                     `json:"option_delta_stock"`
	Inventory          float64                     `json:"inventory"`
	CandidateCode      string                      `json:"candidate_code,omitempty"`
	Candidate          string                      `json:"candidate,omitempty"`
	Candidates         []string                    `json:"candidates,omitempty"`
	Quantity           float64                     `json:"quantity"`
	UnderlyingPrice    float64                     `json:"underlying_price"`
	CashBefore         float64                     `json:"cash_before"`
	CashAfter          float64                     `json:"cash_after"`
	TargetInventory    float64                     `json:"target_inventory"`
	InventoryGap       float64                     `json:"inventory_gap"`
	CandidateDetails   []wheel.CandidateEvaluation `json:"candidate_details,omitempty"`
}

// EquityPoint is one bar's portfolio equity at the bar timestamp.
type EquityPoint struct {
	Ts     time.Time `json:"ts"`
	Equity float64   `json:"equity"`
}

// Trade is one settled trade event: fills at the bar close (stock at close,
// option at its per-share market premium), expiry events exercise ITM legs at the
// strike or void OTM legs. Symbol is the option contract code for legs and is
// filled with the underlying symbol by SaveResult for stock trades.
// Filled=false marks a simulated unfilled sell attempt (no booking, cash and
// positions unchanged); UnfilledModel then names the verdict's model, and is
// empty for fills and non-option trades (manual unexecuted paths are a later
// slice, plan §二).
type Trade struct {
	Ts            time.Time `json:"ts"`
	Action        string    `json:"action"` // buy/sell/*-call/*-put/exercise-call/exercise-put/exercise-buyin/expire-otm
	Symbol        string    `json:"symbol"`
	Size          float64   `json:"size"`  // shares for stock/exercise, contracts for option fills
	Price         float64   `json:"price"` // close, premium, or strike
	CashAfter     float64   `json:"cash_after"`
	Fee           float64   `json:"fee"`
	Filled        bool      `json:"filled"`
	UnfilledModel string    `json:"unfilled_model,omitempty"`
}

// buyTol tolerates one-ulp float error in BuyHold's all-in size (cash/close).
const buyTol = 1e-9

// Run replays bars ascending, calling the strategy once per bar and settling
// trades at the close; no option leg data. details: doc/BACKTEST.md
func Run(ctx context.Context, bars []ingest.Bar, initialCash float64, feePerTrade float64, s Strategy) (*Result, error) {
	return RunOptions(ctx, bars, initialCash, feePerTrade, s, nil)
}

// RunOptions replays bars like Run with an injected option universe (chain +
// per-contract bars); option legs settle mechanically at expiry and Equity
// marks them to their latest option close. nil opts = Run.
func RunOptions(ctx context.Context, bars []ingest.Bar, initialCash float64, feePerTrade float64, s Strategy, opts *OptionsData) (*Result, error) {
	return RunOptionsWithFeeModel(ctx, bars, initialCash, LegacyFeeModel(feePerTrade), s, opts)
}

// RunWithFeeModel replays bars with the type-specific fee schedule and no
// option quote data. Run and RunOptions remain the fixed-fee compatibility
// entry points.
func RunWithFeeModel(ctx context.Context, bars []ingest.Bar, initialCash float64, feeModel FeeModel, s Strategy) (*Result, error) {
	return RunOptionsWithFeeModel(ctx, bars, initialCash, feeModel, s, nil)
}

// RunOptionsWithFeeModel is the fee-aware replay entry point. The legacy
// RunOptions signature intentionally stays unchanged so existing callers and
// acceptance scripts continue to compile and retain their fixed-fee output.
func RunOptionsWithFeeModel(ctx context.Context, bars []ingest.Bar, initialCash float64, feeModel FeeModel, s Strategy, opts *OptionsData) (*Result, error) {
	if len(bars) == 0 {
		return nil, errors.New("backtest: empty bars")
	}
	if initialCash <= 0 {
		return nil, errors.New("backtest: initial cash must be > 0")
	}
	if err := feeModel.validate(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("backtest: nil strategy")
	}
	if err := ingest.ValidateBars(bars); err != nil {
		return nil, err
	}

	st := &State{
		Cash:     initialCash,
		Options:  map[string]OptionPosition{},
		OptPrice: map[string]float64{},
	}
	seed := int64(defaultRunSeed)
	if opts != nil {
		st.Chain = opts.Chain
		st.OptBars = opts.Bars
		if opts.RunSeed != 0 {
			seed = opts.RunSeed
		}
	}
	var (
		peak, maxDD, maxStockMarketValue float64
		curve                            []EquityPoint
		trades                           []Trade
		signals                          []SignalTrace
	)
	for i, b := range bars {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		st.Pending = nil
		st.Price = b.Close
		// DATA_BLOCKED: this remains bar-time replay with the latest atomic
		// snapshot at or before the bar. Without a trusted event timeline we do
		// not claim event-driven historical execution semantics.
		st.QuoteBatch = latestQuoteBatch(opts, b.Ts)
		st.Quotes, st.ObservedAt, st.SnapshotKey = nil, time.Time{}, ""
		if st.QuoteBatch != nil {
			st.Quotes = st.QuoteBatch.Quotes
			st.ObservedAt, st.SnapshotKey = st.QuoteBatch.ObservedAt, st.QuoteBatch.SnapshotKey
		}
		markOptions(st, b.Ts)
		cashBefore := st.Cash
		act, size, err := s.OnBar(ctx, b, st)
		if err != nil {
			return nil, fmt.Errorf("backtest: bar %d: strategy: %w", i, err)
		}
		if err := settleAction(st, act, size, b, feeModel, seed, &trades); err != nil {
			return nil, fmt.Errorf("backtest: bar %d: %w", i, err)
		}
		signals = append(signals, makeSignalTrace(b.Ts, s, st, act, size, cashBefore))
		settleExpired(st, b.Ts, feeModel, &trades)
		eq := st.Equity(b.Close)
		if stockMarketValue := math.Abs(st.Position * b.Close); stockMarketValue > maxStockMarketValue {
			maxStockMarketValue = stockMarketValue
		}
		curve = append(curve, EquityPoint{Ts: b.Ts, Equity: eq})
		if eq > peak {
			peak = eq
		}
		if peak > 0 && (peak-eq)/peak > maxDD {
			maxDD = (peak - eq) / peak
		}
	}

	terminal := terminalSummary(st, initialCash, bars[len(bars)-1].Close)
	final := st.Equity(bars[len(bars)-1].Close)
	if terminal.FinalEquityAmount != nil {
		final = *terminal.FinalEquityAmount
	}
	unfilled := UnfilledStats{
		AttemptCount:  st.AttemptCount,
		FillCount:     st.FillCount,
		UnfilledCount: st.UnfilledCount,
		ModelKind:     modelKind,
		ModelVersion:  modelVersion,
	}
	fees := summarizeFees(trades, feeModel)
	if st.AttemptCount > 0 {
		ratio := float64(st.UnfilledCount) / float64(st.AttemptCount)
		unfilled.UnfilledRatio = &ratio
	}
	attr := attributionOf(st, fees, unfilled)
	return &Result{
		Equity:                    final,
		TotalReturn:               (final - initialCash) / initialCash,
		RealizedReturnAmount:      attr.RealizedPnLAmount,
		RealizedReturnPct:         attr.RealizedPnLAmount / initialCash,
		MaxDrawdown:               maxDD,
		MaxStockMarketValueAmount: maxStockMarketValue,
		Bars:                      len(bars),
		EquityCurve:               curve,
		Trades:                    trades,
		Signals:                   signals,
		Unfilled:                  unfilled,
		Fees:                      fees,
		Attribution:               attr,
		Terminal:                  terminal,
		DataQuality:               summarizeDataQuality(bars, opts, signals),
	}, nil
}

func summarizeFees(trades []Trade, feeModel FeeModel) FeeSummary {
	feeModel = feeModel.normalized()
	fees := FeeSummary{Included: true, PerTrade: feeModel.LegacyPerTrade, OptionPerContract: feeModel.OptionPerContract, StockPerLot: feeModel.StockPerLot, LotSize: feeModel.LotSize, Legacy: feeModel.Legacy}
	for _, tr := range trades {
		if !feeBearingTrade(tr) {
			continue
		}
		fees.ChargedTradeCount++
		fees.TotalAmount += tr.Fee
		switch {
		case isExerciseTrade(tr.Action):
			fees.StockAmount += tr.Fee
			fees.ExerciseDeliveryAmount += tr.Fee
			fees.ExerciseDeliveryLots += stockLots(tr.Size, fees.LotSize)
			fees.ExerciseDeliveryTradeCount++
		case isStockTrade(tr.Action):
			fees.StockAmount += tr.Fee
			fees.StockLots += stockLots(tr.Size, fees.LotSize)
			fees.StockTradeCount++
		case isOptionTrade(tr.Action):
			fees.OptionAmount += tr.Fee
			fees.OptionContracts += absTradeSize(tr.Size)
			fees.OptionTradeCount++
		}
	}
	return fees
}

func absTradeSize(size float64) float64 { return math.Abs(size) }

func feeBearingTrade(tr Trade) bool {
	if tr.UnfilledModel != "" && !tr.Filled {
		return false
	}
	return isActiveTrade(tr.Action) || isExerciseTrade(tr.Action)
}

func latestQuoteBatch(opts *OptionsData, ts time.Time) *QuoteSnapshotBatch {
	if opts == nil {
		return nil
	}
	batches := optionQuoteBatches(opts)
	var best *QuoteSnapshotBatch
	for i := range batches {
		b := &batches[i]
		if b.ObservedAt.After(ts) {
			continue
		}
		if best == nil || b.ObservedAt.After(best.ObservedAt) || (b.ObservedAt.Equal(best.ObservedAt) && preferQuoteBatch(b, best)) {
			best = b
		}
	}
	return best
}

// preferQuoteBatch resolves only exact observed_at ties. A genuinely newer
// observation always wins; at the same instant live Futu is preferred over
// the HKEX EOD research projection, then other providers use lexical source
// order. snapshot_key remains the final deterministic tie-breaker.
func preferQuoteBatch(candidate, current *QuoteSnapshotBatch) bool {
	candidateSource, currentSource := quoteBatchSource(candidate), quoteBatchSource(current)
	candidateRank, currentRank := quoteBatchSourceRank(candidateSource), quoteBatchSourceRank(currentSource)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	if candidateSource != currentSource {
		return candidateSource < currentSource
	}
	return candidate.SnapshotKey > current.SnapshotKey
}

func quoteBatchSource(batch *QuoteSnapshotBatch) string {
	if batch == nil || len(batch.Quotes) == 0 {
		return ""
	}
	source := strings.ToLower(strings.TrimSpace(batch.Quotes[0].Source))
	for _, quote := range batch.Quotes[1:] {
		candidate := strings.ToLower(strings.TrimSpace(quote.Source))
		if candidate != "" && (source == "" || candidate < source) {
			source = candidate
		}
	}
	return source
}

func quoteBatchSourceRank(source string) int {
	switch source {
	case "futu":
		return 3
	case "hkex":
		return 2
	case "":
		return 0
	default:
		return 1
	}
}

type signalProvider interface{ Signal() wheel.Signal }

// candidateDetailTracer is implemented by strategies that can suppress the
// per-bar candidate audit materialization. When a strategy opts out, each
// signal keeps only its small decision metadata (action/reason/inventory/
// chosen candidate) instead of one CandidateEvaluation per chain contract —
// the full candidate list is deterministically re-fetchable by re-running with
// tracing enabled (doc/BACKTEST.md).
type candidateDetailTracer interface {
	KeepCandidateDetails() bool
}

func makeSignalTrace(ts time.Time, s Strategy, st *State, act Action, size, cashBefore float64) SignalTrace {
	t := SignalTrace{Ts: ts, Action: act.String(), Direction: wheel.DirectionHold, CapabilityStatus: wheel.CapabilityReady, BlockedBy: []string{}, Quantity: size, ActualInventory: st.Position, EffectiveInventory: st.Position, Inventory: st.Position, SnapshotKey: st.SnapshotKey, UnderlyingPrice: st.Price, CashBefore: cashBefore, CashAfter: st.Cash}
	if !st.ObservedAt.IsZero() {
		observedAt := st.ObservedAt
		t.SnapshotObservedAt = &observedAt
	}
	if p, ok := s.(signalProvider); ok {
		sig := p.Signal()
		t.Action, t.Direction, t.Reason = string(sig.Action), string(sig.Direction), sig.Reason
		if sig.CapabilityStatus != "" {
			t.CapabilityStatus = sig.CapabilityStatus
		}
		t.BlockedBy = append([]string{}, sig.BlockedBy...)
		t.ActualInventory, t.EffectiveInventory, t.OptionDeltaStock = sig.ActualInventory, sig.EffectiveInventory, sig.OptionDeltaStock
		t.TargetInventory, t.InventoryGap = sig.TargetInventory, sig.InventoryGap
		if keep, ok := s.(candidateDetailTracer); !ok || keep.KeepCandidateDetails() {
			t.CandidateDetails = append([]wheel.CandidateEvaluation(nil), sig.Candidates...)
		}
		t.Inventory = sig.EffectiveInventory
		t.Quantity = float64(sig.Quantity)
		for _, c := range sig.Candidates {
			name := c.Quote.Symbol
			if name == "" {
				name = c.Quote.Code
			}
			if name != "" {
				t.Candidates = append(t.Candidates, name)
			}
		}
		if sig.Quote != nil {
			t.CandidateCode = sig.Quote.Symbol
			if t.CandidateCode == "" {
				t.CandidateCode = sig.Quote.Code
			}
			t.Candidate = t.CandidateCode
		}
	}
	return t
}

// markOptions fills OptPrice with each open leg's latest close at or before ts.
func markOptions(st *State, ts time.Time) {
	for code := range st.Options {
		if price, ok := st.PriceAt(code, ts); ok {
			st.OptPrice[code] = price
		}
	}
}

// settleAction books one bar's trade: stock trades at the close, option trades
// at the pending contract's latest close (size = contracts). seed drives the
// unfilled-attempt draws for sell actions (unfilled.go).
func settleAction(st *State, act Action, size float64, b ingest.Bar, feeModel FeeModel, seed int64, trades *[]Trade) error {
	switch act {
	case ActionHold:
	case ActionBuy:
		if size < 0 || size*b.Close > st.Cash+buyTol {
			return fmt.Errorf("buy %v shares at close %v exceeds cash %v", size, b.Close, st.Cash)
		}
		fee := feeModel.fee("buy", size)
		st.Cash -= size*b.Close + fee
		applyStockPosition(st, size, b.Close)
		*trades = append(*trades, Trade{Ts: b.Ts, Action: "buy", Size: size, Price: b.Close, CashAfter: st.Cash, Fee: fee, Filled: true})
	case ActionSell:
		if size < 0 || size > st.Position+buyTol {
			return fmt.Errorf("sell %v shares exceeds position %v", size, st.Position)
		}
		fee := feeModel.fee("sell", size)
		st.Cash += size*b.Close - fee
		st.StockRealizedPnL += size * (b.Close - st.StockAverageCost)
		applyStockPosition(st, -size, b.Close)
		*trades = append(*trades, Trade{Ts: b.Ts, Action: "sell", Size: size, Price: b.Close, CashAfter: st.Cash, Fee: fee, Filled: true})
	case ActionSellCall, ActionBuyCall, ActionSellPut, ActionBuyPut:
		return settleOptionTrade(st, act, size, b, feeModel, seed, trades)
	default:
		return fmt.Errorf("unknown action %s", act)
	}
	return nil
}

// settleOptionTrade books one option action: size contracts of the pending
// contract at its latest close, enforcing the CSP cash reserve on short puts.
// Sell actions first sample the unfilled-attempt model (unfilled.go): an
// unfilled attempt books nothing (cash/positions unchanged) but stays in the
// ledger as a Filled:false Trade, and Pending is consumed so later bars never
// assume the order filled. Buys settle unconditionally (no sampling).
func settleOptionTrade(st *State, act Action, size float64, b ingest.Bar, feeModel FeeModel, seed int64, trades *[]Trade) error {
	if size <= 0 {
		return fmt.Errorf("%s size %v; want > 0 contracts", act, size)
	}
	p := st.Pending
	if p == nil {
		return fmt.Errorf("%s without a pending option (set State.Pending before the action)", act)
	}
	want := OptionCall
	if act == ActionSellPut || act == ActionBuyPut {
		want = OptionPut
	}
	if p.Kind != want {
		return fmt.Errorf("%s pending kind %q; want %q", act, p.Kind, want)
	}
	lot := p.Lot
	if lot <= 0 {
		lot = p.LotSize
	}
	if p.Code == "" || p.Strike <= 0 || p.Expiry.IsZero() || lot <= 0 {
		return fmt.Errorf("%s pending option incomplete: code=%q strike=%v expiry=%v lot=%d", act, p.Code, p.Strike, p.Expiry, p.Lot)
	}
	price, ok := st.PriceAt(p.Code, b.Ts)
	if !ok {
		return fmt.Errorf("%s %s: no option price data at or before %s", act, p.Code, b.Ts.Format(time.RFC3339))
	}
	sign := 1.0
	if act == ActionSellCall || act == ActionSellPut {
		sign = -1
	}
	contracts := sign * size
	// Option market prices are quoted per underlying share. Sells receive and
	// buys pay price × contract lot size; direction rides on the signed
	// contract count. Keeping this multiplier here also makes the cash ledger
	// use the same unit as option mark-to-market in State.Equity.
	flow := -contracts * p.AvgPremium * float64(lot)
	fee := feeModel.fee(act.String(), size)
	netFlow := flow - fee
	if flow < 0 && st.Cash+netFlow < -buyTol {
		return fmt.Errorf("%s %v contracts costs %v plus fee %v; exceeds cash %v", act, size, -flow, fee, st.Cash)
	}
	if act == ActionSellPut {
		reserve := shortPutCashReserve(st.Options) + size*float64(lot)*p.Strike
		if st.Cash+netFlow < reserve {
			return fmt.Errorf("sell-put %v contracts needs cash reserve %v cumulative across open short puts, have %v",
				size, reserve, st.Cash+netFlow)
		}
	}
	if sign < 0 {
		// A sell attempt consumes the daily order budget and samples the
		// unfilled model; the fill input comes from the current atomic quote
		// (missing quote = no market info = maximally illiquid).
		st.DailyOrders++
		st.AttemptCount++
		if st.AttemptsByContract == nil {
			st.AttemptsByContract = map[string]int64{}
		}
		st.AttemptsByContract[p.Code]++
		symbol := ""
		if st.QuoteBatch != nil {
			symbol = st.QuoteBatch.Underlying
		}
		bid, ask, vol, oi, _ := fillQuote(st, p.Code)
		if attemptDraw(seed, symbol, p.Code, b.Ts, st.AttemptsByContract[p.Code]) < failProb(bid, ask, vol, oi) {
			st.UnfilledCount++
			st.UnfilledAttemptPremium += size * p.AvgPremium * float64(lot)
			*trades = append(*trades, Trade{Ts: b.Ts, Action: act.String(), Symbol: p.Code, Size: size, Price: p.AvgPremium, CashAfter: st.Cash, Filled: false, UnfilledModel: unfilledModelLabel()})
			st.Pending = nil
			return nil
		}
		st.FillCount++
		st.PremiumIncome += size * p.AvgPremium * float64(lot)
	} else {
		// Filled closing buys are never sampled; debit their cost for the ledger.
		st.OptionCloseCost += size * p.AvgPremium * float64(lot)
	}
	pos := OptionPosition{
		Code: p.Code, Kind: p.Kind, Strike: p.Strike, Expiry: p.Expiry,
		Lot: lot, LotSize: lot, Contracts: contracts, AvgPremium: p.AvgPremium,
		MarketDelta: p.MarketDelta, Delta: p.Delta,
	}
	if old, ok := st.Options[p.Code]; ok {
		pos = mergeOptionPosition(old, pos)
	}
	st.Cash += netFlow
	*trades = append(*trades, Trade{Ts: b.Ts, Action: act.String(), Symbol: p.Code, Size: size, Price: p.AvgPremium, CashAfter: st.Cash, Fee: fee, Filled: true})
	if pos.Contracts == 0 {
		delete(st.Options, p.Code)
	} else {
		st.Options[p.Code] = pos
	}
	st.OptPrice[p.Code] = price
	st.Pending = nil
	return nil
}

// fillQuote returns the pending contract's market from the current atomic
// quote batch: bid/ask for the spread component, volume and open interest for
// the liquidity terms. ok=false when the batch has no quote for the contract
// (callers then treat all inputs as missing: maximally illiquid).
func fillQuote(st *State, code string) (bid, ask float64, vol, oi int64, ok bool) {
	quotes := st.Quotes
	if st.QuoteBatch != nil {
		quotes = st.QuoteBatch.Quotes
	}
	for _, q := range quotes {
		name := q.Symbol
		if name == "" {
			name = q.Code
		}
		if name == code {
			return q.Bid, q.Ask, q.Volume, q.OpenInterest, true
		}
	}
	return 0, 0, 0, 0, false
}

func shortPutCashReserve(positions map[string]OptionPosition) float64 {
	var reserve float64
	for _, p := range positions {
		if p.Kind != OptionPut || p.Contracts >= 0 || p.Strike <= 0 {
			continue
		}
		lot := p.Lot
		if lot <= 0 {
			lot = p.LotSize
		}
		if lot > 0 {
			reserve += -p.Contracts * p.Strike * float64(lot)
		}
	}
	return reserve
}

// settleExpired exercises ITM legs (call: shares out at strike, put: shares in
// at strike) and voids OTM legs once their expiry date has passed; the leg is
// always removed. ITM exercise and OTM void both land in the trade ledger.
// Expiring legs are processed in contract-code order: map iteration alone is
// random, and same-day expiries must produce a stable ledger (determinism).
func settleExpired(st *State, ts time.Time, feeModel FeeModel, trades *[]Trade) {
	codes := make([]string, 0, len(st.Options))
	for code, p := range st.Options {
		if !ts.Before(p.Expiry) {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	for _, code := range codes {
		p := st.Options[code]
		lot := p.Lot
		if lot <= 0 {
			lot = p.LotSize
		}
		shares := p.Contracts * float64(lot)
		st.ExpiryCount++
		if p.Contracts < 0 {
			st.ShortExpiryCount++
		}
		switch p.Kind {
		case OptionCall:
			if st.Price > p.Strike {
				if p.Contracts < 0 {
					st.AssignmentCount++
				}
				if shares < 0 {
					// Short-call delivery sells shares out at the strike: realize
					// against the running stock basis. This is the wheel's covered
					// call exit (inventory above target sells calls; exercise
					// delivers the stock out at strike).
					// A position that does not cover the whole delivery buys in
					// the shortfall at market first (exchange buy-in): the
					// assigned seller must deliver every contract share, never
					// open a naked short. Buy-in cost becomes part of the basis,
					// so the realized line below prices all deliver shares.
					deliver := -shares
					if gap := deliver - st.Position; gap > 0 {
						buyInFee := feeModel.fee("exercise-buyin", gap)
						st.Cash -= gap*st.Price + buyInFee
						applyStockPosition(st, gap, st.Price)
						*trades = append(*trades, Trade{Ts: ts, Action: "exercise-buyin", Symbol: code, Size: gap, Price: st.Price, CashAfter: st.Cash, Fee: buyInFee})
					}
					st.StockRealizedPnL += deliver * (p.Strike - st.StockAverageCost)
				}
				applyStockPosition(st, shares, p.Strike)
				fee := feeModel.fee("exercise-call", shares)
				st.Cash -= shares*p.Strike + fee
				*trades = append(*trades, Trade{Ts: ts, Action: "exercise-call", Symbol: code, Size: shares, Price: p.Strike, CashAfter: st.Cash, Fee: fee})
			} else {
				*trades = append(*trades, Trade{Ts: ts, Action: "expire-otm", Symbol: code, CashAfter: st.Cash})
			}
		case OptionPut:
			if st.Price < p.Strike {
				if p.Contracts < 0 {
					st.AssignmentCount++
				}
				if p.Contracts > 0 {
					// Long-put exercise sells the stock out at strike (the
					// holder's right to put it). Undercovered positions buy in
					// the shortfall at market first, same delivery rule as the
					// short-call assignment above.
					deliver := shares
					if gap := deliver - st.Position; gap > 0 {
						buyInFee := feeModel.fee("exercise-buyin", gap)
						st.Cash -= gap*st.Price + buyInFee
						applyStockPosition(st, gap, st.Price)
						*trades = append(*trades, Trade{Ts: ts, Action: "exercise-buyin", Symbol: code, Size: gap, Price: st.Price, CashAfter: st.Cash, Fee: buyInFee})
					}
					st.StockRealizedPnL += deliver * (p.Strike - st.StockAverageCost)
				}
				applyStockPosition(st, -shares, p.Strike)
				fee := feeModel.fee("exercise-put", shares)
				st.Cash += shares*p.Strike - fee
				*trades = append(*trades, Trade{Ts: ts, Action: "exercise-put", Symbol: code, Size: shares, Price: p.Strike, CashAfter: st.Cash, Fee: fee})
			} else {
				*trades = append(*trades, Trade{Ts: ts, Action: "expire-otm", Symbol: code, CashAfter: st.Cash})
			}
		}
		delete(st.Options, code)
	}
}

// barRecord is the JSON wire format of one bar, matching `ingest bars -json`.
type barRecord struct {
	Ts     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

// ParseBars parses a JSON array of bars in `ingest bars -json` format; Run checks sanity/order.
func ParseBars(data []byte) ([]ingest.Bar, error) {
	var recs []barRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("backtest: parse bars: json: %w", err)
	}
	out := make([]ingest.Bar, 0, len(recs))
	for i, r := range recs {
		ts, err := time.Parse(time.RFC3339Nano, r.Ts)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, r.Ts)
			if err != nil {
				return nil, fmt.Errorf("backtest: parse bars: record %d ts: %w", i, err)
			}
		}
		out = append(out, ingest.Bar{
			Ts: ts.UTC(), Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		})
	}
	return out, nil
}
