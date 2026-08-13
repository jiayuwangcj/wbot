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

	"github.com/jiayu/wbot/internal/datacheck"
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

// FundsFunc returns the account's available cash. A nil FundsFunc leaves
// HasCashAvailable false, so every put candidate is rejected on
// cash-availability (fail-closed; 2026-08-13 the runner never wired funds).
type FundsFunc func(ctx context.Context) (float64, error)

// Dependencies is the runner's full injectable surface. Positions is the
// slice-B TradePositions interface (positions.go).
type Dependencies struct {
	Quoter            Quoter
	Positions         TradePositions
	Funds             FundsFunc
	Chain             OptionChainer
	Store             wheelstore.SignalRepository
	Watchlist         WatchlistLister
	LLMReviewer       llmreview.Reviewer
	LLMModel          string
	Calendar          datacheck.Calendar
	Now               func() time.Time
	MarketOpen        MarketOpenFunc
	SnapshotRecorder  QuoteSnapshotRecorder
	SnapshotQueueSize int
}

// Runner evaluates every wheel binding on one sequential pass. It is not
// safe for concurrent use; a single goroutine owns it (Run).
type Runner struct {
	deps      Dependencies
	snapshots *asyncSnapshotRecorder
}

func NewRunner(deps Dependencies) *Runner {
	recorder := deps.SnapshotRecorder
	if recorder == nil {
		if candidate, ok := deps.Store.(QuoteSnapshotRecorder); ok {
			recorder = candidate
		}
	}
	return &Runner{
		deps:      deps,
		snapshots: newAsyncSnapshotRecorder(recorder, deps.SnapshotQueueSize),
	}
}

// Close drains the bounded snapshot side channel. Run calls it automatically;
// callers that use RunOnce directly can close explicitly when they need to
// wait for queued observations to finish.
func (r *Runner) Close() {
	if r != nil {
		r.snapshots.close()
	}
}

func (r *Runner) now() time.Time {
	if r.deps.Now != nil {
		return r.deps.Now()
	}
	return time.Now()
}

// fallbackBlocker keeps the validateSignal contract (DATA_BLOCKED must have
// at least one blocker) when Evaluate reports a data block without naming it.
const fallbackBlocker = "no complete quote snapshot"

// 与 doc/WHEEL_STRATEGY.md「LLM 审核规则摘要（单一来源）」小节同步，文档为源。
const wheelReviewRules = `仅审核 wheel 区间策略；信号只能是 ALERT 或 HOLD，审核不得触发自动下单。策略在满仓价到清仓价区间内线性计算目标库存，通过卖出现金担保 Put 或备兑 Call 收取权利金：目标库存高于有效库存时只能考虑卖 Put 增加库存敞口，目标库存低于有效库存时只能考虑卖 Call 降低库存敞口。
当前情况由标的现价、策略配置版本、现金可用额及股票/期权持仓组成；signal 描述提示动作、方向、卖出合约数、候选报价、当前/目标/有效/交易后库存、库存缺口、能力状态和阻断原因；预期收益 expected_gain 是按卖价 Bid × 合约乘数 × 数量估算的毛权利金，不含手续费、滑点、税费及指派损益，不代表保证收益，缺失或为零不得推断为有收益。
必须逐项审核：
1. 方向反转检查（硬性项）：核对 signal.direction 与当前持仓、effective_inventory、inventory_gap、target_inventory 及满仓价—清仓价区间一致；缺口为正时卖 Put、缺口为负时卖 Call，卖出/买入符号与目标库存变化必须一致，任何方向反转或符号矛盾都必须 REJECT。
2. 策略参数：核对 full_position_price/zero_position_price、max_inventory、move_interval_pct、min_premium_per_share、stock_switch_pct、trade_gap、min_option_quality、min_dte/max_dte、strategic_state 及候选合约参数。
3. 数据质量：报价必须完整且新鲜，Bid/Ask 正数且未倒挂，IV/Delta/Theta 合理，Volume/OI 非零；不得用缺失 Greeks 或过期、拼接数据作判断。
4. 资金与库存：核对现金/保证金预算、最大库存、Put 指派承诺、Call 备兑数量和交易后有效库存；策略不设每日提醒次数上限。
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
	now := r.now()
	for _, it := range items {
		if it.Strategy != "wheel" {
			continue
		}
		wheelBindings++
		if !r.marketOpen(it.Symbol, now) {
			fmt.Fprintf(os.Stderr, "wheelrun: %s: market closed; skipping live evaluation\n", it.Symbol)
			continue
		}
		if err := r.runSymbol(ctx, it.Symbol, now); err != nil {
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
	defer r.Close()
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
func (r *Runner) marketOpen(symbol string, now time.Time) bool {
	if r.deps.MarketOpen != nil {
		return r.deps.MarketOpen(symbol, now)
	}
	return MarketIsOpen(symbol, now, r.deps.Calendar)
}

func (r *Runner) runSymbol(ctx context.Context, symbol string, now time.Time) error {
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
	quoteContracts, quotes, err := r.collectOptionQuotes(ctx, symbol, contracts, price, cfg, stockShares, opts, now)
	if err != nil {
		return fmt.Errorf("option quotes: %w", err)
	}
	// Held options need live delta/lot from the quote map for the effective
	// inventory; PositionsInput leaves Delta/LotSize zero (its contract:
	// "the runner fills quotes ... from OptionQuotes" — implemented here,
	// 2026-08-13: signal 500's sold 450P contributed zero delta stock, so
	// the inventory gap never closed after the fill). The directional
	// fetch may have skipped a held contract of the opposite direction,
	// so pull those individually.
	for _, p := range opts {
		if _, ok := quotes[p.Symbol]; ok {
			continue
		}
		if page, err := r.deps.Quoter.OptionQuotes(ctx, []string{p.Symbol}); err == nil {
			for k, q := range page {
				if q.Symbol == "" {
					q.Symbol = k
				}
				quotes[k] = q
				quotes[q.Symbol] = q
			}
		}
	}
	for i := range opts {
		if q, ok := quotes[opts[i].Symbol]; ok {
			opts[i].Delta = q.Delta
			if opts[i].LotSize <= 0 && q.LotSize > 0 {
				opts[i].LotSize = q.LotSize
			}
		}
	}
	asOf := r.now()
	r.enqueueQuoteSnapshots(symbol, price, quoteContracts, quotes, asOf)
	in := wheel.DecisionInput{
		CurrentPrice:     price,
		AsOf:             asOf,
		StockShares:      stockShares,
		Positions:        opts,
		Quotes:           assembleQuotes(symbol, quoteContracts, quotes),
		CashAvailable:    0,
		HasCashAvailable: false,
	}
	if r.deps.Funds != nil {
		if cash, err := r.deps.Funds(ctx); err == nil {
			in.CashAvailable = cash
			in.HasCashAvailable = true
		} else {
			fmt.Fprintf(os.Stderr, "wheelrun: %s: funds: %v\n", symbol, err)
		}
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
		r.reviewAlert(ctx, symbol, id, rec.Version, cfg, sig, record, filteredPositions, price, in.CashAvailable, in.HasCashAvailable)
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
		Strategy:         "wheel",
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

func (r *Runner) reviewAlert(ctx context.Context, symbol string, signalID int64, configVersion int, cfg wheel.Config, sig wheel.Signal, record wheelstore.SignalRecord, positions []Position, price float64, cash float64, hasCash bool) {
	if r.deps.LLMReviewer == nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: LLM reviewer unavailable; skipping review signal=%d\n", symbol, signalID)
		return
	}
	var cashPtr *float64
	if hasCash {
		cashPtr = &cash
	}
	// 挂单声明是审核的强制输入(老板指令 2026-08-13):LLM 规则要求
	// pending_orders 缺失必须 REJECT。查询后显式归一为空切片——「查过且
	// 无挂单」与「没查(nil)」必须区分(2026-08-13:signal 735 因 null REJECT)。
	pending, perr := r.deps.Store.ListPendingOrders(ctx, symbol)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: list pending orders: %v\n", symbol, perr)
	}
	if pending == nil {
		pending = []wheelstore.PendingOrder{}
	}
	summary := map[string]any{
		"symbol":           symbol,
		"config_version":   configVersion,
		"current_price":    price,
		"strategy_config":  cfg,
		"signal":           sig,
		"persisted_signal": record,
		"positions":        positions,
		"cash_available":   cashPtr,
		"pending_orders":   pending,
		"rules":            wheelReviewRules,
	}
	_, action, err := llmreview.RecordLLMGate(ctx, r.deps.Store, r.deps.LLMReviewer, strings.TrimSpace(r.deps.LLMModel), llmreview.GateInput{
		SignalID:                   signalID,
		UnexpectedVerdictIsFailure: true,
		Request: llmreview.ReviewRequest{
			StrategyConfig: cfg,
			Signal:         sig,
			Positions:      positions,
			CashAvailable:  cashPtr,
			CurrentPrice:   price,
			RulesText:      wheelReviewRules,
			Symbol:         symbol,
			PendingOrders:  pending,
			// 审核模型需要当前日期验证 DTE/报价时效(signal 736:
			// "current_date 为空,无法验证 max_quote_age_seconds=3600")。
			AsOf: r.now().UTC().Format(time.RFC3339),
		},
		Summary: summary,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: append LLM gate action %s: %v\n", symbol, action, err)
	}
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
		symbol := q.Symbol
		if symbol == "" {
			symbol = c.Symbol
		}
		lot := q.LotSize
		if lot <= 0 {
			lot = c.LotSize
		}
		out = append(out, wheel.OptionQuote{
			Symbol:       symbol,
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
		Strategy:         "wheel",
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
