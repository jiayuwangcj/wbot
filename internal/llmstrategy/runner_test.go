package llmstrategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/llmsignal"
	"github.com/jiayu/wbot/internal/watchlist"
	"github.com/jiayu/wbot/internal/wheelstore"
)

type wlFake struct{ items []watchlist.Item }

func (f wlFake) List(context.Context) ([]watchlist.Item, error) { return f.items, nil }

type dedupeFake struct {
	pending      bool
	orders       []wheelstore.PendingOrder
	calls        int
	pendingCalls int
}

func (f *dedupeFake) HasRecentUndisposedSignal(context.Context, string, time.Time) (bool, error) {
	f.calls++
	return f.pending, nil
}

func (f *dedupeFake) ListPendingOrders(context.Context, string) ([]wheelstore.PendingOrder, error) {
	f.pendingCalls++
	return f.orders, nil
}

type marketFake struct{ calls int }

func (f *marketFake) Snapshot(context.Context, string, map[string]any, time.Time) (Snapshot, error) {
	f.calls++
	cash := 100000.0
	return Snapshot{Symbol: "HK.00700", CurrentPrice: 459, CashAvailable: &cash, Options: []Option{{Contract: "HK.TCH260821P450000", Direction: "PUT", Strike: 450, Expiry: "2026-08-21T00:00:00Z", Premium: 8.5, Delta: -.35, IV: .4, OpenInterest: 100}}}, nil
}

type genFake struct {
	err   error
	calls int
	snap  Snapshot
}

func (f *genFake) Generate(_ context.Context, s Snapshot) (llmsignal.Decision, error) {
	f.calls++
	f.snap = s
	return llmsignal.Decision{Direction: "PUT", Quantity: 1, Contract: "HK.TCH260821P450000", Strike: 450, Expiry: "2026-08-21T00:00:00Z", Premium: 8.5, Delta: -.35, IV: .4, OpenInterest: 100, Reason: "specific reason"}, f.err
}

type submitFake struct {
	submits, rejections int
	rejectEmptyOptions  bool
	account             llmsignal.Context
}

func (f *submitFake) Submit(_ context.Context, _ llmsignal.Decision, account llmsignal.Context, _ llmsignal.Policy) (llmsignal.Result, error) {
	f.submits++
	f.account = account
	if f.rejectEmptyOptions && len(account.ObservedOptions) == 0 {
		return llmsignal.Result{}, llmsignal.ErrRejected
	}
	return llmsignal.Result{}, nil
}
func (f *submitFake) RecordGenerationRejection(context.Context, string, error) (int64, error) {
	f.rejections++
	return 1, nil
}

func TestRunOnceDBDedupeSkipsGeneration(t *testing.T) {
	d := &dedupeFake{pending: true}
	m := &marketFake{}
	g := &genFake{}
	s := &submitFake{}
	r := Runner{Watchlist: wlFake{[]watchlist.Item{{Symbol: "HK.00700", Strategy: "llm"}}}, Dedupe: d, Market: m, Generator: g, Submitter: s, Now: func() time.Time { return time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC) }}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.calls != 1 || m.calls != 0 || g.calls != 0 || s.submits != 0 {
		t.Fatalf("calls=%d/%d/%d/%d", d.calls, m.calls, g.calls, s.submits)
	}
}
func TestRunOncePassesPendingOrdersToGeneratorAndSubmitter(t *testing.T) {
	// A confirmed-but-unfilled order no longer skips the tick: the generator
	// and the review gate must see the open exposure and judge whether a new
	// decision is still reasonable (老板指令 2026-08-13: 未成交订单要传入策略
	// 与审核综合考虑;确定性校验仍拒绝同合约重复)。
	d := &dedupeFake{orders: []wheelstore.PendingOrder{{SignalID: 701, OrderID: "12345", Direction: "PUT", Quantity: 1, Contract: "HK.TCH260821P450000", Strike: 450, Expiry: "2026-08-21T00:00:00Z", Premium: 8.5}}}
	m := &marketFake{}
	g := &genFake{}
	s := &submitFake{}
	r := Runner{Watchlist: wlFake{[]watchlist.Item{{Symbol: "HK.00700", Strategy: "llm"}}}, Dedupe: d, Market: m, Generator: g, Submitter: s}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.pendingCalls != 1 || m.calls != 1 || g.calls != 1 || s.submits != 1 {
		t.Fatalf("calls=%d/%d/%d/%d; pending order must be passed, not skipped", d.pendingCalls, m.calls, g.calls, s.submits)
	}
	if len(g.snap.PendingOrders) != 1 || g.snap.PendingOrders[0].OrderID != "12345" {
		t.Fatalf("generator snapshot pending orders = %+v", g.snap.PendingOrders)
	}
	if len(s.account.PendingOrders) != 1 || s.account.PendingOrders[0].Contract != "HK.TCH260821P450000" {
		t.Fatalf("submitter account pending orders = %+v", s.account.PendingOrders)
	}
}

func TestRunOnceGenerationFailureRecordedAndRetriable(t *testing.T) {
	d := &dedupeFake{}
	m := &marketFake{}
	g := &genFake{err: errors.New("bad JSON")}
	s := &submitFake{}
	r := Runner{Watchlist: wlFake{[]watchlist.Item{{Symbol: "HK.00700", Strategy: "llm"}}}, Dedupe: d, Market: m, Generator: g, Submitter: s}
	if err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("want pass error")
	}
	if s.rejections != 1 || s.submits != 0 {
		t.Fatalf("rejections/submits=%d/%d", s.rejections, s.submits)
	}
}

type emptyOptionsMarket struct{}

func (emptyOptionsMarket) Snapshot(context.Context, string, map[string]any, time.Time) (Snapshot, error) {
	cash := 100000.0
	return Snapshot{Symbol: "HK.00700", CurrentPrice: 459, CashAvailable: &cash}, nil
}

func TestRunOnceRejectsFabricatedOptionWhenSnapshotOptionsEmpty(t *testing.T) {
	s := &submitFake{rejectEmptyOptions: true}
	r := Runner{
		Watchlist: wlFake{[]watchlist.Item{{Symbol: "HK.00700", Strategy: "llm"}}},
		Dedupe:    &dedupeFake{}, Market: emptyOptionsMarket{}, Generator: &genFake{}, Submitter: s,
	}

	if err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("want pass error")
	}
	// 价格注入发生在 Submit 之前:合约不在快照 → 注入层拒绝,不再调用 Submit。
	if s.submits != 0 || s.rejections != 1 {
		t.Fatalf("submits/rejections=%d/%d", s.submits, s.rejections)
	}
}

type tickSubmit struct{ calls chan struct{} }

func (s tickSubmit) Submit(context.Context, llmsignal.Decision, llmsignal.Context, llmsignal.Policy) (llmsignal.Result, error) {
	s.calls <- struct{}{}
	return llmsignal.Result{}, nil
}
func (s tickSubmit) RecordGenerationRejection(context.Context, string, error) (int64, error) {
	return 0, nil
}

func TestRunTriggersImmediatelyAndOnInterval(t *testing.T) {
	calls := make(chan struct{}, 3)
	r := Runner{Watchlist: wlFake{[]watchlist.Item{{Symbol: "HK.00700", Strategy: "llm"}}}, Dedupe: &dedupeFake{}, Market: &marketFake{}, Generator: &genFake{}, Submitter: tickSubmit{calls: calls}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, 5*time.Millisecond) }()
	for i := 0; i < 2; i++ {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatal("scheduler did not trigger")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error=%v", err)
	}
}

func TestParseDecisionJSONStripsFences(t *testing.T) {
	plain, err := parseDecisionJSON(`{"symbol":"HK.00700","direction":"PUT","quantity":1}`)
	if err != nil || plain.Direction != "PUT" {
		t.Fatalf("plain: %+v err=%v", plain, err)
	}
	fenced, err := parseDecisionJSON("```json\n{\"symbol\":\"HK.00700\",\"direction\":\"PUT\",\"quantity\":1}\n```")
	if err != nil || fenced.Direction != "PUT" {
		t.Fatalf("fenced: %+v err=%v", fenced, err)
	}
	if _, err := parseDecisionJSON("[{\"direction\":\"PUT\"}]"); err == nil {
		t.Fatal("array content must still reject")
	}
	if _, err := parseDecisionJSON(`{"direction":"PUT"} trailing`); err == nil {
		t.Fatal("trailing content must still reject")
	}
}

func TestClientUsesDeepseekV4Flash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != Model {
			t.Errorf("model=%v", body["model"])
		}
		if r.Header.Get("Authorization") != "Bearer fake-key" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"symbol\":\"HK.00700\",\"direction\":\"PUT\",\"quantity\":1}"}}]}`))
	}))
	defer srv.Close()
	client, err := NewClient(srv.URL, "fake-key")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.Generate(context.Background(), Snapshot{Symbol: "HK.00700"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Direction != "PUT" || decision.Quantity != 1 {
		t.Fatalf("decision=%+v", decision)
	}
}

type toolExecutorFake struct {
	calls []string
	err   error
}

func (f *toolExecutorFake) result(call string) (any, error) {
	f.calls = append(f.calls, call)
	if f.err != nil {
		return nil, f.err
	}
	return map[string]any{"symbol": "HK.00700", "current_price": 459.0}, nil
}
func (f *toolExecutorFake) Quote(_ context.Context, symbol string, _ Snapshot) (any, error) {
	return f.result("quote:" + symbol)
}
func (f *toolExecutorFake) OptionChain(_ context.Context, symbol string, minDTE, maxDTE, maxStrikes int, _ Snapshot) (any, error) {
	return f.result(fmt.Sprintf("option_chain:%s:%d:%d:%d", symbol, minDTE, maxDTE, maxStrikes))
}
func (f *toolExecutorFake) OptionQuote(_ context.Context, contract string, _ Snapshot) (any, error) {
	return f.result("option_quote:" + contract)
}
func (f *toolExecutorFake) Account(_ context.Context, symbol string, _ Snapshot) (any, error) {
	return f.result("account:" + symbol)
}

func TestClientAgentToolCallThenDecision(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Tools    []map[string]any `json:"tools"`
			Messages []agentMessage   `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Tools) != 4 {
			t.Errorf("tools=%d, want 4", len(body.Tools))
		}
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"quote","arguments":"{\"symbol\":\"HK.00700\"}"}}]}}]}`))
			return
		}
		last := body.Messages[len(body.Messages)-1]
		if last.Role != "tool" || last.ToolCallID != "call-1" || last.Name != "quote" || !strings.Contains(last.Content, `"ok":true`) {
			t.Errorf("tool message=%+v", last)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"symbol\":\"HK.00700\",\"direction\":\"PUT\",\"quantity\":1,\"contract\":\"HK.TCH260821P450000\",\"reason\":\"quote checked\",\"notes\":\"\"}"}}]}`))
	}))
	defer srv.Close()
	executor := &toolExecutorFake{}
	client, err := NewClient(srv.URL, "fake-key", WithToolExecutor(executor))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.Generate(context.Background(), Snapshot{Symbol: "HK.00700"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(executor.calls) != 1 || decision.Contract != "HK.TCH260821P450000" {
		t.Fatalf("requests=%d tools=%v decision=%+v", requests, executor.calls, decision)
	}
}

func TestClientAgentRoundLimit(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"again","type":"function","function":{"name":"quote","arguments":"{\"symbol\":\"HK.00700\"}"}}]}}]}`))
	}))
	defer srv.Close()
	client, err := NewClient(srv.URL, "fake-key")
	if err != nil {
		t.Fatal(err)
	}
	client.maxRounds = 2
	_, err = client.Generate(context.Background(), Snapshot{Symbol: "HK.00700"})
	if err == nil || !strings.Contains(err.Error(), "exceeded 2 rounds") {
		t.Fatalf("error=%v", err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestFilterByDTE(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	opts := []Option{
		{Contract: "A", Expiry: "2026-08-14T00:00:00Z"}, // DTE 2
		{Contract: "B", Expiry: "2026-08-20T00:00:00Z"}, // DTE 8
		{Contract: "C", Expiry: "2026-08-12T00:00:00Z"}, // 已过期
		{Contract: "D", Expiry: "not-a-date"},           // 无法解析
	}
	if got := filterByDTE(opts, 0, 0, now); len(got) != 4 {
		t.Fatalf("no bounds: got %d, want 4", len(got))
	}
	got := filterByDTE(opts, 5, 10, now)
	if len(got) != 1 || got[0].Contract != "B" {
		t.Fatalf("min=5 max=10: got %+v, want only B", got)
	}
	got = filterByDTE(opts, 0, 5, now)
	if len(got) != 1 || got[0].Contract != "A" {
		t.Fatalf("max=5: got %+v, want only A", got)
	}
	if got := filterByDTE(opts, 3, 0, now); len(got) != 1 || got[0].Contract != "B" {
		t.Fatalf("min=3: got %+v, want only B", got)
	}
}

func TestSnapshotOptionChainFiltersExpiredByDTE(t *testing.T) {
	s := Snapshot{
		Symbol: "HK.00700",
		Options: []Option{
			{Contract: "HK.TCH260812P450000", Expiry: "2026-08-12T00:00:00Z"}, // 已过期
			{Contract: "HK.TCH260821P450000", Expiry: "2026-08-21T00:00:00Z"}, // DTE 9
		},
	}
	got, err := (snapshotToolExecutor{}).OptionChain(context.Background(), "HK.00700", 5, 10, 0, s)
	if err != nil {
		t.Fatal(err)
	}
	opts, ok := got.([]Option)
	if !ok || len(opts) != 1 || opts[0].Contract != "HK.TCH260821P450000" {
		t.Fatalf("chain=%+v, want only the live contract", got)
	}
}

func TestClientAgentFeedsToolErrorBackAndCanDecide(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-error","type":"function","function":{"name":"account","arguments":"{\"symbol\":\"HK.00700\"}"}}]}}]}`))
			return
		}
		var body struct {
			Messages []agentMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		last := body.Messages[len(body.Messages)-1]
		if last.Role != "tool" || !strings.Contains(last.Content, `"ok":false`) || !strings.Contains(last.Content, "account unavailable") {
			t.Errorf("tool error message=%+v", last)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"symbol\":\"HK.00700\",\"direction\":\"BUY\",\"quantity\":0,\"reason\":\"account unavailable\",\"notes\":\"fail closed\"}"}}]}`))
	}))
	defer srv.Close()
	executor := &toolExecutorFake{err: errors.New("account unavailable")}
	client, err := NewClient(srv.URL, "fake-key", WithToolExecutor(executor))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.Generate(context.Background(), Snapshot{Symbol: "HK.00700"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Quantity != 0 || decision.Notes != "fail closed" {
		t.Fatalf("decision=%+v", decision)
	}
}
