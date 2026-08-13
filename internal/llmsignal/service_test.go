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
func (s *testStore) HasAction(context.Context, int64, string) (bool, error) { return false, nil }
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
	return Context{CashAvailable: &cash, Positions: []Position{{Symbol: "HK.00700", Qty: actual}}, Inventory: wheelstore.InventorySnapshot{CurrentPrice: &price, ActualInventory: &actual, OptionDeltaStock: &zero, EffectiveInventory: &actual, TargetInventory: &actual, InventoryGap: &zero}}
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

func TestSyntheticOptionRequiresExpiry(t *testing.T) {
	if _, err := SyntheticOptionCode("HK.00700", "PUT", 450, ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("err=%v", err)
	}
}
