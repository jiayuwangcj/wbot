// Runner drives the live wheel loop: for every watchlist binding it loads the
// latest config, pulls a current price, positions, an option chain window and
// contract quotes, then evaluates a wheel decision and persists the signal
// (watchlist execution status stays in sync). All dependencies are injected
// so tests drive the loop with fakes; the futu REST client satisfies the
// quote/chain interfaces and the proto TradeClient the positions source.

package wheelrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/strategy"
	"github.com/jiayu/wbot/internal/watchlist"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// Quoter supplies the underlying's current price and batch option quotes
// (futu.Client satisfies both; fakes in tests).
type Quoter interface {
	Quote(ctx context.Context, symbol string) (float64, error)
	OptionQuotes(ctx context.Context, symbols []string) (map[string]futu.OptionQuoteEx, error)
}

// OptionChainer lists call/put contracts in a closed DTE window (futu.Client).
type OptionChainer interface {
	OptionChain(ctx context.Context, symbol string, begin, end time.Time) ([]futu.OptionContract, error)
}

// WatchlistLister lists wheel bindings and syncs their execution status
// (adapted from the watchlist package so the runner stays DB-free).
type WatchlistLister interface {
	List(ctx context.Context) ([]watchlist.Item, error)
	SetExecutionStatus(ctx context.Context, symbol, status, reason string) error
}

// Dependencies is the runner's full injectable surface. Positions is the
// slice-B TradePositions interface (positions.go).
type Dependencies struct {
	Quoter      Quoter
	Positions   TradePositions
	Chain       OptionChainer
	Store       wheelstore.SignalRepository
	Watchlist   WatchlistLister
	LLMReviewer llmreview.Reviewer
	LLMModel    string
}

// Runner evaluates every wheel binding on one sequential pass. It is not
// safe for concurrent use; a single goroutine owns it (Run).
type Runner struct {
	deps Dependencies
}

func NewRunner(deps Dependencies) *Runner { return &Runner{deps: deps} }

// fallbackBlocker keeps the validateSignal contract (DATA_BLOCKED must have
// at least one blocker) when Evaluate reports a data block without naming it.
const fallbackBlocker = "no complete quote snapshot"

// 与 doc/WHEEL_STRATEGY.md「LLM 审核规则摘要（单一来源）」小节同步，文档为源。
const wheelReviewRules = `仅审核 wheel 区间策略；信号只能是 ALERT 或 HOLD，审核不得触发自动下单。策略在价格区间内通过卖出现金担保 Put 或备兑 Call 收取权利金，并在价格超出区间时依照价格-目标库存曲线调整风险敞口：目标库存高于有效库存时只能考虑卖 Put 增加库存敞口，目标库存低于有效库存时只能考虑卖 Call 降低库存敞口。
当前情况由标的现价、策略配置版本、现金可用额及股票/期权持仓组成；signal 描述提示动作、方向、卖出合约数、候选报价、当前/目标/有效/交易后库存、库存缺口、能力状态和阻断原因；预期收益 expected_gain 是按卖价 Bid × 合约乘数 × 数量估算的毛权利金，不含手续费、滑点、税费及指派损益，不代表保证收益，缺失或为零不得推断为有收益。
必须逐项审核：
1. 方向反转检查（硬性项）：核对 signal.direction 与当前持仓、effective_inventory、inventory_gap、target_inventory 及价格-目标库存曲线一致；缺口为正时卖 Put、缺口为负时卖 Call，卖出/买入符号与目标库存变化必须一致，任何方向反转或符号矛盾都必须 REJECT。
2. 策略参数：核对 min_dte/max_dte、价格区间、max_inventory、max_daily_orders、strategic_state 及候选合约参数。
3. 数据质量：报价必须完整且新鲜，Bid/Ask 正数且未倒挂，IV/Delta/Theta 合理，Volume/OI 非零；不得用缺失 Greeks 或过期、拼接数据作判断。
4. 资金与库存：核对现金/保证金预算、最大库存、Put 指派承诺、Call 备兑数量、交易后有效库存和 extreme 每日限制。
5. 系统性错误：排查闭市或停牌误判、同一合约重复动作、与现有持仓或历史动作矛盾、合约类型/到期日/乘数错误及 Greeks 缺失。
6. 数据不足：capability_status 为 DATA_BLOCKED、blocked_by 非空，或任一关键字段不足时必须 REJECT；不得以 expected_gain 补偿或放宽任何校验。`

// RunOnce evaluates every wheel binding once. A per-symbol failure is logged
// and does not stop the remaining symbols; the returned error (nil when all
// succeeded) reports the failing count.
func (r *Runner) RunOnce(ctx context.Context) error {
	items, err := r.deps.Watchlist.List(ctx)
	if err != nil {
		return fmt.Errorf("wheelrun: watchlist: %w", err)
	}
	failed := 0
	wheelBindings := 0
	for _, it := range items {
		if it.Strategy != "wheel" {
			continue
		}
		wheelBindings++
		if err := r.runSymbol(ctx, it.Symbol); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "wheelrun: %s: %v\n", it.Symbol, err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("wheelrun: %d of %d wheel bindings failed", failed, wheelBindings)
	}
	return nil
}

// Run runs RunOnce immediately, then on every interval tick until ctx is
// cancelled. Interval must be positive; per-pass errors are logged and never
// abort the loop.
func (r *Runner) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("wheelrun: interval must be positive")
	}
	pass := func() {
		if err := r.RunOnce(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "wheelrun: %v\n", err)
		}
	}
	pass()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pass()
		}
	}
}

// runSymbol executes the full chain for one symbol. Every failure is returned
// for RunOnce to log; nothing panics and no broker order is ever placed.
func (r *Runner) runSymbol(ctx context.Context, symbol string) error {
	rec, err := r.deps.Store.LatestConfig(ctx, symbol)
	if err != nil {
		return fmt.Errorf("latest config: %w", err)
	}
	cfg, err := strategy.ParseConfig(rec.Config)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	price, err := r.deps.Quoter.Quote(ctx, symbol)
	if err != nil {
		return r.persistDataBlocked(ctx, symbol, rec.Version, "current_price", fmt.Errorf("current price: %w", err))
	}
	if price <= 0 {
		return fmt.Errorf("current price %v is not positive", price)
	}
	now := time.Now()
	contracts, err := r.deps.Chain.OptionChain(ctx, symbol, now.AddDate(0, 0, cfg.MinDTE), now.AddDate(0, 0, cfg.MaxDTE))
	if err != nil {
		return fmt.Errorf("option chain: %w", err)
	}
	contractSymbols := make([]string, 0, len(contracts))
	for _, c := range contracts {
		contractSymbols = append(contractSymbols, c.Symbol)
	}
	positions, err := r.deps.Positions.Positions(ctx, nil)
	if err != nil {
		return fmt.Errorf("positions: %w", err)
	}
	filteredPositions, unassignedOptions := filterPositions(symbol, positions, contractSymbols)
	for _, p := range unassignedOptions {
		positionCode := p.Code
		if positionCode == "" {
			positionCode = p.Symbol
		}
		fmt.Fprintf(os.Stderr, "wheelrun: %s: skipping unassigned option position %s (not in option chain)\n", symbol, positionCode)
	}
	stockShares, opts, err := PositionsInput(filteredPositions)
	if err != nil {
		return fmt.Errorf("positions input: %w", err)
	}
	quotes, err := r.deps.Quoter.OptionQuotes(ctx, contractSymbols)
	if err != nil {
		return fmt.Errorf("option quotes: %w", err)
	}
	asOf := time.Now()
	dailyOrders, err := r.dailyOrders(ctx, symbol, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: daily orders: %v (using 0)\n", symbol, err)
	}
	in := wheel.DecisionInput{
		CurrentPrice:     price,
		AsOf:             asOf,
		StockShares:      stockShares,
		Positions:        opts,
		Quotes:           assembleQuotes(symbol, contracts, quotes),
		DailyOrders:      dailyOrders,
		ExtremeDay:       false,
		CashAvailable:    0,
		HasCashAvailable: false,
	}
	sig, err := wheel.Evaluate(cfg, in)
	if err != nil {
		return fmt.Errorf("evaluate: %w", err)
	}
	record, status, reason := mapSignal(symbol, rec.Version, sig, price)
	id, err := r.deps.Store.AppendSignal(ctx, record)
	if err != nil {
		return fmt.Errorf("append signal: %w", err)
	}
	if record.Action == "ALERT" {
		r.reviewAlert(ctx, symbol, id, rec.Version, cfg, sig, record, filteredPositions, price)
	}
	if err := r.deps.Watchlist.SetExecutionStatus(ctx, symbol, status, reason); err != nil {
		return fmt.Errorf("signal %d stored, watchlist status sync: %w", id, err)
	}
	fmt.Fprintf(os.Stderr, "wheelrun: %s: %s capability=%s signal=%d\n", symbol, sig.Action, sig.CapabilityStatus, id)
	return nil
}

func (r *Runner) persistDataBlocked(ctx context.Context, symbol string, version int, blocker string, err error) error {
	reason := err.Error()
	record := wheelstore.SignalRecord{
		Symbol:           symbol,
		Action:           "HOLD",
		ConfigVersion:    version,
		CapabilityStatus: wheel.CapabilityDataBlocked,
		BlockedBy:        []string{blocker},
		Reason:           reason,
	}
	id, appendErr := r.deps.Store.AppendSignal(ctx, record)
	if appendErr != nil {
		return fmt.Errorf("%s; append DATA_BLOCKED signal: %w", reason, appendErr)
	}
	if statusErr := r.deps.Watchlist.SetExecutionStatus(ctx, symbol, watchlist.StatusDataBlocked, blocker); statusErr != nil {
		return fmt.Errorf("%s; signal %d stored, watchlist status sync: %w", reason, id, statusErr)
	}
	return fmt.Errorf("%s; signal %d DATA_BLOCKED", reason, id)
}

func (r *Runner) reviewAlert(ctx context.Context, symbol string, signalID int64, configVersion int, cfg wheel.Config, sig wheel.Signal, record wheelstore.SignalRecord, positions []Position, price float64) {
	if r.deps.LLMReviewer == nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: LLM reviewer unavailable; skipping review signal=%d\n", symbol, signalID)
		return
	}
	summary := map[string]any{
		"symbol":           symbol,
		"config_version":   configVersion,
		"current_price":    price,
		"strategy_config":  cfg,
		"signal":           sig,
		"persisted_signal": record,
		"positions":        positions,
		"cash_available":   nil,
		"rules":            wheelReviewRules,
	}
	_, action, err := llmreview.RecordLLMGate(ctx, r.deps.Store, r.deps.LLMReviewer, strings.TrimSpace(r.deps.LLMModel), llmreview.GateInput{
		SignalID:                   signalID,
		UnexpectedVerdictIsFailure: true,
		Request: llmreview.ReviewRequest{
			StrategyConfig: cfg,
			Signal:         sig,
			Positions:      positions,
			CashAvailable:  nil,
			RulesText:      wheelReviewRules,
			Symbol:         symbol,
		},
		Summary: summary,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: append LLM gate action %s: %v\n", symbol, action, err)
	}
}

// dailyOrders counts today's (UTC) ALERT signals as the day's order usage.
// The count is best-effort: a store failure logs and yields 0.
func (r *Runner) dailyOrders(ctx context.Context, symbol string, now time.Time) (int, error) {
	signals, err := r.deps.Store.ListSignals(ctx, symbol, "", "", 1000)
	if err != nil {
		return 0, err
	}
	start := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	n := 0
	for _, s := range signals {
		if s.Action == "ALERT" && !s.CreatedAt.Before(start) {
			n++
		}
	}
	return n, nil
}

// assembleQuotes merges chain metadata (type/expiry/strike) with live quotes
// into wheel candidates. Contracts the gateway did not answer stay absent;
// zero fields flow through and Evaluate turns them into HOLD.
func assembleQuotes(underlying string, contracts []futu.OptionContract, quotes map[string]futu.OptionQuoteEx) []wheel.OptionQuote {
	out := make([]wheel.OptionQuote, 0, len(contracts))
	for _, c := range contracts {
		q, ok := quotes[c.Symbol]
		if !ok {
			continue
		}
		lot := q.LotSize
		if lot <= 0 {
			lot = c.LotSize
		}
		out = append(out, wheel.OptionQuote{
			Symbol:       q.Symbol,
			Underlying:   underlying,
			Source:       "futu",
			OptionType:   wheel.OptionType(c.OptionType),
			Expiry:       c.Expiry,
			Strike:       c.Strike,
			Delta:        q.Delta,
			Bid:          q.Bid,
			Ask:          q.Ask,
			Last:         q.Last,
			ImpliedVol:   q.ImpliedVol,
			Theta:        q.Theta, // nil = missing -> quote fails validation -> HOLD
			Volume:       q.Volume,
			OpenInterest: q.OpenInterest,
			LotSize:      lot,
			QuoteTime:    q.QuoteTime,
		})
	}
	return out
}

// mapSignal translates a wheel.Signal into the persisted SignalRecord plus
// the watchlist status pair. Evaluate's capability is preserved: a risk HOLD
// (READY capability) stays READY; only a data block degrades to
// DATA_BLOCKED, with a fallback blocker when Evaluate left it unnamed.
func mapSignal(symbol string, version int, sig wheel.Signal, price float64) (wheelstore.SignalRecord, string, string) {
	capStatus := strings.ToUpper(strings.TrimSpace(sig.CapabilityStatus))
	if capStatus == "" {
		capStatus = wheel.CapabilityReady
	}
	blocked := sig.BlockedBy
	if capStatus != wheel.CapabilityReady && len(blocked) == 0 {
		blocked = []string{fallbackBlocker}
	}
	record := wheelstore.SignalRecord{
		Symbol:           symbol,
		Action:           strings.ToUpper(sig.Action),
		ConfigVersion:    version,
		CapabilityStatus: capStatus,
		BlockedBy:        blocked,
		Inventory: wheelstore.InventorySnapshot{
			CurrentPrice:       fptr(price),
			ActualInventory:    fptr(sig.ActualInventory),
			OptionDeltaStock:   fptr(sig.OptionDeltaStock),
			EffectiveInventory: fptr(sig.EffectiveInventory),
			TargetInventory:    fptr(sig.TargetInventory),
			InventoryGap:       fptr(sig.InventoryGap),
		},
		Candidates:       candidateRecords(sig.Candidates),
		RejectionReasons: sig.RejectReasons,
		Reason:           sig.Reason,
	}
	status, reason := watchlist.StatusReady, ""
	if capStatus != wheel.CapabilityReady {
		status = watchlist.StatusDataBlocked
		if len(blocked) > 0 {
			reason = blocked[0]
		}
	}
	return record, status, reason
}

// candidateRecords converts domain candidates directly to the shared signal
// DTO; no JSON map round-trip is needed between strategy and repository.
func candidateRecords(cands []wheel.CandidateEvaluation) []wheelstore.Candidate {
	out := make([]wheelstore.Candidate, 0, len(cands))
	for _, c := range cands {
		quote := quoteRecord(c.Quote)
		out = append(out, wheelstore.AsFullCandidate(wheelstore.Candidate{
			Quote:               &quote,
			Direction:           string(c.Direction),
			Quantity:            c.Quantity,
			SignedContracts:     c.SignedContracts,
			Quality:             c.Quality,
			PostTradeEffective:  c.PostTradeEffective,
			AssignmentInventory: c.AssignmentInventory,
			Accepted:            c.Accepted,
			Reasons:             c.Reasons,
		}))
	}
	return out
}

func quoteRecord(q wheel.OptionQuote) wheelstore.Quote {
	return wheelstore.Quote{
		Symbol:       q.Symbol,
		Code:         q.Code,
		Underlying:   q.Underlying,
		Source:       q.Source,
		OptionType:   string(q.OptionType),
		Type:         string(q.Type),
		Expiry:       q.Expiry.Format(time.RFC3339Nano),
		Strike:       q.Strike,
		Delta:        q.Delta,
		MarketDelta:  q.MarketDelta,
		Bid:          q.Bid,
		Ask:          q.Ask,
		Last:         q.Last,
		ImpliedVol:   q.ImpliedVol,
		Theta:        q.Theta,
		Volume:       q.Volume,
		OpenInterest: q.OpenInterest,
		LotSize:      q.LotSize,
		QuoteTime:    q.QuoteTime.Format(time.RFC3339Nano),
		CapturedAt:   q.CapturedAt.Format(time.RFC3339Nano),
		Timestamp:    q.Timestamp.Format(time.RFC3339Nano),
		Ts:           q.Ts.Format(time.RFC3339Nano),
		IV:           q.IV,
		OI:           q.OI,
	}
}

func fptr(v float64) *float64 { return &v }
