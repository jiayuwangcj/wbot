package wheelrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/db"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/watchlist"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// wheelParams is the minimal valid wheel config envelope (like watchlist.Upsert
// writes); min_option_quality 0 keeps the ALERT path independent of the
// quality score's liquidity components.
func wheelParams() map[string]any {
	return map[string]any{
		"strategy": "wheel",
		"params": map[string]any{
			"full_position_price": 400.0,
			"zero_position_price": 550.0,
			"max_inventory":       1200.0,
			"min_option_quality":  0.0,
		},
	}
}

func configRecord(symbol string) *wheelstore.ConfigRecord {
	return &wheelstore.ConfigRecord{Symbol: symbol, Version: 1, Config: wheelParams()}
}

func fullCallQuote(symbol string, strike float64, expiry, now time.Time) futu.OptionQuoteEx {
	return futu.OptionQuoteEx{
		Symbol: symbol, Bid: 4, Ask: 4.1, Last: 4.05, Volume: 1000, OpenInterest: 10000,
		ImpliedVol: 0.3, Delta: 0.3, Theta: floatPtr(-0.1), QuoteTime: now.Add(-time.Hour), LotSize: 100,
	}
}

func floatPtr(v float64) *float64 { return &v }

func callContract(symbol, underlying string, strike float64, expiry time.Time) futu.OptionContract {
	return futu.OptionContract{Symbol: symbol, Underlying: underlying, OptionType: "call", Strike: strike, Expiry: expiry, LotSize: 100}
}

type fakeQuoter struct {
	prices               map[string]float64
	opts                 map[string]futu.OptionQuoteEx // keyed by contract symbol
	quoteCalls           []string
	optionCalls          [][]string
	optionDelay          time.Duration
	stampOptionQuoteTime bool
	perr                 error
	oerr                 error
}

func (f *fakeQuoter) Quote(ctx context.Context, symbol string) (float64, error) {
	f.quoteCalls = append(f.quoteCalls, symbol)
	if f.perr != nil {
		return 0, f.perr
	}
	p, ok := f.prices[symbol]
	if !ok {
		return 0, fmt.Errorf("fake quoter: no price for %s", symbol)
	}
	return p, nil
}

func (f *fakeQuoter) OptionQuotes(ctx context.Context, symbols []string) (map[string]futu.OptionQuoteEx, error) {
	f.optionCalls = append(f.optionCalls, append([]string(nil), symbols...))
	if f.oerr != nil {
		return nil, f.oerr
	}
	if f.optionDelay > 0 {
		timer := time.NewTimer(f.optionDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	out := map[string]futu.OptionQuoteEx{}
	for _, s := range symbols {
		if q, ok := f.opts[s]; ok {
			if f.stampOptionQuoteTime {
				q.QuoteTime = time.Now()
			}
			out[s] = q
		}
	}
	return out, nil
}

type fakeChain struct {
	contracts []futu.OptionContract
	err       error
}

func (f fakeChain) OptionChain(ctx context.Context, symbol string, begin, end time.Time) ([]futu.OptionContract, error) {
	return f.contracts, f.err
}

type fakeStore struct {
	mu              sync.Mutex
	configs         map[string]*wheelstore.ConfigRecord
	signals         []wheelstore.SignalRecord
	actions         []wheelstore.ActionRecord
	appendErr       error
	appendActionErr error
	dismissed       map[string]bool
}

func (f *fakeStore) LatestConfig(ctx context.Context, symbol string) (*wheelstore.ConfigRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.configs[symbol]
	if !ok {
		return nil, wheelstore.ErrNotFound
	}
	return rec, nil
}

func (f *fakeStore) AppendSignal(ctx context.Context, r wheelstore.SignalRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return 0, f.appendErr
	}
	f.signals = append(f.signals, r)
	return int64(len(f.signals)), nil
}

func (f *fakeStore) AppendAction(ctx context.Context, r wheelstore.ActionRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendActionErr != nil {
		return 0, f.appendActionErr
	}
	f.actions = append(f.actions, r)
	return int64(len(f.actions)), nil
}

func (f *fakeStore) ListSignals(ctx context.Context, symbol, action, capability string, limit int) ([]wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]wheelstore.SignalRecord, 0, len(f.signals))
	for _, signal := range f.signals {
		if symbol != "" && signal.Symbol != symbol {
			continue
		}
		if action != "" && signal.Action != action {
			continue
		}
		if capability != "" && signal.CapabilityStatus != capability {
			continue
		}
		out = append(out, signal)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) GetSignal(_ context.Context, id int64) (*wheelstore.SignalRecord, error) {
	for i := range f.signals {
		storedID := f.signals[i].ID
		if storedID == 0 {
			storedID = int64(i + 1)
		}
		if storedID == id {
			r := f.signals[i]
			r.ID = storedID
			return &r, nil
		}
	}
	return nil, wheelstore.ErrNotFound
}

func (f *fakeStore) LatestLLMReview(ctx context.Context, signalID int64) (*wheelstore.ActionRecord, error) {
	return f.LatestAction(ctx, signalID, "LLM_REVIEW")
}

func (f *fakeStore) LatestAction(_ context.Context, signalID int64, action string) (*wheelstore.ActionRecord, error) {
	for i := len(f.actions) - 1; i >= 0; i-- {
		if f.actions[i].SignalID == signalID && f.actions[i].Action == action {
			r := f.actions[i]
			return &r, nil
		}
	}
	return nil, wheelstore.ErrNotFound
}

func (f *fakeStore) HasAction(_ context.Context, signalID int64, action string) (bool, error) {
	for _, a := range f.actions {
		if a.SignalID == signalID && a.Action == action {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) ListPendingOrders(context.Context, string) ([]wheelstore.PendingOrder, error) {
	return []wheelstore.PendingOrder{}, nil
}

func (f *fakeStore) QuerySignalsSince(_ context.Context, action string, afterID int64, limit int) ([]wheelstore.SignalRecord, error) {
	var out []wheelstore.SignalRecord
	for i, signal := range f.signals {
		id := signal.ID
		if id == 0 {
			id = int64(i + 1)
		}
		if id <= afterID || (action != "" && signal.Action != action) {
			continue
		}
		signal.ID = id
		out = append(out, signal)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) MaxSignalID(context.Context) (int64, error) {
	return int64(len(f.signals)), nil
}

func (f *fakeStore) Dismiss(_ context.Context, symbol string, date time.Time) error {
	if f.dismissed == nil {
		f.dismissed = map[string]bool{}
	}
	f.dismissed[symbol+"|"+date.UTC().Format("2006-01-02")] = true
	return nil
}

func (f *fakeStore) IsDismissed(_ context.Context, symbol string, date time.Time) (bool, error) {
	return f.dismissed[symbol+"|"+date.UTC().Format("2006-01-02")], nil
}

// signalCount/actionCount return the store sizes under lock (the review
// workers append actions concurrently with the test's waitFor polling).
func (f *fakeStore) signalCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.signals)
}

func (f *fakeStore) actionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.actions)
}

type fakeReviewer struct {
	mu       sync.Mutex
	results  map[string]llmreview.ReviewResult
	errors   map[string]error
	requests []llmreview.ReviewRequest
}

func (f *fakeReviewer) Review(ctx context.Context, req llmreview.ReviewRequest) (llmreview.ReviewResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if err, ok := f.errors[req.Symbol]; ok {
		return llmreview.ReviewResult{}, err
	}
	return f.results[req.Symbol], nil
}

func (f *fakeReviewer) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

type statusCall struct {
	symbol, status, reason string
}

type fakeWatchlist struct {
	items  []watchlist.Item
	status []statusCall
}

func (f *fakeWatchlist) List(ctx context.Context) ([]watchlist.Item, error) {
	return f.items, nil
}

func (f *fakeWatchlist) SetExecutionStatus(ctx context.Context, symbol, status, reason string) error {
	f.status = append(f.status, statusCall{symbol, status, reason})
	return nil
}

func wheelItem(symbol string) watchlist.Item {
	return watchlist.Item{Symbol: symbol, Strategy: "wheel"}
}

func testRunner(t *testing.T, deps Dependencies) *Runner {
	t.Helper()
	// Existing runner tests exercise decision/persistence contracts with fixed
	// fakes; market-hours behavior has dedicated tests below and is injected
	// explicitly there.
	if deps.MarketOpen == nil {
		deps.MarketOpen = func(string, time.Time) bool { return true }
	}
	if deps.Quoter == nil {
		deps.Quoter = &fakeQuoter{}
	}
	if deps.Positions == nil {
		deps.Positions = fakePositions{}
	}
	if deps.Chain == nil {
		deps.Chain = fakeChain{}
	}
	if deps.Store == nil {
		deps.Store = &fakeStore{}
	}
	if deps.Watchlist == nil {
		deps.Watchlist = &fakeWatchlist{}
	}
	return NewRunner(deps)
}

// waitFor polls cond for up to 5s. LLM reviews run on the runner's worker
// goroutines (as of the async-review fix), so assertions on review requests
// and their persisted actions must poll instead of reading synchronously.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	// 8s covers the gate retry path (3s sleep + audit) in
	// TestReviewFailureClearsSuppression, which waits across both attempts.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor: %s", msg)
}

// TestRunOnceAlertReady: a full quote snapshot with a short-inventory gap
// yields an ALERT signal (READY capability) and syncs READY with no reason.
func TestCandidateRecordsPreserveWheelJSON(t *testing.T) {
	theta := -0.01
	quoteTime := time.Date(2026, 8, 12, 1, 2, 3, 456000000, time.UTC)
	candidate := wheel.CandidateEvaluation{
		Quote: wheel.OptionQuote{
			Symbol: "HK.TCH260821P460000", Underlying: "HK.00700", Source: "futu",
			OptionType: wheel.Put, Expiry: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
			Strike: 460, Delta: -0.47, MarketDelta: -0.46, Bid: 11.45, Ask: 11.5,
			Last: 11.48, ImpliedVol: 0.404, Theta: &theta, Volume: 100,
			OpenInterest: 249, LotSize: 100, QuoteTime: quoteTime,
			CapturedAt: quoteTime, Timestamp: quoteTime, Ts: quoteTime, IV: 0.404, OI: 249,
		},
		Direction: wheel.DirectionPut, Quantity: 1, SignedContracts: -1,
		Quality: 0.8, PostTradeEffective: 450, AssignmentInventory: 600,
		Accepted: true, Reasons: []string{"ok"},
	}

	oldJSON, err := json.Marshal(candidate)
	if err != nil {
		t.Fatalf("marshal domain candidate: %v", err)
	}
	records := candidateRecords([]wheel.CandidateEvaluation{candidate})
	newJSON, err := json.Marshal(records[0])
	if err != nil {
		t.Fatalf("marshal typed candidate: %v", err)
	}
	var oldValue, newValue any
	if err := json.Unmarshal(oldJSON, &oldValue); err != nil {
		t.Fatalf("decode domain candidate: %v", err)
	}
	if err := json.Unmarshal(newJSON, &newValue); err != nil {
		t.Fatalf("decode typed candidate: %v", err)
	}
	if !reflect.DeepEqual(oldValue, newValue) {
		t.Fatalf("typed candidate changed JSON\nold: %s\nnew: %s", oldJSON, newJSON)
	}
}

func TestRunOnceAlertReady(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	quoter := &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts: map[string]futu.OptionQuoteEx{
			contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now),
		},
	}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter: quoter, Positions: fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain: fakeChain{contracts: []futu.OptionContract{contract}}, Store: store, Watchlist: wl,
	})

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(store.signals) != 1 {
		t.Fatalf("signals = %d; want 1", len(store.signals))
	}
	if len(store.actions) != 0 {
		t.Fatalf("actions = %+v; want none when reviewer is not configured", store.actions)
	}
	sig := store.signals[0]
	if sig.Symbol != symbol || sig.Action != "ALERT" || sig.CapabilityStatus != "READY" || sig.ConfigVersion != 1 {
		t.Fatalf("signal = %+v; want ALERT/READY v1 for %s", sig, symbol)
	}
	if len(sig.BlockedBy) != 0 {
		t.Fatalf("BlockedBy = %v; want empty for READY", sig.BlockedBy)
	}
	inv := sig.Inventory
	if inv.CurrentPrice == nil || *inv.CurrentPrice != 600 || inv.ActualInventory == nil || inv.OptionDeltaStock == nil ||
		inv.EffectiveInventory == nil || inv.TargetInventory == nil || inv.InventoryGap == nil {
		t.Fatalf("inventory snapshot incomplete: %+v", inv)
	}
	if len(sig.Candidates) == 0 {
		t.Fatal("ALERT requires at least one candidate")
	}
	if sig.Candidates[0].Quote == nil || sig.Candidates[0].Quote.Last != 4.05 {
		t.Fatalf("candidate last = %v; want 4.05", sig.Candidates[0].Quote)
	}
	if len(wl.status) != 1 || wl.status[0] != (statusCall{symbol, "READY", ""}) {
		t.Fatalf("watchlist status = %+v; want READY with empty reason", wl.status)
	}
}

func TestRunOnceInventoryIsolatedPerSymbol(t *testing.T) {
	const hkSymbol = "HK.00700"
	const usSymbol = "US.JD"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", hkSymbol, 650, now.AddDate(0, 0, 7))
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{
		hkSymbol: configRecord(hkSymbol),
		usSymbol: configRecord(usSymbol),
	}}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{hkSymbol: 600, usSymbol: 600},
			opts:   map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now)},
		},
		Positions: fakePositions{
			{Symbol: hkSymbol, Code: "00700", Qty: 200, Side: SideLong},
			{Symbol: "HK.00883", Code: "00883", Qty: 22000, Side: SideLong},
		},
		Chain: fakeChain{contracts: []futu.OptionContract{contract}},
		Store: store,
		Watchlist: &fakeWatchlist{items: []watchlist.Item{
			wheelItem(hkSymbol),
			wheelItem(usSymbol),
		}},
	})

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(store.signals) != 2 {
		t.Fatalf("signals = %d; want one per wheel symbol", len(store.signals))
	}
	bySymbol := make(map[string]wheelstore.SignalRecord, len(store.signals))
	for _, signal := range store.signals {
		bySymbol[signal.Symbol] = signal
	}
	if got := *bySymbol[hkSymbol].Inventory.ActualInventory; got != 200 {
		t.Fatalf("%s actual inventory = %v; want 200 shares, excluding HK.00883", hkSymbol, got)
	}
	if got := *bySymbol[usSymbol].Inventory.ActualInventory; got != 0 {
		t.Fatalf("%s actual inventory = %v; want no US.JD shares", usSymbol, got)
	}
}

func TestRunOnceAsOfAfterOptionQuotes(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	quoter := &fakeQuoter{
		prices:               map[string]float64{symbol: 600},
		opts:                 map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now)},
		optionDelay:          5 * time.Millisecond,
		stampOptionQuoteTime: true,
	}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter: quoter, Positions: fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:     fakeChain{contracts: []futu.OptionContract{contract}},
		Store:     store,
		Watchlist: &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
	})

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(store.signals) != 1 {
		t.Fatalf("signals = %d; want 1", len(store.signals))
	}
	if got := store.signals[0].CapabilityStatus; got != "READY" {
		t.Fatalf("CapabilityStatus = %s; want READY for quote timestamped during OptionQuotes", got)
	}
}

func TestRunOnceLLMGateStates(t *testing.T) {
	const approved, rejected, failed, after = "HK.00710", "HK.00711", "HK.00712", "HK.00713"
	symbols := []string{approved, rejected, failed, after}
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", approved, 650, now.AddDate(0, 0, 7))
	quoter := &fakeQuoter{
		prices: map[string]float64{approved: 600, rejected: 600, failed: 600, after: 600},
		opts:   map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now)},
	}
	configs := map[string]*wheelstore.ConfigRecord{}
	items := make([]watchlist.Item, 0, len(symbols))
	positions := make(fakePositions, 0, len(symbols))
	for _, symbol := range symbols {
		configs[symbol] = configRecord(symbol)
		items = append(items, wheelItem(symbol))
		positions = append(positions, Position{Symbol: symbol, Code: symbol[3:], Qty: 500, Side: SideLong, AvgCost: 500})
	}
	store := &fakeStore{configs: configs}
	reviewer := &fakeReviewer{
		results: map[string]llmreview.ReviewResult{
			approved: {Verdict: "APPROVE", Reasons: []string{"within budget"}, Notes: "ok"},
			rejected: {Verdict: "REJECT", Reasons: []string{"risk limit"}},
			after:    {Verdict: "APPROVE", Reasons: []string{"after failure"}},
		},
		errors: map[string]error{failed: errors.New("fake llm timeout")},
	}
	r := testRunner(t, Dependencies{
		Quoter: quoter, Positions: positions, Chain: fakeChain{contracts: []futu.OptionContract{contract}},
		Store: store, Watchlist: &fakeWatchlist{items: items}, LLMReviewer: reviewer, LLMModel: "test-model",
	})

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(store.signals) != len(symbols) {
		t.Fatalf("signals = %d; want %d", len(store.signals), len(symbols))
	}
	// reviews run off the pass loop now; wait for the worker to land them.
	// failed 场景重试也失败:首次 + 重试各落一条 LLM_REVIEW_FAILED(审计保留
	// 两次错误),其余各一条 → 总动作 = 标的数 + 1。
	waitFor(t, func() bool { return reviewer.requestCount() == len(symbols)+1 },
		fmt.Sprintf("expected %d review requests", len(symbols)+1))
	waitFor(t, func() bool { return store.actionCount() == len(symbols)+1 },
		fmt.Sprintf("expected %d review actions", len(symbols)+1))
	for _, req := range reviewer.requests {
		if req.RulesText == "" || req.StrategyConfig == nil || req.Signal == nil || req.Positions == nil {
			t.Fatalf("review request missing audit input: %+v", req)
		}
	}
	ids := map[string]int64{}
	for i, signal := range store.signals {
		ids[signal.Symbol] = int64(i + 1)
	}
	actions := map[int64]wheelstore.ActionRecord{}
	for _, action := range store.actions {
		actions[action.SignalID] = action
	}
	assertAction := func(symbol, action, verdict string) wheelstore.ActionRecord {
		t.Helper()
		got, ok := actions[ids[symbol]]
		if !ok {
			t.Fatalf("no action for %s: %+v", symbol, actions)
		}
		if got.Action != action || got.Actor != "llm:test-model" || got.Details["verdict"] != verdict {
			t.Fatalf("action for %s = %+v; want %s/llm:test-model/%s", symbol, got, action, verdict)
		}
		if got.Details["input_summary"] == nil {
			t.Fatalf("action for %s lacks input summary: %+v", symbol, got)
		}
		return got
	}
	approvedAction := assertAction(approved, "LLM_REVIEW", "APPROVE")
	if approvedAction.Details["reasons"].([]string)[0] != "within budget" {
		t.Fatalf("approved reasons = %+v", approvedAction.Details["reasons"])
	}
	rejectedAction := assertAction(rejected, "REJECTED", "REJECT")
	if rejectedAction.Details["reasons"].([]string)[0] != "risk limit" {
		t.Fatalf("rejected reasons = %+v", rejectedAction.Details["reasons"])
	}
	// 审核请求失败(瞬态)落 LLM_REVIEW_FAILED,不是模型裁决,不得冒充
	// REJECTED(2026-08-13: signal 741 DNS 超时被硬记 REJECTED 的教训)。
	failedAction := assertAction(failed, "LLM_REVIEW_FAILED", "REJECT")
	if failedAction.Details["error"] != "fake llm timeout" {
		t.Fatalf("failed review details = %+v", failedAction.Details)
	}
	assertAction(after, "LLM_REVIEW", "APPROVE")
}

func TestReviewAlertDirectionReversalIsRejected(t *testing.T) {
	const symbol = "HK.00700"
	store := &fakeStore{}
	reviewer := &fakeReviewer{results: map[string]llmreview.ReviewResult{
		symbol: {Verdict: "REJECT", Reasons: []string{"方向反转: 正库存缺口却给出 CALL"}},
	}}
	r := testRunner(t, Dependencies{Store: store, LLMReviewer: reviewer, LLMModel: "fake-risk"})
	reversed := wheel.Signal{
		Action: wheel.ActionAlert, Direction: wheel.DirectionCall, Quantity: 1, SignedContracts: -1,
		TargetInventory: 600, EffectiveInventory: 400, InventoryGap: 200,
		CapabilityStatus: wheel.CapabilityReady,
	}
	r.reviewAlert(context.Background(), symbol, 42, 1, wheel.Config{Strategy: "wheel"}, reversed, wheelstore.SignalRecord{Symbol: symbol, Action: "ALERT"}, nil, 450, 12345.67, true)

	if len(reviewer.requests) != 1 {
		t.Fatalf("review requests = %d; want 1", len(reviewer.requests))
	}
	gotSignal, ok := reviewer.requests[0].Signal.(wheel.Signal)
	if !ok || gotSignal.Direction != wheel.DirectionCall || gotSignal.InventoryGap <= 0 {
		t.Fatalf("fake scenario did not carry reversed signal: %#v", reviewer.requests[0].Signal)
	}
	req := reviewer.requests[0]
	if req.CashAvailable == nil || *req.CashAvailable != 12345.67 {
		t.Fatalf("review request cash = %v; want 12345.67", req.CashAvailable)
	}
	if req.CurrentPrice != 450 {
		t.Fatalf("review request current price = %v; want 450", req.CurrentPrice)
	}
	if len(store.actions) != 1 || store.actions[0].Action != "REJECTED" || store.actions[0].Details["verdict"] != "REJECT" {
		t.Fatalf("actions = %+v; want fail-closed REJECTED", store.actions)
	}
	reasons, ok := store.actions[0].Details["reasons"].([]string)
	if !ok || len(reasons) != 1 || !strings.Contains(reasons[0], "方向反转") {
		t.Fatalf("reasons = %#v; want direction reversal", store.actions[0].Details["reasons"])
	}
}

// blockingReviewer starts once and then blocks until the context is
// cancelled — the shape of a real LLM call that takes minutes.
type blockingReviewer struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
}

func (b *blockingReviewer) Review(ctx context.Context, req llmreview.ReviewRequest) (llmreview.ReviewResult, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return llmreview.ReviewResult{}, ctx.Err()
}

func (b *blockingReviewer) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// TestRunOnceAlertReviewDoesNotBlock: an in-flight audit must not stall the
// run loop (2026-08-14: the synchronous reviewAlert stalled every symbol for
// the LLM call duration; signals 868/870/871).
func TestRunOnceAlertReviewDoesNotBlock(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	ctx, cancel := context.WithCancel(context.Background())
	reviewer := &blockingReviewer{started: make(chan struct{}, 1)}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{symbol: 600},
			opts:   map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now)},
		},
		Positions:   fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:       fakeChain{contracts: []futu.OptionContract{contract}},
		Store:       &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}},
		Watchlist:   &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
		LLMReviewer: reviewer,
		LLMModel:    "test-model",
	})

	done := make(chan error, 1)
	go func() { done <- r.RunOnce(ctx) }()
	select {
	case <-reviewer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("review never started")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunOnce blocked on the in-flight LLM review")
	}
	cancel()
	r.Close()
}

// TestReviewInFlightDedup: a symbol that keeps ALERTing (candidate stays in
// range) must not queue one audit per pass while one is in flight — only the
// first ALERT is reviewed. 2026-08-14 JD: every pass re-alerted and every
// round spawned a fresh 4-minute audit.
func TestReviewInFlightDedup(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contractA := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	ctx, cancel := context.WithCancel(context.Background())
	reviewer := &blockingReviewer{started: make(chan struct{}, 1)}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{symbol: 600},
			opts:   map[string]futu.OptionQuoteEx{contractA.Symbol: fullCallQuote(contractA.Symbol, 650, contractA.Expiry, now)},
		},
		Positions:   fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:       fakeChain{contracts: []futu.OptionContract{contractA}},
		Store:       &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}},
		Watchlist:   &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
		LLMReviewer: reviewer,
		LLMModel:    "test-model",
	})

	if err := r.RunOnce(ctx); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	select {
	case <-reviewer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("review never started")
	}
	// second pass with a different contract: not suppressed (candidate
	// differs) but the in-flight audit dedupes it.
	contractB := callContract("HK.TCH260901C660000", symbol, 660, now.AddDate(0, 0, 7))
	r.deps.Chain = fakeChain{contracts: []futu.OptionContract{contractB}}
	r.deps.Quoter = &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts:   map[string]futu.OptionQuoteEx{contractB.Symbol: fullCallQuote(contractB.Symbol, 660, contractB.Expiry, now)},
	}
	if err := r.RunOnce(ctx); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	// give a wrongly-enqueued second review a chance to start, then assert
	// only the first ALERT was audited.
	time.Sleep(200 * time.Millisecond)
	if n := reviewer.callCount(); n != 1 {
		t.Fatalf("review calls = %d; want 1 (in-flight dedup)", n)
	}
	cancel()
	r.Close()
}

// TestRepeatAlertSuppression: the same symbol+contract ALERTing again inside
// repeatAlertWindow is downgraded to HOLD (window not extended); a different
// contract still ALERTs; once the window expires the candidate ALERTs again
// (and is re-audited).
func TestRepeatAlertSuppression(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	quoter := &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts:   map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now)},
	}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter:    quoter,
		Positions: fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:     fakeChain{contracts: []futu.OptionContract{contract}},
		Store:     store,
		Watchlist: &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
		LLMReviewer: &fakeReviewer{results: map[string]llmreview.ReviewResult{
			symbol: {Verdict: "APPROVE", Reasons: []string{"ok"}},
		}},
		LLMModel: "test-model",
	})
	defer r.Close()
	run := func() error { return r.RunOnce(context.Background()) }

	if err := run(); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if store.signalCount() != 1 || store.signals[0].Action != "ALERT" {
		t.Fatalf("signals after pass 1 = %+v; want one ALERT", store.signals)
	}
	if err := run(); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	if store.signalCount() != 2 || store.signals[1].Action != "HOLD" ||
		!strings.Contains(store.signals[1].Reason, "重复候选抑制") {
		t.Fatalf("signals after pass 2 = %+v; want HOLD (repeat candidate)", store.signals)
	}
	// a different contract is not suppressed.
	contractB := callContract("HK.TCH260901C660000", symbol, 660, now.AddDate(0, 0, 7))
	r.deps.Chain = fakeChain{contracts: []futu.OptionContract{contractB}}
	r.deps.Quoter = &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts:   map[string]futu.OptionQuoteEx{contractB.Symbol: fullCallQuote(contractB.Symbol, 660, contractB.Expiry, now)},
	}
	if err := run(); err != nil {
		t.Fatalf("pass 3: %v", err)
	}
	if store.signalCount() != 3 || store.signals[2].Action != "ALERT" {
		t.Fatalf("signals after pass 3 = %+v; want ALERT (different contract)", store.signals)
	}
	// window expired: the original contract alerts again (and is re-audited).
	r.commitAlertBaseline(symbol, wheel.Signal{Quote: &wheel.OptionQuote{Symbol: contract.Symbol}}, time.Now().Add(-repeatAlertWindow-time.Minute))
	r.deps.Chain = fakeChain{contracts: []futu.OptionContract{contract}}
	r.deps.Quoter = quoter
	if err := run(); err != nil {
		t.Fatalf("pass 4: %v", err)
	}
	if store.signalCount() != 4 || store.signals[3].Action != "ALERT" {
		t.Fatalf("signals after pass 4 = %+v; want ALERT (window expired)", store.signals)
	}
}

// TestReviewFailureClearsSuppression: an audit that fails on the
// infrastructure side (reviewer error) must not leave the repeat-candidate
// baseline in place — the next pass re-alerts the same contract and
// re-audits instead of being muted for repeatAlertWindow (reviewer P1-1,
// 2026-08-14: async reviews turned the pre-async implicit per-pass retry
// into a 30-minute lock-out).
func TestReviewFailureClearsSuppression(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	quoter := &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts:   map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now)},
	}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	reviewer := &fakeReviewer{errors: map[string]error{symbol: errors.New("llm unreachable")}}
	r := testRunner(t, Dependencies{
		Quoter:      quoter,
		Positions:   fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:       fakeChain{contracts: []futu.OptionContract{contract}},
		Store:       store,
		Watchlist:   &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
		LLMReviewer: reviewer,
		LLMModel:    "test-model",
	})
	defer r.Close()
	run := func() error { return r.RunOnce(context.Background()) }

	if err := run(); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if store.signalCount() != 1 || store.signals[0].Action != "ALERT" {
		t.Fatalf("signals after pass 1 = %+v; want one ALERT", store.signals)
	}
	// the failed audit lands as LLM_REVIEW_FAILED actions: first attempt +
	// one retry (2026-08-13 gate retry). Wait for both before pass 2 — the
	// baseline is cleared only after the retry settles.
	waitFor(t, func() bool { return reviewer.requestCount() == 2 }, "review retry never ran")
	waitFor(t, func() bool { return store.actionCount() == 2 }, "failure actions never persisted")

	if err := run(); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	// same contract, inside the window: not suppressed — the failed audit
	// cleared the baseline, so the candidate re-alerts and re-audits.
	if store.signalCount() != 2 || store.signals[1].Action != "ALERT" {
		t.Fatalf("signals after pass 2 = %+v; want a second ALERT (baseline cleared by failure)", store.signals)
	}
	waitFor(t, func() bool { return reviewer.requestCount() == 4 }, "pass 2 audit was not retried")
	waitFor(t, func() bool { return store.actionCount() == 4 }, "second failure actions never persisted")
}

// TestRunOnceNoPricePersistsDataBlocked: a symbol without a current price
// records a fail-closed signal while the next binding still runs.
func TestRunOnceNoPricePersistsDataBlocked(t *testing.T) {
	const bad, good = "HK.00001", "HK.00002"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", good, 650, now.AddDate(0, 0, 7))
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{bad: configRecord(bad), good: configRecord(good)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(bad), wheelItem(good)}}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{good: 600},
			opts: map[string]futu.OptionQuoteEx{
				contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now),
			},
		},
		Positions: fakePositions{{Symbol: good, Code: "00002", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:     fakeChain{contracts: []futu.OptionContract{contract}},
		Store:     store, Watchlist: wl,
	})

	err := r.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() = nil; want aggregate error for the missing price symbol")
	}
	if len(store.signals) != 2 || store.signals[0].Symbol != bad || store.signals[1].Symbol != good {
		t.Fatalf("signals = %+v; want DATA_BLOCKED %s and completed %s", store.signals, bad, good)
	}
	if store.signals[0].Action != "HOLD" || store.signals[0].CapabilityStatus != "DATA_BLOCKED" ||
		len(store.signals[0].BlockedBy) != 1 || store.signals[0].BlockedBy[0] != "current_price" {
		t.Fatalf("blocked signal = %+v; want HOLD/DATA_BLOCKED/current_price", store.signals[0])
	}
	if len(wl.status) != 2 || wl.status[0].symbol != bad || wl.status[0].status != "DATA_BLOCKED" || wl.status[1].symbol != good {
		t.Fatalf("watchlist status = %+v; want both symbols synced", wl.status)
	}
}

// TestRunOnceHoldDataBlocked: candidates without a complete quote snapshot
// yield HOLD with DATA_BLOCKED capability and a non-empty blocker, syncing
// the binding to DATA_BLOCKED with the first blocker as reason.
func TestRunOnceHoldDataBlocked(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{symbol: 600},
			opts:   map[string]futu.OptionQuoteEx{}, // chain contract has no live quote
		},
		Positions: fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:     fakeChain{contracts: []futu.OptionContract{contract}},
		Store:     store, Watchlist: wl,
	})

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(store.signals) != 1 {
		t.Fatalf("signals = %d; want 1", len(store.signals))
	}
	sig := store.signals[0]
	if sig.Action != "HOLD" || sig.CapabilityStatus != "DATA_BLOCKED" {
		t.Fatalf("signal = %+v; want HOLD/DATA_BLOCKED", sig)
	}
	if len(sig.BlockedBy) == 0 {
		t.Fatal("DATA_BLOCKED requires at least one blocker")
	}
	if len(wl.status) != 1 || wl.status[0].symbol != symbol || wl.status[0].status != "DATA_BLOCKED" ||
		wl.status[0].reason != sig.BlockedBy[0] {
		t.Fatalf("watchlist status = %+v; want DATA_BLOCKED with blocker %v", wl.status, sig.BlockedBy)
	}
}

// TestRunOnceSymbolFailureContinues: a binding whose config is missing (or any
// stage fails) is logged and skipped; the loop still evaluates the rest.
func TestRunOnceSymbolFailureContinues(t *testing.T) {
	const bad, good = "HK.00003", "HK.00004"
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", good, 650, now.AddDate(0, 0, 7))
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{good: configRecord(good)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(bad), wheelItem(good)}}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{good: 600},
			opts: map[string]futu.OptionQuoteEx{
				contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now),
			},
		},
		Positions: fakePositions{{Symbol: good, Code: "00004", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:     fakeChain{contracts: []futu.OptionContract{contract}},
		Store:     store, Watchlist: wl,
	})

	err := r.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() = nil; want aggregate error for the config-missing symbol")
	}
	if len(store.signals) != 1 || store.signals[0].Symbol != good {
		t.Fatalf("signals = %+v; want only %s evaluated", store.signals, good)
	}
}

// TestMapSignalFallbackBlocker: a DATA_BLOCKED signal without explicit
// blockers gets the fallback blocker (validateSignal contract), and a risk
// HOLD with READY capability stays READY.
func TestMapSignalFallbackBlocker(t *testing.T) {
	base := wheel.Signal{Action: wheel.ActionHold, Direction: wheel.DirectionHold, Reason: "hold", CapabilityStatus: wheel.CapabilityDataBlocked}
	rec, status, reason := mapSignal("HK.00700", 2, base, 100)
	if rec.CapabilityStatus != "DATA_BLOCKED" || len(rec.BlockedBy) != 1 || rec.BlockedBy[0] != fallbackBlocker {
		t.Fatalf("record = %+v; want DATA_BLOCKED with fallback blocker", rec)
	}
	if status != "DATA_BLOCKED" || reason != fallbackBlocker {
		t.Fatalf("status pair = (%s, %q); want (DATA_BLOCKED, %s)", status, reason, fallbackBlocker)
	}
	riskHold := wheel.Signal{Action: wheel.ActionHold, Direction: wheel.DirectionHold, Reason: "inventory gap is within no-trade band", CapabilityStatus: wheel.CapabilityReady}
	rec, status, reason = mapSignal("HK.00700", 2, riskHold, 100)
	if rec.CapabilityStatus != "READY" || len(rec.BlockedBy) != 0 {
		t.Fatalf("record = %+v; want READY without blockers", rec)
	}
	if status != "READY" || reason != "" {
		t.Fatalf("status pair = (%s, %q); want (READY, empty)", status, reason)
	}
}

// TestRunRejectsNonPositiveInterval guards the ticker against a hot loop.
func TestRunRejectsNonPositiveInterval(t *testing.T) {
	r := testRunner(t, Dependencies{})
	if err := r.Run(context.Background(), 0); err == nil {
		t.Fatal("Run(0) = nil; want interval error")
	}
	if err := r.Run(context.Background(), -time.Second); err == nil {
		t.Fatal("Run(-1s) = nil; want interval error")
	}
}

func openWheelrunIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WBOT_PG_DSN")
	if dsn == "" {
		t.Skip("WBOT_PG_DSN not set")
	}
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateUp(database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func cleanWheelrunIntegration(t *testing.T, database *sql.DB, symbol string) {
	t.Helper()
	if _, err := database.Exec(`DELETE FROM wheel_signal_actions WHERE signal_id IN (SELECT id FROM wheel_signals WHERE symbol = $1)`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM wheel_signals WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM wheel_configs WHERE symbol = $1`, symbol); err != nil {
		t.Fatal(err)
	}
}

func TestRunOnceLLMReviewVisibleToLatestLLMReview(t *testing.T) {
	database := openWheelrunIntegrationDB(t)
	ctx := context.Background()
	symbol := "WHEELRUN.LLM"
	cleanWheelrunIntegration(t, database, symbol)
	t.Cleanup(func() { cleanWheelrunIntegration(t, database, symbol) })

	store := wheelstore.New(database)
	if _, err := store.AppendConfig(ctx, *configRecord(symbol)); err != nil {
		t.Fatalf("AppendConfig: %v", err)
	}
	now := time.Now()
	contract := callContract("HK.TCH260901C650000", symbol, 650, now.AddDate(0, 0, 7))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"verdict\":\"APPROVE\",\"reasons\":[\"integration ok\"],\"notes\":\"\"}"}}]}`)
	}))
	defer server.Close()
	reviewer, err := llmreview.New(server.URL, "test-key", "pg-test")
	if err != nil {
		t.Fatalf("llmreview.New: %v", err)
	}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{symbol: 600},
			opts:   map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 650, contract.Expiry, now)},
		},
		Positions:   fakePositions{{Symbol: symbol, Code: "LLM", Qty: 500, Side: SideLong, AvgCost: 500}},
		Chain:       fakeChain{contracts: []futu.OptionContract{contract}},
		Store:       store,
		Watchlist:   &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
		LLMReviewer: reviewer,
		LLMModel:    "pg-test",
	})
	if err := r.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	signals, err := store.ListSignals(ctx, symbol, "ALERT", "", 10)
	if err != nil || len(signals) != 1 {
		t.Fatalf("ListSignals = %+v, err=%v; want one ALERT", signals, err)
	}
	// the audit lands on a worker goroutine now; poll until it is persisted.
	var review *wheelstore.ActionRecord
	waitFor(t, func() bool {
		got, err := store.LatestLLMReview(ctx, signals[0].ID)
		if err != nil {
			return false
		}
		review = got
		return true
	}, "LLM review persisted")
	if review.Actor != "llm:pg-test" || review.Details["verdict"] != "APPROVE" || review.Details["input_summary"] == nil {
		t.Fatalf("review = %+v; want persisted APPROVE audit", review)
	}
}
