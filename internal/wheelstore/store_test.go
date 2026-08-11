package wheelstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }
func i(v int64) *int64     { return &v }

func validQuote() QuoteSnapshotRecord {
	return QuoteSnapshotRecord{
		Symbol: "TEST.C", Underlying: "TEST", OptionType: "PUT", Strike: 100,
		Expiry: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC), Source: "test",
		SnapshotKey: "batch-1", UnderlyingPrice: f(100), Delta: f(-0.3),
		Bid: f(1), Ask: f(1.2), IV: f(0.3), Theta: f(-0.01), Volume: i(10),
		OpenInterest: i(20), LotSize: i(100), ObservedAt: time.Now().UTC(),
	}
}

func validInventory() InventorySnapshot {
	return InventorySnapshot{CurrentPrice: f(100), ActualInventory: f(500), OptionDeltaStock: f(20), EffectiveInventory: f(520), TargetInventory: f(600), InventoryGap: f(80)}
}

func TestQuoteValidationAndCompleteness(t *testing.T) {
	base := validQuote()
	if err := validateQuote(base); err != nil || !base.Complete() {
		t.Fatalf("valid quote: err=%v complete=%v", err, base.Complete())
	}
	checks := []struct {
		name string
		edit func(*QuoteSnapshotRecord)
	}{
		{"identity", func(q *QuoteSnapshotRecord) { q.Symbol = "" }},
		{"source", func(q *QuoteSnapshotRecord) { q.Source = "" }},
		{"snapshot key", func(q *QuoteSnapshotRecord) { q.SnapshotKey = "" }},
		{"option type", func(q *QuoteSnapshotRecord) { q.OptionType = "STOCK" }},
		{"strike", func(q *QuoteSnapshotRecord) { q.Strike = 0 }},
		{"put delta range", func(q *QuoteSnapshotRecord) { q.Delta = f(0.1) }},
		{"call delta range", func(q *QuoteSnapshotRecord) { q.OptionType = "CALL"; q.Delta = f(-0.1) }},
		{"bid", func(q *QuoteSnapshotRecord) { q.Bid = f(0) }},
		{"crossed market", func(q *QuoteSnapshotRecord) { q.Ask = f(0.5) }},
		{"iv", func(q *QuoteSnapshotRecord) { q.IV = f(-0.1) }},
		{"volume", func(q *QuoteSnapshotRecord) { q.Volume = i(-1) }},
		{"lot size", func(q *QuoteSnapshotRecord) { q.LotSize = i(0) }},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			q := base
			tc.edit(&q)
			if err := validateQuote(q); err == nil {
				t.Fatal("validateQuote returned nil")
			}
		})
	}
	missing := base
	missing.IV = nil
	if err := validateQuote(missing); err != nil {
		t.Fatalf("incomplete quote should be retained: %v", err)
	}
	if missing.Complete() {
		t.Fatal("incomplete quote reported complete")
	}
}

func TestConfigValidation(t *testing.T) {
	if _, _, err := validateConfig(ConfigRecord{Symbol: "TEST", Version: 1, Config: map[string]any{"strategy": "wheel"}}); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for _, r := range []ConfigRecord{
		{Version: 1, Config: map[string]any{}},
		{Symbol: "TEST", Config: map[string]any{}},
		{Symbol: "TEST", Version: 1},
	} {
		if _, _, err := validateConfig(r); err == nil {
			t.Fatal("invalid config returned nil")
		}
	}
}

func TestSignalAndActionValidation(t *testing.T) {
	store := New(&sql.DB{})
	ctx := context.Background()
	if _, err := store.AppendSignal(ctx, SignalRecord{Symbol: "TEST", Action: "NOPE", ConfigVersion: 1, Reason: "x"}); !errors.Is(err, ErrInvalidAction) {
		t.Fatalf("invalid signal action error=%v", err)
	}
	if _, err := store.AppendSignal(ctx, SignalRecord{Symbol: "TEST", Action: "HOLD", ConfigVersion: 1}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("missing reason error=%v", err)
	}
	// Incomplete market/inventory data must fail closed rather than become ALERT.
	if _, err := store.AppendSignal(ctx, SignalRecord{Symbol: "TEST", Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY", Reason: "candidate", Candidates: []map[string]any{{"symbol": "TEST.C"}}}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("incomplete ALERT error=%v", err)
	}
	if _, err := store.AppendSignal(ctx, SignalRecord{Symbol: "TEST", Action: "ALERT", ConfigVersion: 1, Reason: "blocked", Inventory: validInventory(), Candidates: []map[string]any{{"symbol": "TEST.C"}}, CapabilityStatus: "DATA_BLOCKED", BlockedBy: []string{"missing_iv"}}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("blocked ALERT error=%v", err)
	}
	if _, err := store.AppendAction(ctx, ActionRecord{SignalID: 0, Actor: "human", Action: "NOTE"}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("invalid action signal error=%v", err)
	}
	if _, err := store.AppendAction(ctx, ActionRecord{SignalID: 1, Actor: "human", Action: "ORDER"}); !errors.Is(err, ErrInvalidOp) {
		t.Fatalf("invalid operator action error=%v", err)
	}
}

func TestActionValidation(t *testing.T) {
	valid := []ActionRecord{
		{SignalID: 1, Actor: "operator", Action: "NOTE"},
		{SignalID: 1, Actor: "llm:gpt-4o", Action: " llm_review ", Details: map[string]any{"verdict": "APPROVE"}},
		{SignalID: 1, Actor: "telegram:42", Action: "no"},
		{SignalID: 1, Actor: "telegram:42", Action: "REJECTED", Note: "live env not allowed"},
	}
	for _, r := range valid {
		rr := r
		if err := validateAction(&rr); err != nil {
			t.Errorf("valid action %+v: %v", r, err)
		}
		if rr.Action != strings.ToUpper(strings.TrimSpace(r.Action)) {
			t.Errorf("action not normalized: %q -> %q", r.Action, rr.Action)
		}
	}
	for _, r := range []ActionRecord{
		{SignalID: 0, Actor: "human", Action: "NOTE"},
		{SignalID: 1, Actor: "", Action: "NOTE"},
		{SignalID: 1, Actor: "human", Action: "HACK"},
	} {
		if err := validateAction(&r); err == nil {
			t.Errorf("invalid action %+v accepted", r)
		}
	}
	// nil details must fall back to an empty JSON object, not an error.
	if _, err := validateJSONMap("details", nil, true); err != nil {
		t.Fatalf("nil details: %v", err)
	}
}

func TestSignalCapabilityValidation(t *testing.T) {
	validAlert := SignalRecord{
		Symbol: "TEST", Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY",
		Inventory: validInventory(), Candidates: []map[string]any{{"symbol": "TEST.C"}}, Reason: "candidate",
	}
	valid := []SignalRecord{
		validAlert,
		{Symbol: "TEST", Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "READY", Reason: "risk rule"},
		{Symbol: "TEST", Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "DATA_BLOCKED", BlockedBy: []string{"missing_iv"}, Reason: "missing data"},
	}
	for _, signal := range valid {
		if err := validateSignal(&signal); err != nil {
			t.Errorf("valid signal %+v: %v", signal, err)
		}
	}

	invalid := []SignalRecord{
		{Symbol: "TEST", Action: "HOLD", ConfigVersion: 1, Reason: "missing status"},
		{Symbol: "TEST", Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "UNKNOWN", Reason: "unknown status"},
		{Symbol: "TEST", Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "READY", BlockedBy: []string{"missing_iv"}, Reason: "contradiction"},
		{Symbol: "TEST", Action: "HOLD", ConfigVersion: 1, CapabilityStatus: "DATA_BLOCKED", Reason: "missing blocker"},
		{Symbol: "TEST", Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "DATA_BLOCKED", BlockedBy: []string{"missing_iv"}, Inventory: validInventory(), Candidates: []map[string]any{{"symbol": "TEST.C"}}, Reason: "blocked alert"},
	}
	for _, signal := range invalid {
		if err := validateSignal(&signal); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("invalid signal %+v error=%v; want ErrInvalidRecord", signal, err)
		}
	}

	normalized := SignalRecord{Symbol: "TEST", Action: " hold ", ConfigVersion: 1, CapabilityStatus: " data_blocked ", BlockedBy: []string{"missing_iv"}, Reason: "missing data"}
	if err := validateSignal(&normalized); err != nil || normalized.Action != "HOLD" || normalized.CapabilityStatus != "DATA_BLOCKED" {
		t.Fatalf("normalized signal=%+v err=%v", normalized, err)
	}
}

func TestNilDBValidation(t *testing.T) {
	ctx := context.Background()
	s := New(nil)
	if _, err := s.AppendConfig(ctx, ConfigRecord{}); !errors.Is(err, ErrNilDB) {
		t.Errorf("AppendConfig: %v", err)
	}
	if _, err := s.AppendQuoteSnapshot(ctx, QuoteSnapshotRecord{}); !errors.Is(err, ErrNilDB) {
		t.Errorf("AppendQuoteSnapshot: %v", err)
	}
	if _, err := s.AppendSignal(ctx, SignalRecord{}); !errors.Is(err, ErrNilDB) {
		t.Errorf("AppendSignal: %v", err)
	}
	if _, err := s.AppendAction(ctx, ActionRecord{}); !errors.Is(err, ErrNilDB) {
		t.Errorf("AppendAction: %v", err)
	}
	if _, err := s.GetConfig(ctx, "TEST", 1); !errors.Is(err, ErrNilDB) {
		t.Errorf("GetConfig: %v", err)
	}
	if _, err := s.LatestConfig(ctx, "TEST"); !errors.Is(err, ErrNilDB) {
		t.Errorf("LatestConfig: %v", err)
	}
	if _, err := s.ListConfigs(ctx, "TEST", 1); !errors.Is(err, ErrNilDB) {
		t.Errorf("ListConfigs: %v", err)
	}
	if _, err := s.QueryQuoteSnapshots(ctx, "TEST", time.Time{}, time.Time{}, 1); !errors.Is(err, ErrNilDB) {
		t.Errorf("QueryQuoteSnapshots: %v", err)
	}
	if _, err := s.GetSignal(ctx, 1); !errors.Is(err, ErrNilDB) {
		t.Errorf("GetSignal: %v", err)
	}
	if _, err := s.ListSignals(ctx, "TEST", "", "", 1); !errors.Is(err, ErrNilDB) {
		t.Errorf("ListSignals: %v", err)
	}
	if _, err := s.ListActions(ctx, 1); !errors.Is(err, ErrNilDB) {
		t.Errorf("ListActions: %v", err)
	}
	if _, err := s.LatestLLMReview(ctx, 1); !errors.Is(err, ErrNilDB) {
		t.Errorf("LatestLLMReview: %v", err)
	}
	if _, err := s.QuerySignalsSince(ctx, "ALERT", 0, 10); !errors.Is(err, ErrNilDB) {
		t.Errorf("QuerySignalsSince: %v", err)
	}
	if _, err := s.MaxSignalID(ctx); !errors.Is(err, ErrNilDB) {
		t.Errorf("MaxSignalID: %v", err)
	}
	if err := s.Dismiss(ctx, "TEST", time.Now()); !errors.Is(err, ErrNilDB) {
		t.Errorf("Dismiss: %v", err)
	}
	if _, err := s.IsDismissed(ctx, "TEST", time.Now()); !errors.Is(err, ErrNilDB) {
		t.Errorf("IsDismissed: %v", err)
	}
}

func TestDismissValidation(t *testing.T) {
	s := New(&sql.DB{})
	ctx := context.Background()
	for _, tc := range []struct {
		symbol string
		date   time.Time
	}{
		{"", time.Now()},
		{"TEST", time.Time{}},
	} {
		if err := s.Dismiss(ctx, tc.symbol, tc.date); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("Dismiss(%q, %v): %v; want ErrInvalidRecord", tc.symbol, tc.date, err)
		}
		if _, err := s.IsDismissed(ctx, tc.symbol, tc.date); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("IsDismissed(%q, %v): %v; want ErrInvalidRecord", tc.symbol, tc.date, err)
		}
	}
	if _, err := s.LatestLLMReview(ctx, 0); !errors.Is(err, ErrInvalidRecord) {
		t.Errorf("LatestLLMReview(0): %v; want ErrInvalidRecord", err)
	}
	if _, err := s.QuerySignalsSince(ctx, "NOPE", 0, 10); !errors.Is(err, ErrInvalidAction) {
		t.Errorf("QuerySignalsSince(NOPE): %v; want ErrInvalidAction", err)
	}
}
