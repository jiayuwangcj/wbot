// Runner drives the live wheel loop: for every watchlist binding it loads the
// latest config, pulls a current price, positions, an option chain window and
// contract quotes, then evaluates a wheel decision and persists the signal
// (watchlist execution status stays in sync). All dependencies are injected
// so tests drive the loop with fakes; the futu REST client satisfies the
// quote/chain interfaces and the proto TradeClient the positions source.

package wheelrun

import (
	"context"
	"encoding/json"
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

// LLMReviewer audits an ALERT before it can pass the notification gate.
type LLMReviewer interface {
	Review(ctx context.Context, req llmreview.ReviewRequest) (llmreview.ReviewResult, error)
}

// SignalStore is the wheelstore subset the runner reads and writes.
type SignalStore interface {
	LatestConfig(ctx context.Context, symbol string) (*wheelstore.ConfigRecord, error)
	AppendSignal(ctx context.Context, r wheelstore.SignalRecord) (int64, error)
	AppendAction(ctx context.Context, r wheelstore.ActionRecord) (int64, error)
	ListSignals(ctx context.Context, symbol, action, capability string, limit int) ([]wheelstore.SignalRecord, error)
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
	Store       SignalStore
	Watchlist   WatchlistLister
	LLMReviewer LLMReviewer
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
const wheelReviewRules = "仅审核 wheel 策略；信号只能是 ALERT 或 HOLD；审核不得触发自动下单；候选必须有完整、及时的期权报价；不得超过最大库存、每日订单数或战略状态限制；数据不足时必须拒绝。"

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
	positions, err := r.deps.Positions.Positions(ctx, nil)
	if err != nil {
		return fmt.Errorf("positions: %w", err)
	}
	stockShares, opts, err := PositionsInput(positions)
	if err != nil {
		return fmt.Errorf("positions input: %w", err)
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
	quotes, err := r.deps.Quoter.OptionQuotes(ctx, contractSymbols)
	if err != nil {
		return fmt.Errorf("option quotes: %w", err)
	}
	dailyOrders, err := r.dailyOrders(ctx, symbol, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: daily orders: %v (using 0)\n", symbol, err)
	}
	in := wheel.DecisionInput{
		CurrentPrice:     price,
		AsOf:             now,
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
		r.reviewAlert(ctx, symbol, id, rec.Version, cfg, sig, record, positions, price)
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
	result, err := r.deps.LLMReviewer.Review(ctx, llmreview.ReviewRequest{
		StrategyConfig: cfg,
		Signal:         sig,
		Positions:      positions,
		CashAvailable:  nil,
		RulesText:      wheelReviewRules,
		Symbol:         symbol,
	})
	if err != nil {
		r.recordReviewFailure(ctx, symbol, signalID, summary, err)
		return
	}
	verdict := strings.ToUpper(strings.TrimSpace(result.Verdict))
	switch verdict {
	case "APPROVE":
		r.appendReviewAction(ctx, symbol, wheelstore.ActionRecord{
			SignalID: signalID,
			Action:   "LLM_REVIEW",
			Actor:    r.llmActor(),
			Details:  reviewDetails(verdict, result.Reasons, result.Notes, summary),
		})
	case "REJECT":
		r.appendReviewAction(ctx, symbol, wheelstore.ActionRecord{
			SignalID: signalID,
			Action:   "REJECTED",
			Actor:    r.llmActor(),
			Details:  reviewDetails(verdict, result.Reasons, result.Notes, summary),
		})
	default:
		r.recordReviewFailure(ctx, symbol, signalID, summary, fmt.Errorf("unexpected LLM verdict %q", result.Verdict))
	}
}

func (r *Runner) recordReviewFailure(ctx context.Context, symbol string, signalID int64, summary map[string]any, err error) {
	reason := err.Error()
	r.appendReviewAction(ctx, symbol, wheelstore.ActionRecord{
		SignalID: signalID,
		Action:   "REJECTED",
		Actor:    r.llmActor(),
		Details: map[string]any{
			"verdict":       "REJECT",
			"reasons":       []string{reason},
			"error":         reason,
			"input_summary": summary,
		},
	})
}

func (r *Runner) appendReviewAction(ctx context.Context, symbol string, action wheelstore.ActionRecord) {
	if _, err := r.deps.Store.AppendAction(ctx, action); err != nil {
		fmt.Fprintf(os.Stderr, "wheelrun: %s: append LLM gate action %s: %v\n", symbol, action.Action, err)
	}
}

func (r *Runner) llmActor() string {
	model := strings.TrimSpace(r.deps.LLMModel)
	if model == "" {
		model = "unknown"
	}
	return "llm:" + model
}

func reviewDetails(verdict string, reasons []string, notes string, summary map[string]any) map[string]any {
	if reasons == nil {
		reasons = []string{}
	}
	details := map[string]any{
		"verdict":       verdict,
		"reasons":       reasons,
		"input_summary": summary,
	}
	if notes != "" {
		details["notes"] = notes
	}
	return details
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
		Candidates:       candidateMaps(sig.Candidates),
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

// candidateMaps renders candidates as the JSON maps the signal table stores
// (marshal cannot fail: all candidate fields are primitives).
func candidateMaps(cands []wheel.CandidateEvaluation) []map[string]any {
	out := make([]map[string]any, 0, len(cands))
	for _, c := range cands {
		b, _ := json.Marshal(c)
		m := map[string]any{}
		_ = json.Unmarshal(b, &m)
		out = append(out, m)
	}
	return out
}

func fptr(v float64) *float64 { return &v }
