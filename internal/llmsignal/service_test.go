package llmsignal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/wheelstore"
)

type testStore struct {
	signals []wheelstore.SignalRecord
	actions []wheelstore.ActionRecord
}

func (s *testStore) AppendSignal(_ context.Context, r wheelstore.SignalRecord) (int64, error) {
	s.signals = append(s.signals, r)
	return int64(len(s.signals)), nil
}
func (s *testStore) AppendAction(_ context.Context, r wheelstore.ActionRecord) (int64, error) {
	s.actions = append(s.actions, r)
	return int64(len(s.actions)), nil
}
func (s *testStore) LatestConfig(context.Context, string) (*wheelstore.ConfigRecord, error) {
	return nil, wheelstore.ErrNotFound
}
func (s *testStore) ListSignals(context.Context, string, string, string, int) ([]wheelstore.SignalRecord, error) {
	return s.signals, nil
}
func (s *testStore) GetSignal(context.Context, int64) (*wheelstore.SignalRecord, error) {
	return nil, wheelstore.ErrNotFound
}
func (s *testStore) LatestLLMReview(context.Context, int64) (*wheelstore.ActionRecord, error) {
	return nil, wheelstore.ErrNotFound
}
func (s *testStore) LatestAction(context.Context, int64, string) (*wheelstore.ActionRecord, error) {
	return nil, wheelstore.ErrNotFound
}
func (s *testStore) HasAction(_ context.Context, signalID int64, action string) (bool, error) {
	for _, a := range s.actions {
		if a.SignalID == signalID && a.Action == action {
			return true, nil
		}
	}
	return false, nil
}
func (s *testStore) QuerySignalsSince(context.Context, string, int64, int) ([]wheelstore.SignalRecord, error) {
	return nil, nil
}
func (s *testStore) MaxSignalID(context.Context) (int64, error)                   { return 0, nil }
func (s *testStore) Dismiss(context.Context, string, time.Time) error             { return nil }
func (s *testStore) IsDismissed(context.Context, string, time.Time) (bool, error) { return false, nil }

type approve struct{}

func (approve) Review(context.Context, llmreview.ReviewRequest) (llmreview.ReviewResult, error) {
	return llmreview.ReviewResult{Verdict: "APPROVE", Reasons: []string{"ok"}}, nil
}

func validContext() Context {
	cash := 100000.0
	actual, zero := 200.0, 0.0
	price := 459.0
	return Context{
		CashAvailable: &cash,
		Positions:     []Position{{Symbol: "HK.00700", Qty: actual}},
		Inventory:     wheelstore.InventorySnapshot{CurrentPrice: &price, ActualInventory: &actual, OptionDeltaStock: &zero, EffectiveInventory: &actual, TargetInventory: &actual, InventoryGap: &zero},
		ObservedOptions: map[string]ObservedOption{
			"HK.TCH260821P450000": {Strike: 450, Expiry: "2026-08-21T00:00:00Z", Premium: 8.5, Delta: -.35, IV: .4, OpenInterest: 100},
		},
	}
}
func validDecision() Decision {
	return Decision{Symbol: "HK.00700", Direction: "PUT", Quantity: 1, Contract: "HK.TCH260821P450000", Strike: 450, Expiry: "2026-08-21T00:00:00Z", CurrentPrice: 459, Premium: 8.5, Delta: -.35, IV: .4, OpenInterest: 100, Reason: "行权价低于现价并收取权利金"}
}

func TestSubmitValidatesBeforeImmutableAppend(t *testing.T) {
	store := &testStore{}
	svc := &Service{Store: store, Reviewer: approve{}, Model: "review", Now: func() time.Time { return time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC) }}
	ctx := validContext()
	result, err := svc.Submit(context.Background(), validDecision(), ctx, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if result.SignalID != 1 || len(store.signals) != 1 || len(store.actions) != 1 {
		t.Fatalf("result/store=%+v/%d/%d", result, len(store.signals), len(store.actions))
	}
	got := store.signals[0].Inventory
	if got.ActualInventory == nil || *got.ActualInventory != 200 {
		t.Fatalf("persisted inventory=%+v", got)
	}
}

func TestSubmitRejectsHardConstraintsWithoutAlert(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Decision, *Context)
	}{{"expired", func(d *Decision, _ *Context) { d.Expiry = "2026-08-01"; d.Contract = "HK.TCH260801P450000" }}, {"expiry mismatch", func(d *Decision, _ *Context) { d.Expiry = "2026-08-22" }}, {"delta sign", func(d *Decision, _ *Context) { d.Delta = .2 }}, {"cash secured", func(_ *Decision, c *Context) { v := 1.0; c.CashAvailable = &v }}, {"quantity", func(d *Decision, _ *Context) { d.Quantity = 6 }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &testStore{}
			svc := &Service{Store: store, Now: func() time.Time { return time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC) }}
			d, c := validDecision(), validContext()
			tc.mutate(&d, &c)
			_, err := svc.Submit(context.Background(), d, c, Policy{})
			if !errors.Is(err, ErrRejected) {
				t.Fatalf("err=%v", err)
			}
			if len(store.signals) != 0 {
				t.Fatalf("rejected decision appended %+v", store.signals)
			}
		})
	}
}

func TestSubmitDailyLimitCountsOnlyApprovedAlerts(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	today := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	alert := func(id int64) wheelstore.SignalRecord {
		return wheelstore.SignalRecord{ID: id, Symbol: "HK.00700", Action: "ALERT", CreatedAt: today}
	}
	rejected := func(id int64) wheelstore.ActionRecord {
		return wheelstore.ActionRecord{SignalID: id, Action: "REJECTED", Actor: "llm:review"}
	}
	approved := func(id int64) wheelstore.ActionRecord {
		return wheelstore.ActionRecord{SignalID: id, Action: "LLM_REVIEW", Actor: "llm:review"}
	}
	cases := []struct {
		name         string
		alerts       []wheelstore.SignalRecord
		actions      []wheelstore.ActionRecord
		wantRejected bool
	}{
		{"five approved block", []wheelstore.SignalRecord{alert(1), alert(2), alert(3), alert(4), alert(5)}, []wheelstore.ActionRecord{approved(1), approved(2), approved(3), approved(4), approved(5)}, true},
		{"five rejected do not block", []wheelstore.SignalRecord{alert(1), alert(2), alert(3), alert(4), alert(5)}, []wheelstore.ActionRecord{rejected(1), rejected(2), rejected(3), rejected(4), rejected(5)}, false},
		{"mixed three approved pass", []wheelstore.SignalRecord{alert(1), alert(2), alert(3), alert(4)}, []wheelstore.ActionRecord{approved(1), approved(2), approved(3), rejected(4)}, false},
		{"yesterday approved does not count", []wheelstore.SignalRecord{{ID: 9, Symbol: "HK.00700", Action: "ALERT", CreatedAt: time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)}, alert(1), alert(2)}, []wheelstore.ActionRecord{approved(9), approved(1), approved(2)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &testStore{signals: tc.alerts, actions: tc.actions}
			svc := &Service{Store: store, Reviewer: approve{}, Model: "m", Now: func() time.Time { return now }}
			_, err := svc.Submit(context.Background(), validDecision(), validContext(), Policy{MaxDailySignals: 5})
			if tc.wantRejected != errors.Is(err, ErrRejected) {
				t.Fatalf("wantRejected=%v err=%v", tc.wantRejected, err)
			}
		})
	}
}

func TestSubmitRejectsFabricatedOptionWhenObservedOptionsEmpty(t *testing.T) {
	store := &testStore{}
	svc := &Service{Store: store, Now: func() time.Time { return time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC) }}
	account := validContext()
	account.ObservedOptions = nil

	_, err := svc.Submit(context.Background(), validDecision(), account, Policy{})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err=%v", err)
	}
	if len(store.signals) != 0 {
		t.Fatalf("fabricated option appended %+v", store.signals)
	}
}

func TestSyntheticOptionRequiresExpiry(t *testing.T) {
	if _, err := SyntheticOptionCode("HK.00700", "PUT", 450, ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("err=%v", err)
	}
}
