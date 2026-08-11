package wheelrun

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/futu"
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
			"price_position_curve": []any{
				map[string]any{"price": 400.0, "target_inventory": 1200.0},
				map[string]any{"price": 550.0, "target_inventory": 0.0},
			},
			"max_inventory":      1200.0,
			"min_option_quality": 0.0,
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
	prices map[string]float64
	opts   map[string]futu.OptionQuoteEx // keyed by contract symbol
	perr   error
	oerr   error
}

func (f *fakeQuoter) Quote(ctx context.Context, symbol string) (float64, error) {
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
	if f.oerr != nil {
		return nil, f.oerr
	}
	out := map[string]futu.OptionQuoteEx{}
	for _, s := range symbols {
		if q, ok := f.opts[s]; ok {
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
	configs   map[string]*wheelstore.ConfigRecord
	signals   []wheelstore.SignalRecord
	appendErr error
}

func (f *fakeStore) LatestConfig(ctx context.Context, symbol string) (*wheelstore.ConfigRecord, error) {
	rec, ok := f.configs[symbol]
	if !ok {
		return nil, wheelstore.ErrNotFound
	}
	return rec, nil
}

func (f *fakeStore) AppendSignal(ctx context.Context, r wheelstore.SignalRecord) (int64, error) {
	if f.appendErr != nil {
		return 0, f.appendErr
	}
	f.signals = append(f.signals, r)
	return int64(len(f.signals)), nil
}

func (f *fakeStore) ListSignals(ctx context.Context, symbol, action, capability string, limit int) ([]wheelstore.SignalRecord, error) {
	return f.signals, nil
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

// TestRunOnceAlertReady: a full quote snapshot with a short-inventory gap
// yields an ALERT signal (READY capability) and syncs READY with no reason.
func TestRunOnceAlertReady(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C335000", symbol, 335, now.AddDate(0, 0, 7))
	quoter := &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts: map[string]futu.OptionQuoteEx{
			contract.Symbol: fullCallQuote(contract.Symbol, 335, contract.Expiry, now),
		},
	}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter: quoter, Positions: fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong}},
		Chain: fakeChain{contracts: []futu.OptionContract{contract}}, Store: store, Watchlist: wl,
	})

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(store.signals) != 1 {
		t.Fatalf("signals = %d; want 1", len(store.signals))
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
	if len(wl.status) != 1 || wl.status[0] != (statusCall{symbol, "READY", ""}) {
		t.Fatalf("watchlist status = %+v; want READY with empty reason", wl.status)
	}
}

// TestRunOnceNoPriceSkipsSymbol: a symbol without a current price (gateway
// down or unparsable) is skipped with the failure aggregated, while the next
// binding still runs to completion.
func TestRunOnceNoPriceSkipsSymbol(t *testing.T) {
	const bad, good = "HK.00001", "HK.00002"
	now := time.Now()
	contract := callContract("HK.TCH260901C335000", good, 335, now.AddDate(0, 0, 7))
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{bad: configRecord(bad), good: configRecord(good)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(bad), wheelItem(good)}}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{good: 600},
			opts: map[string]futu.OptionQuoteEx{
				contract.Symbol: fullCallQuote(contract.Symbol, 335, contract.Expiry, now),
			},
		},
		Positions: fakePositions{{Symbol: good, Code: "00002", Qty: 500, Side: SideLong}},
		Chain:     fakeChain{contracts: []futu.OptionContract{contract}},
		Store:     store, Watchlist: wl,
	})

	err := r.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() = nil; want aggregate error for the missing price symbol")
	}
	if len(store.signals) != 1 || store.signals[0].Symbol != good {
		t.Fatalf("signals = %+v; want only %s evaluated", store.signals, good)
	}
	if len(wl.status) != 1 || wl.status[0].symbol != good {
		t.Fatalf("watchlist status = %+v; want only %s synced", wl.status, good)
	}
}

// TestRunOnceHoldDataBlocked: candidates without a complete quote snapshot
// yield HOLD with DATA_BLOCKED capability and a non-empty blocker, syncing
// the binding to DATA_BLOCKED with the first blocker as reason.
func TestRunOnceHoldDataBlocked(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C335000", symbol, 335, now.AddDate(0, 0, 7))
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{symbol: 600},
			opts:   map[string]futu.OptionQuoteEx{}, // chain contract has no live quote
		},
		Positions: fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong}},
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
	contract := callContract("HK.TCH260901C335000", good, 335, now.AddDate(0, 0, 7))
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{good: configRecord(good)}}
	wl := &fakeWatchlist{items: []watchlist.Item{wheelItem(bad), wheelItem(good)}}
	r := testRunner(t, Dependencies{
		Quoter: &fakeQuoter{
			prices: map[string]float64{good: 600},
			opts: map[string]futu.OptionQuoteEx{
				contract.Symbol: fullCallQuote(contract.Symbol, 335, contract.Expiry, now),
			},
		},
		Positions: fakePositions{{Symbol: good, Code: "00004", Qty: 500, Side: SideLong}},
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

func TestDailyOrdersUTCDateAndAlertFilter(t *testing.T) {
	now := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{signals: []wheelstore.SignalRecord{
		{Symbol: "HK.00700", Action: "ALERT", CreatedAt: now.Add(-time.Nanosecond)},
		{Symbol: "HK.00700", Action: "HOLD", CreatedAt: now.Add(-time.Nanosecond)},
		{Symbol: "HK.00700", Action: "ALERT", CreatedAt: now},
		{Symbol: "HK.00700", Action: "HOLD", CreatedAt: now},
	}}
	r := testRunner(t, Dependencies{Store: store})

	got, err := r.dailyOrders(context.Background(), "HK.00700", now.Add(12*time.Hour))
	if err != nil {
		t.Fatalf("dailyOrders() error: %v", err)
	}
	if got != 1 {
		t.Fatalf("dailyOrders() = %d; want 1 today's ALERT only", got)
	}
}
