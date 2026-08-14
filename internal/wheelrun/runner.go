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
	"sync"
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
//
// LLM audits run off the pass loop: reviewAlert can take minutes (300s HTTP
// timeout) and a synchronous call inside runSymbol stalled every symbol
// while one audit was in flight (2026-08-14: signals 868/870/871). ALERTs
// are handed to a bounded review queue drained by reviewWorkers; the audit
// result lands as an LLM_REVIEW/REJECTED action exactly as before, so the
// push gate's semantics are unchanged.
type Runner struct {
	deps      Dependencies
	snapshots *asyncSnapshotRecorder

	// reviewCh is the bounded queue of pending audits. reviewInflight tracks
	// symbols with a queued or running audit so a symbol that keeps ALERTing
	// (candidate stays in range) does not pile up one audit per pass — the
	// audit the push gate waits on is already in flight.
	reviewCh       chan reviewTask
	reviewInflight map[string]bool
	reviewMu       sync.Mutex
	reviewWG       sync.WaitGroup
	closeOnce      sync.Once

	// lastAlert is the per-symbol suppression baseline for repeat ALERTs of
	// the same contract (see suppressRepeatAlert/commitAlertBaseline). The
	// pass loop writes it on successful ALERTs; review workers clear it when
	// an audit fails on the infrastructure side (clearSuppression), so all
	// access is serialized by reviewMu.
	lastAlert map[string]lastAlertInfo
}

// reviewTask snapshots everything reviewAlert needs so the worker can run
// after the pass has moved on (cfg/sig/record are values; positions is the
// pass's slice, read-only after enqueue).
type reviewTask struct {
	ctx           context.Context
	symbol        string
	signalID      int64
	configVersion int
	cfg           wheel.Config
	sig           wheel.Signal
	record        wheelstore.SignalRecord
	positions     []Position
	price         float64
	cash          float64
	hasCash       bool
}

// lastAlertInfo is the suppression baseline: the contract that last alerted
// for a symbol and when, plus the last close-position alert time (own
// cooldown — see suppressRepeatAlert). A suppressed round does not update it,
// so the window expires and the candidate alerts (and is re-audited) again.
type lastAlertInfo struct {
	at       time.Time
	contract string
	closeAt  time.Time
}

const (
	// reviewWorkers bounds concurrent LLM audits. A serial queue would age
	// later signals out of the push gate's freshness window once several
	// symbols alert in the same pass (each audit takes minutes).
	reviewWorkers = 2
	// reviewQueueDepth caps queued audits; entries beyond it are dropped
	// with a log line (defensive only: per-symbol dedup already bounds the
	// queue to one entry per symbol).
	reviewQueueDepth = 8
	// repeatAlertWindow: a second ALERT for the same symbol and contract
	// inside this window is downgraded to HOLD. JD 2026-08-14: the 28/29P
	// candidate stayed in range, every pass re-alerted and every round
	// spawned a fresh 4-minute audit.
	repeatAlertWindow = 30 * time.Minute
	// closeAlertCooldown gates re-alerts of an unanswered close_position
	// signal (评审 P2,2026-08-15): while the position stays open, every pass
	// would otherwise re-trigger the close ALERT and a fresh LLM audit — an
	// unbounded per-symbol cost and review-queue pressure. The sell-side
	// 30-minute repeat window does not apply (a close is a risk-reducing
	// action), but a dedicated 90-minute cooldown bounds the flood; the
	// human is re-prompted as the captured ratio keeps decaying.
	closeAlertCooldown = 90 * time.Minute
)

func NewRunner(deps Dependencies) *Runner {
	recorder := deps.SnapshotRecorder
	if recorder == nil {
		if candidate, ok := deps.Store.(QuoteSnapshotRecorder); ok {
			recorder = candidate
		}
	}
	r := &Runner{
		deps:           deps,
		snapshots:      newAsyncSnapshotRecorder(recorder, deps.SnapshotQueueSize),
		reviewCh:       make(chan reviewTask, reviewQueueDepth),
		reviewInflight: map[string]bool{},
		lastAlert:      map[string]lastAlertInfo{},
	}
	for i := 0; i < reviewWorkers; i++ {
		r.reviewWG.Add(1)
		go r.reviewWorker()
	}
	return r
}

// Close drains the bounded snapshot side channel and stops the review
// workers after queued audits complete. Run calls it automatically; callers
// that use RunOnce directly can close explicitly when they need to wait for
// queued observations and audits to finish.
func (r *Runner) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		close(r.reviewCh)
		r.reviewWG.Wait()
		r.snapshots.close()
	})
}

func (r *Runner) now() time.Time {
	if r.deps.Now != nil {
		return r.deps.Now()
	}
	return time.Now()
}

// reviewWorker drains the review queue. The audit uses the enqueue-time
// context, so a cancelled run loop fails in-flight audits promptly (the
// failure lands as an audit action, same as the synchronous path). A panic
// inside the audit must not kill the worker: it is recovered, the
// suppression baseline is cleared (the audit never produced a verdict) and
// the next pass re-audits.
func (r *Runner) reviewWorker() {
	defer r.reviewWG.Done()
	for task := range r.reviewCh {
		func() {
			defer func() {
				if p := recover(); p != nil {
					fmt.Fprintf(os.Stderr, "wheelrun: %s: review worker panic: %v\n", task.symbol, p)
					r.clearSuppression(task.symbol)
				}
				r.reviewMu.Lock()
				delete(r.reviewInflight, task.symbol)
				r.reviewMu.Unlock()
			}()
			r.reviewAlert(task.ctx, task.symbol, task.signalID, task.configVersion, task.cfg, task.sig, task.record, task.positions, task.price, task.cash, task.hasCash)
		}()
	}
}

// fallbackBlocker keeps the validateSignal contract (DATA_BLOCKED must have
// at least one blocker) when Evaluate reports a data block without naming it.
const fallbackBlocker = "no complete quote snapshot"

// 与 doc/WHEEL_STRATEGY.md「LLM 审核规则摘要（单一来源）」小节同步，文档为源。
const wheelReviewRules = `仅审核 wheel 区间策略；信号只能是 ALERT 或 HOLD，审核不得触发自动下单。策略在满仓价到清仓价区间内线性计算目标库存，通过卖出现金担保 Put 或备兑 Call 收取权利金：目标库存高于有效库存时只能考虑卖 Put 增加库存敞口，目标库存低于有效库存时只能考虑卖 Call 降低库存敞口。
当前情况由标的现价、策略配置版本、现金可用额及股票/期权持仓组成；signal 描述提示动作、方向、卖出合约数、候选报价、当前/目标/有效/交易后库存、库存缺口、能力状态和阻断原因；预期收益 expected_gain 是按卖价 Bid × 合约乘数 × 数量估算的毛权利金，不含手续费、滑点、税费及指派损益，不代表保证收益，缺失或为零不得推断为有收益。
必须逐项审核：
1. 方向反转检查（硬性项）：核对 signal.direction 与当前持仓、effective_inventory、inventory_gap、target_inventory 及满仓价—清仓价区间一致；缺口为正时卖 Put、缺口为负时卖 Call，卖出/买入符号与目标库存变化必须一致，任何方向反转或符号矛盾都必须 REJECT。
2. 策略参数：核对 full_position_price/zero_position_price、max_inventory、move_interval_pct、min_premium_per_share、min_option_profit、stock_switch_pct、covered_call_pct、trade_gap、min_option_quality、min_dte/max_dte、strategic_state 及候选合约参数；卖 Put 必须 OTM，卖 Call 必须 OTM 且行权价不低于现价×(1+covered_call_pct)与正股成本。
3. 数据质量：报价必须完整且新鲜，Bid/Ask 正数且未倒挂，IV/Delta/Theta 合理，Volume/OI 非零；不得用缺失 Greeks 或过期、拼接数据作判断。
4. 资金与库存：核对现金/保证金预算、最大库存、Put 指派承诺、Call 备兑数量和交易后有效库存；策略不设每日提醒次数上限。
5. 系统性错误：排查闭市或停牌误判、同一合约重复动作、与现有持仓或历史动作矛盾、合约类型/到期日/乘数错误及 Greeks 缺失。
6. 数据不足：capability_status 为 DATA_BLOCKED、blocked_by 非空，或任一关键字段不足时必须 REJECT；不得以 expected_gain 补偿或放宽任何校验。
7. 改单（signal.replace 非空，硬性项）：改单=撤销 pending_orders 中旧挂单（replace.order_id/replace.contract）改挂首选候选，是写操作、同样需要审核。必须核对：a) 新合约不要求严格优于旧合约：允许价格稍差的调整（如权利金略低、质量相当），若理由合理——更快成交、流动性更好、更接近目标库存——应予批准；但新合约明显劣化（质量/流动性显著更差、风险显著增大）或调整无任何依据时必须 REJECT；b) 旧挂单确在 pending_orders 中且方向一致；c) 改单后库存偏差不增大；d) 频繁改单（同标的短时多次）必须 REJECT——避免反复撤换浪费与不确定性。
8. 平仓（signal.close_position 为真，硬性项）：平仓=买回已持空腿以兑现已收权利金（profit_take_pct 达到阈值），属风险降低动作，方向与库存缺口相反是其固有特征，不受第 1 条方向反转规则约束。必须核对：a) 合约确为当前持仓空腿（positions 中该合约数量为负），且平仓数量 ≤ 持仓数量；b) 报价合理（Bid/Ask 正数且未倒挂、非陈旧），平仓价显著高于持仓成本或明显不合理时必须 REJECT；c) 买回成本 ≤ 可用现金/保证金（不足时必须 REJECT）；d) 平仓后不遗留反向裸仓（不得把平仓当成反手开新仓）。`

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
	// Close before the interval check: Run(ctx, 0) must not leak the review
	// workers it already spawned in NewRunner.
	defer r.Close()
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
	begin := now.AddDate(0, 0, cfg.MinDTE)
	end := now.AddDate(0, 0, cfg.MaxDTE)
	// futu gateway rejects option-chain windows wider than 30 days + 1h;
	// clamp the far end so a trained min_dte..max_dte span (e.g. 13..45)
	// cannot DATA_BLOCK the live loop (2026-08-14 JD 40d/00700 32d 实测).
	if span := end.Sub(begin); span > 30*24*time.Hour {
		clamped := begin.Add(30 * 24 * time.Hour)
		fmt.Fprintf(os.Stderr, "wheelrun: %s: option chain window %s..%s (%dd) exceeds futu 30d limit; clamped to %s\n",
			symbol, begin.Format("2006-01-02"), end.Format("2006-01-02"), int(span.Hours()/24), clamped.Format("2006-01-02"))
		end = clamped
	}
	contracts, err := r.deps.Chain.OptionChain(ctx, symbol, begin, end)
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
	// Legs outside the chain window (final min_dte days before expiry) cannot
	// enter the inventory — but they must stay closable: profit_take_pct buys
	// them back at theta's steepest decay. They ride in ClosePositions (exit
	// evaluation only) and in the review input (审核核对持仓空腿). The chain's
	// underlying letters (00700→TCH) gate which unassigned legs belong to this
	// symbol: cross-underlying legs are skipped entirely (评审 P1-A), which
	// also spares their per-pass quote pulls (限频).
	closeLegs, closeReviewPositions, closeExpiries := closePositionLegs(unassignedOptions, chainUnderlyingLetters(contractSymbols))
	reviewPositions := append([]Position{}, filteredPositions...)
	reviewPositions = append(reviewPositions, closeReviewPositions...)
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
	stockAverageCost := StockAverageCost(filteredPositions)
	quoteContracts, quotes, err := r.collectOptionQuotes(ctx, symbol, contracts, price, cfg, stockShares, stockAverageCost, opts, now)
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
	for _, p := range append(append([]wheel.OptionPosition{}, opts...), closeLegs...) {
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
	for i := range closeLegs {
		if q, ok := quotes[closeLegs[i].Symbol]; ok {
			closeLegs[i].Delta = q.Delta
			if closeLegs[i].LotSize <= 0 && q.LotSize > 0 {
				closeLegs[i].LotSize = q.LotSize
			}
		}
	}
	// Chain-external legs are priced for the exit decision too: assembleQuotes
	// only walks chain contracts, so the close legs' quotes ride in their own
	// OptionQuote set (expiry parsed from the code) appended to in.Quotes —
	// otherwise takeProfitSignal's positionQuote can never find them and the
	// live close never fires (评审 P1-A).
	closeQuotes := make([]wheel.OptionQuote, 0, len(closeLegs))
	for _, leg := range closeLegs {
		q, ok := quotes[leg.Symbol]
		if !ok {
			continue
		}
		closeQuotes = append(closeQuotes, wheel.OptionQuote{
			Symbol:       leg.Symbol,
			Underlying:   symbol,
			Source:       "futu",
			OptionType:   leg.OptionType,
			Expiry:       closeExpiries[leg.Symbol],
			Strike:       leg.Strike,
			Delta:        q.Delta,
			Bid:          q.Bid,
			Ask:          q.Ask,
			Last:         q.Last,
			ImpliedVol:   q.ImpliedVol,
			Theta:        q.Theta,
			Volume:       q.Volume,
			OpenInterest: q.OpenInterest,
			LotSize:      leg.LotSize,
			QuoteTime:    q.QuoteTime,
		})
	}
	asOf := r.now()
	r.enqueueQuoteSnapshots(symbol, price, quoteContracts, quotes, asOf)
	// Unfilled orders must gate the candidate selection, not just the review:
	// without this, an unfilled order keeps re-alerting the same contract and
	// the LLM gate rejects the duplicate every cycle (2026-08-13: P29000 over
	// pending order 206158430256 rejected on signals 747/749/750/751).
	pending, perr := r.deps.Store.ListPendingOrders(ctx, symbol)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: list pending orders: %v\n", symbol, perr)
	}
	in := wheel.DecisionInput{
		CurrentPrice:     price,
		AsOf:             asOf,
		StockShares:      stockShares,
		StockAverageCost: stockAverageCost,
		Positions:        opts,
		Quotes:           append(assembleQuotes(symbol, quoteContracts, quotes), closeQuotes...),
		CashAvailable:    0,
		HasCashAvailable: false,
		Pending:          mapPending(pending),
		// IVRank stays 0 live: no one-year IV history source exists for the
		// running process, so min_iv_rank > 0 makes live evaluation HOLD
		// (fail-closed) until a rank data source lands.
		IVRank: 0,
		// 链外持仓仅用于平仓评估,不进库存口径(与回测对称:到期前最后
		// min_dte 天仍可平仓)。
		ClosePositions: closeLegs,
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
	if record.Action == "ALERT" && r.suppressRepeatAlert(symbol, sig, now) {
		// 同候选在窗口内重复:降为 HOLD,不落 ALERT、不触发审核。窗口不滚动,
		// 期满后候选重新 ALERT 并重新审核。原策略原因保留在文案中,不覆盖。
		record.Action = "HOLD"
		record.Reason = fmt.Sprintf("重复候选抑制: %s 在 %v 窗口内已 ALERT 过, 降为 HOLD; %s", sig.Quote.Symbol, repeatAlertWindow, sig.Reason)
	}
	id, err := r.deps.Store.AppendSignal(ctx, record)
	if err != nil {
		return fmt.Errorf("append signal: %w", err)
	}
	if record.Action == "ALERT" {
		// 基线在信号落库成功后才写:落库失败的 ALERT 从未发生,不应进入
		// 抑制窗口(否则下一 pass 会被静默降 HOLD)。
		r.commitAlertBaseline(symbol, sig, now)
		r.enqueueReview(ctx, symbol, id, rec.Version, cfg, sig, record, reviewPositions, price, in.CashAvailable, in.HasCashAvailable)
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

// enqueueReview hands an ALERT to the review queue without blocking the run
// loop. Per-symbol dedup: a symbol whose audit is already queued or running
// skips further reviews (the audit the push gate waits on is in flight, a
// second one would only delay the queue). A full queue drops the entry with
// a log line — the push gate then skips this signal and the next windowed
// ALERT audits again.
func (r *Runner) enqueueReview(ctx context.Context, symbol string, signalID int64, configVersion int, cfg wheel.Config, sig wheel.Signal, record wheelstore.SignalRecord, positions []Position, price float64, cash float64, hasCash bool) {
	if r.deps.LLMReviewer == nil {
		return
	}
	r.reviewMu.Lock()
	if r.reviewInflight[symbol] {
		r.reviewMu.Unlock()
		fmt.Fprintf(os.Stderr, "wheelrun: %s: LLM review already in flight; skipping review signal=%d\n", symbol, signalID)
		return
	}
	r.reviewInflight[symbol] = true
	r.reviewMu.Unlock()
	task := reviewTask{
		ctx:           ctx,
		symbol:        symbol,
		signalID:      signalID,
		configVersion: configVersion,
		cfg:           cfg,
		sig:           sig,
		record:        record,
		positions:     positions,
		price:         price,
		cash:          cash,
		hasCash:       hasCash,
	}
	select {
	case r.reviewCh <- task:
	default:
		r.reviewMu.Lock()
		delete(r.reviewInflight, symbol)
		r.reviewMu.Unlock()
		// 队列满丢弃 = 审核未发生:清抑制基线,下一 pass 的重复 ALERT 重新
		// 入队重试(否则该 symbol 在窗口内静默降 HOLD,通知最长丢 30 分钟)。
		r.clearSuppression(symbol)
		fmt.Fprintf(os.Stderr, "wheelrun: %s: review queue full; dropping review signal=%d\n", symbol, signalID)
	}
}

// suppressRepeatAlert downgrades an ALERT when the same contract already
// alerted for this symbol within repeatAlertWindow: the candidate stays in
// range pass after pass and every round would otherwise re-alert and spawn a
// fresh audit (JD 28/29P, 2026-08-14). It is a read-only check — the
// baseline is committed by commitAlertBaseline only after the ALERT lands in
// the store; suppressed rounds do not extend it, so the candidate alerts
// again once the window expires. sig.Quote must be non-nil for ALERTs.
func (r *Runner) suppressRepeatAlert(symbol string, sig wheel.Signal, now time.Time) bool {
	r.reviewMu.Lock()
	last, ok := r.lastAlert[symbol]
	r.reviewMu.Unlock()
	// 平仓是风险降低动作:不受卖向 30 分钟重复抑制窗约束(与卖向候选的
	// 「同一合约重复 ALERT」性质不同),但受独立冷却窗约束(评审 P2):
	// 持仓未平期间每 pass 都重发会烧 LLM 审核、挤压卖向审核队列。
	if sig.ClosePosition {
		if ok && now.Sub(last.closeAt) < closeAlertCooldown {
			fmt.Fprintf(os.Stderr, "wheelrun: %s: close_position re-alert within %v cooldown; HOLD instead of ALERT\n", symbol, closeAlertCooldown)
			return true
		}
		return false
	}
	if sig.Quote == nil {
		return false
	}
	contract := sig.Quote.Symbol
	if ok && last.contract == contract && now.Sub(last.at) < repeatAlertWindow {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: repeat candidate %s within %v; HOLD instead of ALERT\n", symbol, contract, repeatAlertWindow)
		return true
	}
	return false
}

// commitAlertBaseline records the contract+time baseline for a persisted
// ALERT. Called by the pass loop after AppendSignal succeeds; workers clear
// it (clearSuppression) when an audit fails on the infrastructure side. A
// close_position ALERT additionally arms its own cooldown (closeAt) so the
// next pass does not immediately re-alert the same closing leg.
func (r *Runner) commitAlertBaseline(symbol string, sig wheel.Signal, now time.Time) {
	if sig.Quote == nil {
		return
	}
	r.reviewMu.Lock()
	last, ok := r.lastAlert[symbol]
	if !ok {
		last = lastAlertInfo{}
	}
	last.at, last.contract = now, sig.Quote.Symbol
	if sig.ClosePosition {
		last.closeAt = now
	}
	r.lastAlert[symbol] = last
	r.reviewMu.Unlock()
}

// clearSuppression removes the repeat-alert baseline for a symbol. Used by
// the review path when an audit never produced a verdict (gate failure after
// retry, dropped queue entry, worker panic): the next pass must re-ALERT and
// re-audit instead of being silently HOLD for the rest of the window.
func (r *Runner) clearSuppression(symbol string) {
	r.reviewMu.Lock()
	delete(r.lastAlert, symbol)
	r.reviewMu.Unlock()
}

// reviewAlert runs the LLM audit for one signal (on a review worker; the
// enqueue-time context is the deadline). The gate is fail-closed: pending
// orders and cash availability are mandatory audit inputs, a transient gate
// failure is retried once (2026-08-13: signal 741 a DNS timeout was hard
// recorded as REJECTED), and any remaining failure lands as an audit action
// the push gate skips.
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
	gate := func() (string, string, error) {
		return llmreview.RecordLLMGate(ctx, r.deps.Store, r.deps.LLMReviewer, strings.TrimSpace(r.deps.LLMModel), llmreview.GateInput{
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
	}
	_, action, err := gate()
	if action == "LLM_REVIEW_FAILED" {
		// 审核请求失败(DNS/网络/超时)多为瞬态:同步重试一次,避免信号被
		// 落成 failed 后无人再审(2026-08-13: signal 741 一次 DNS 超时被
		// 硬记 REJECTED,用户看到「模型拒绝」实际是网络错误)。RecordLLMGate
		// 落库后返回 nil err,失败语义经 disposition=LLM_REVIEW_FAILED 传递。
		// 重试仍失败才保留 LLM_REVIEW_FAILED(推送器跳过,审计可查)。
		fmt.Fprintf(os.Stderr, "wheelrun: %s: LLM gate transient failure signal=%d, retrying once: %v\n", symbol, signalID, err)
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return
		}
		_, action, err = gate()
	}
	if action == "LLM_REVIEW_FAILED" {
		// 审核基础设施失败(重试后仍失败):本次审核从未产生 verdict,清除
		// 抑制基线,下一 pass 重新 ALERT 并重新审核(P1-1)。
		r.clearSuppression(symbol)
	}
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
	if sig.ClosePosition && sig.Quote != nil {
		// 平仓独立载荷:卖向候选管道(Candidates/firstCandidate)永不复用——
		// 推送与确认侧走独立 buy 路径,绝不把已空腿当卖向候选(评审 P1-B)。
		record.ClosePosition = true
		record.CloseQty = sig.Quantity
		closeQuote := quoteRecord(*sig.Quote)
		record.CloseQuote = &closeQuote
	}
	if sig.Replace != nil {
		record.Replace = &wheelstore.ReplaceRecord{OrderID: sig.Replace.OrderID, Contract: sig.Replace.Contract}
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

// mapPending converts store pending-order rows to the strategy's
// duplicate-detection / 改单 footprint (contract + direction + order id).
func mapPending(rows []wheelstore.PendingOrder) []wheel.PendingOrder {
	if len(rows) == 0 {
		return nil
	}
	out := make([]wheel.PendingOrder, 0, len(rows))
	for _, r := range rows {
		out = append(out, wheel.PendingOrder{Contract: r.Contract, Direction: r.Direction, OrderID: r.OrderID})
	}
	return out
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
			ExpectedGain:        c.ExpectedGain,
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
