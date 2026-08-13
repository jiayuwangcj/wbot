package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// fakeLLMStore records appended signals/actions for the endpoint test.
type fakeLLMStore struct {
	mu        sync.Mutex
	signals   []wheelstore.SignalRecord
	actions   []wheelstore.ActionRecord
	dismissed map[string]bool
}

func (f *fakeLLMStore) LatestConfig(context.Context, string) (*wheelstore.ConfigRecord, error) {
	return nil, wheelstore.ErrNotFound
}

func (f *fakeLLMStore) ListSignals(_ context.Context, symbol, action, capability string, limit int) ([]wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []wheelstore.SignalRecord
	for i, signal := range f.signals {
		if (symbol != "" && signal.Symbol != symbol) || (action != "" && signal.Action != action) || (capability != "" && signal.CapabilityStatus != capability) {
			continue
		}
		signal.ID = int64(i + 1)
		out = append(out, signal)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeLLMStore) AppendSignal(_ context.Context, r wheelstore.SignalRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = append(f.signals, r)
	return int64(len(f.signals)), nil
}

func (f *fakeLLMStore) AppendAction(_ context.Context, r wheelstore.ActionRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, r)
	return int64(len(f.actions)), nil
}

func (f *fakeLLMStore) GetSignal(_ context.Context, id int64) (*wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id <= 0 || id > int64(len(f.signals)) {
		return nil, wheelstore.ErrNotFound
	}
	r := f.signals[id-1]
	r.ID = id
	return &r, nil
}

func (f *fakeLLMStore) LatestLLMReview(ctx context.Context, signalID int64) (*wheelstore.ActionRecord, error) {
	return f.LatestAction(ctx, signalID, "LLM_REVIEW")
}

func (f *fakeLLMStore) LatestAction(_ context.Context, signalID int64, action string) (*wheelstore.ActionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.actions) - 1; i >= 0; i-- {
		if f.actions[i].SignalID == signalID && f.actions[i].Action == action {
			r := f.actions[i]
			return &r, nil
		}
	}
	return nil, wheelstore.ErrNotFound
}

func (f *fakeLLMStore) HasAction(_ context.Context, signalID int64, action string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.actions {
		if a.SignalID == signalID && a.Action == action {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeLLMStore) QuerySignalsSince(_ context.Context, action string, afterID int64, limit int) ([]wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []wheelstore.SignalRecord
	for i, signal := range f.signals {
		id := int64(i + 1)
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

func (f *fakeLLMStore) MaxSignalID(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.signals)), nil
}

func (f *fakeLLMStore) Dismiss(_ context.Context, symbol string, date time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dismissed == nil {
		f.dismissed = map[string]bool{}
	}
	f.dismissed[symbol+"|"+date.UTC().Format("2006-01-02")] = true
	return nil
}

func (f *fakeLLMStore) IsDismissed(_ context.Context, symbol string, date time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dismissed[symbol+"|"+date.UTC().Format("2006-01-02")], nil
}

// stubReviewer always approves.
type stubReviewer struct{}

func (stubReviewer) Review(_ context.Context, _ llmreview.ReviewRequest) (llmreview.ReviewResult, error) {
	return llmreview.ReviewResult{Verdict: "APPROVE", Reasons: []string{"ok"}}, nil
}

// stubAccounter returns a fixed sim account snapshot so the audit gate gets
// cash/positions context (nil err = healthy account).
type stubAccounter struct {
	cash      float64
	positions []PositionJSON
	err       error
}

func (s *stubAccounter) Account(_ context.Context, env futu.Env, accID uint64) (AccountSnapshot, error) {
	if s.err != nil {
		return AccountSnapshot{}, s.err
	}
	return AccountSnapshot{
		Env:       futu.EnvName(env),
		AccID:     accID,
		Funds:     FundsJSON{AvailableCash: s.cash},
		Positions: s.positions,
	}, nil
}

// AccountForSymbol mirrors Account (symbol-based resolution is exercised at
// the internal/futu layer; here the stub just returns the same snapshot).
func (s *stubAccounter) AccountForSymbol(_ context.Context, env futu.Env, _ string) (AccountSnapshot, error) {
	return s.Account(context.Background(), env, 13477968)
}

type stubLLMSignalQuoter struct{}

func (stubLLMSignalQuoter) Quote(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"basic_qot_list":[{"cur_price":459}]}`), nil
}

func (stubLLMSignalQuoter) OptionQuotes(_ context.Context, symbols []string) (map[string]futu.OptionQuoteEx, error) {
	quotes := make(map[string]futu.OptionQuoteEx, len(symbols))
	for _, symbol := range symbols {
		quote := futu.OptionQuoteEx{Symbol: symbol, Bid: 11.45, Last: 11.45, Delta: -.47, ImpliedVol: .404, OpenInterest: 249, LotSize: 100}
		if symbol == "HK.TCH260821P450000" {
			quote.Bid, quote.Last, quote.Delta, quote.ImpliedVol, quote.OpenInterest = 8.5, 8.5, -.35, .4, 100
		}
		quotes[symbol] = quote
	}
	return quotes, nil
}

func postLLMSignal(t *testing.T, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	store := &fakeLLMStore{}
	h := LLMSignalHandler(store, stubReviewer{}, "deepseek-v4-pro", &stubAccounter{cash: 50000}, stubLLMSignalQuoter{})
	req := httptest.NewRequest(http.MethodPost, LLMSignalPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("bad response JSON: %v", err)
		}
	}
	return rec, resp
}

func TestLLMSignalOK(t *testing.T) {
	body := `{"symbol":"HK.00700","direction":"PUT","quantity":1,"contract":"HK.TCH260821P460000","strike":460,"current_price":459,"premium":11.45,"delta":-0.47,"iv":0.404,"open_interest":249,"reason":"卖 put 收取权利金"}`
	rec, resp := postLLMSignal(t, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if resp["signal_id"].(float64) != 1 {
		t.Errorf("signal_id = %v, want 1", resp["signal_id"])
	}
	if resp["llm_verdict"] != "APPROVE" || resp["approved"] != true {
		t.Errorf("verdict = %v/%v, want APPROVE/true", resp["llm_verdict"], resp["approved"])
	}
	if resp["contract"] != "HK.TCH260821P460000" {
		t.Errorf("contract = %v", resp["contract"])
	}
}

func TestLLMSignalSyntheticContract(t *testing.T) {
	// No contract → synthetic code from strike+expiry (HK.TCH260821P460000).
	body := `{"symbol":"HK.00700","direction":"PUT","quantity":1,"strike":450,"expiry":"2026-08-21T00:00:00Z","current_price":459,"premium":8.5,"delta":-0.35,"iv":0.4,"open_interest":100,"reason":"行权价低于现价并收取权利金"}`
	rec, resp := postLLMSignal(t, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if resp["contract"] != "HK.TCH260821P450000" {
		t.Errorf("contract = %v, want HK.TCH260821P450000", resp["contract"])
	}
}

func TestLLMSignalValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing symbol", `{"direction":"PUT","quantity":1}`},
		{"bad direction", `{"symbol":"HK.00700","direction":"HOLD","quantity":1}`},
		{"zero quantity", `{"symbol":"HK.00700","direction":"PUT","quantity":0}`},
		{"no contract no strike", `{"symbol":"HK.00700","direction":"PUT","quantity":1}`},
		{"bad contract", `{"symbol":"HK.00700","direction":"PUT","quantity":1,"contract":"HK.TCH260821X460000"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, _ := postLLMSignal(t, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestLLMSignalMethodNotAllowed(t *testing.T) {
	store := &fakeLLMStore{}
	h := LLMSignalHandler(store, stubReviewer{}, "", &stubAccounter{cash: 50000})
	req := httptest.NewRequest(http.MethodGet, LLMSignalPath, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// TestLLMSignalFailClosed: nil reviewer records REJECTED, response says REJECT.
func TestLLMSignalFailClosed(t *testing.T) {
	store := &fakeLLMStore{}
	h := LLMSignalHandler(store, nil, "", &stubAccounter{cash: 50000}, stubLLMSignalQuoter{})
	body := `{"symbol":"HK.00700","direction":"PUT","quantity":1,"contract":"HK.TCH260821P460000","current_price":459,"premium":11.45,"delta":-0.47,"iv":0.404,"open_interest":249,"reason":"行权价接近现价并收取权利金"}`
	req := httptest.NewRequest(http.MethodPost, LLMSignalPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad response JSON: %v", err)
	}
	if resp["llm_verdict"] != "REJECT" || resp["approved"] != false {
		t.Errorf("verdict = %v/%v, want REJECT/false", resp["llm_verdict"], resp["approved"])
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.actions) != 1 || store.actions[0].Action != "REJECTED" {
		t.Errorf("actions = %+v, want one REJECTED", store.actions)
	}
}
