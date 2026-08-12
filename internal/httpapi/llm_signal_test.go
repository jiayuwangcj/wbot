package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// fakeLLMStore records appended signals/actions for the endpoint test.
type fakeLLMStore struct {
	mu      sync.Mutex
	signals []wheelstore.SignalRecord
	actions []wheelstore.ActionRecord
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

// stubReviewer always approves.
type stubReviewer struct{}

func (stubReviewer) Review(_ context.Context, _ llmreview.ReviewRequest) (llmreview.ReviewResult, error) {
	return llmreview.ReviewResult{Verdict: "APPROVE", Reasons: []string{"ok"}}, nil
}

func postLLMSignal(t *testing.T, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	store := &fakeLLMStore{}
	h := LLMSignalHandler(store, stubReviewer{}, "deepseek-v4-pro")
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
	body := `{"symbol":"HK.00700","direction":"CALL","quantity":2,"strike":470,"expiry":"2026-08-21T00:00:00Z","current_price":459}`
	rec, resp := postLLMSignal(t, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if resp["contract"] != "HK.TCH260821C470000" {
		t.Errorf("contract = %v, want HK.TCH260821C470000", resp["contract"])
	}
}

func TestLLMSignalValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing symbol", `{"direction":"PUT","quantity":1}`},
		{"bad direction", `{"symbol":"HK.00700","direction":"SELL","quantity":1}`},
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
	h := LLMSignalHandler(store, stubReviewer{}, "")
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
	h := LLMSignalHandler(store, nil, "")
	body := `{"symbol":"HK.00700","direction":"PUT","quantity":1,"contract":"HK.TCH260821P460000"}`
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
