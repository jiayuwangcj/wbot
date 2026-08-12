package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/telegram"
	"github.com/jiayu/wbot/internal/wheelstore"
)

const tgTestToken = "bottest-token"

// fakeTGServer records answerCallbackQuery toasts (the scheduler's reply path).
type fakeTGServer struct {
	mu      sync.Mutex
	answers []map[string]any
	sends   []map[string]any
}

func (f *fakeTGServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/bot"+tgTestToken+"/")
	switch path {
	case "answerCallbackQuery":
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		f.answers = append(f.answers, p)
		f.mu.Unlock()
		w.Write([]byte(`{"ok":true}`))
	case "sendMessage":
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		f.sends = append(f.sends, p)
		f.mu.Unlock()
		w.Write([]byte(`{"ok":true}`))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeTGServer) lastSend(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) == 0 {
		t.Fatal("no sendMessage received")
	}
	return f.sends[len(f.sends)-1]
}

func (f *fakeTGServer) lastToast(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.answers) == 0 {
		t.Fatal("no answerCallbackQuery received")
	}
	text, _ := f.answers[len(f.answers)-1]["text"].(string)
	return text
}

// fakeTGStore is an in-memory wheelTelegramStore for handler tests.
type fakeTGStore struct {
	mu            sync.Mutex
	signals       map[int64]*wheelstore.SignalRecord
	reviews       map[int64]*wheelstore.ActionRecord
	dismissed     map[string]bool
	appended      []wheelstore.ActionRecord
	maxID         int64
	maxIDFailures int
	querySince    []wheelstore.SignalRecord
	queryCalls    []int64
}

func newFakeTGStore() *fakeTGStore {
	return &fakeTGStore{
		signals:   map[int64]*wheelstore.SignalRecord{},
		reviews:   map[int64]*wheelstore.ActionRecord{},
		dismissed: map[string]bool{},
	}
}

func (f *fakeTGStore) GetSignal(_ context.Context, id int64) (*wheelstore.SignalRecord, error) {
	sig, ok := f.signals[id]
	if !ok {
		return nil, wheelstore.ErrNotFound
	}
	return sig, nil
}

func (f *fakeTGStore) LatestLLMReview(_ context.Context, signalID int64) (*wheelstore.ActionRecord, error) {
	r, ok := f.reviews[signalID]
	if !ok {
		return nil, wheelstore.ErrNotFound
	}
	return r, nil
}

func (f *fakeTGStore) AppendAction(_ context.Context, r wheelstore.ActionRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appended = append(f.appended, r)
	return int64(len(f.appended)), nil
}

func (f *fakeTGStore) HasAction(_ context.Context, signalID int64, action string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.appended {
		if a.SignalID == signalID && a.Action == action {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeTGStore) QuerySignalsSince(_ context.Context, _ string, afterID int64, _ int) ([]wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryCalls = append(f.queryCalls, afterID)
	return append([]wheelstore.SignalRecord(nil), f.querySince...), nil
}

func (f *fakeTGStore) MaxSignalID(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.maxIDFailures > 0 {
		f.maxIDFailures--
		return 0, errors.New("max id: db blip")
	}
	return f.maxID, nil
}

func (f *fakeTGStore) Dismiss(_ context.Context, symbol string, date time.Time) error {
	f.dismissed[symbol+"|"+date.Format("2006-01-02")] = true
	return nil
}

func (f *fakeTGStore) IsDismissed(_ context.Context, symbol string, date time.Time) (bool, error) {
	return f.dismissed[symbol+"|"+date.Format("2006-01-02")], nil
}

func (f *fakeTGStore) lastAppended(t *testing.T) wheelstore.ActionRecord {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.appended) == 0 {
		t.Fatal("no action appended")
	}
	return f.appended[len(f.appended)-1]
}

type fakePlacer struct {
	err       error
	orderIDEx string
	orderID   uint64
	gotSymbol string
	gotSide   string
	gotQty    float64
	calls     int
}

func (p *fakePlacer) PlaceOrder(_ context.Context, symbol, side string, qty float64) (string, uint64, error) {
	p.calls++
	p.gotSymbol, p.gotSide, p.gotQty = symbol, side, qty
	return p.orderIDEx, p.orderID, p.err
}

func startFakeTG(t *testing.T) (*fakeTGServer, *httptest.Server) {
	t.Helper()
	fake := &fakeTGServer{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func newTestScheduler(t *testing.T, server *httptest.Server, store *fakeTGStore, placer *fakePlacer, chatIDs map[int64]bool, now time.Time) *telegramScheduler {
	t.Helper()
	tg, err := telegram.New(tgTestToken, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	s := newTelegramScheduler(tg, store, placer, chatIDs)
	s.now = func() time.Time { return now }
	s.logf = func(string, ...any) {}
	return s
}

// signalFixture is an ALERT with a full first candidate (as the runner
// persists it: opaque JSON maps).
func signalFixture(id int64, symbol string, created time.Time) *wheelstore.SignalRecord {
	return &wheelstore.SignalRecord{
		ID: id, Symbol: symbol, Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY",
		Inventory: wheelstore.InventorySnapshot{
			CurrentPrice: f64ptr(248.5), ActualInventory: f64ptr(5000),
			TargetInventory: f64ptr(4700), InventoryGap: f64ptr(-300),
		},
		Candidates: []map[string]any{{
			"direction": "PUT",
			"quantity":  2,
			"quote": map[string]any{
				"symbol": "US.AAPL260815C250000", "option_type": "CALL", "strike": 250.0,
				"expiry": "2026-08-15T00:00:00Z", "bid": 3.2, "ask": 3.35, "last": 3.28, "delta": 0.42,
				"implied_vol": 0.25, "open_interest": 1234.0,
			},
		}},
		Reason: "gap", CreatedAt: created,
	}
}

func f64ptr(v float64) *float64 { return &v }

func approvedReview() *wheelstore.ActionRecord {
	return &wheelstore.ActionRecord{Action: "LLM_REVIEW", Actor: "llm:test", Details: map[string]any{"verdict": "APPROVE"}}
}

func callback(from int64, data string) *telegram.CallbackQuery {
	return &telegram.CallbackQuery{ID: "cb-" + data, From: telegram.User{ID: from}, Data: data}
}

func TestCallbackUnknownUserRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, time.Now())

	s.handleCallback(context.Background(), callback(99, "wheel:1:yes"))
	if toast := fake.lastToast(t); !strings.Contains(toast, "未授权") {
		t.Fatalf("toast = %q; want 未授权", toast)
	}
	if len(store.appended) != 0 {
		t.Fatalf("unknown user triggered %d store writes", len(store.appended))
	}
}

func TestCallbackYesPlacesSimOrder(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{orderID: 12345, orderIDEx: "ord-12345"}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	if placer.calls != 1 {
		t.Fatalf("PlaceOrder calls = %d; want 1", placer.calls)
	}
	if placer.gotSymbol != "US.AAPL260815C250000" || placer.gotSide != "sell" || placer.gotQty != 2 {
		t.Fatalf("order = %s %s %v", placer.gotSymbol, placer.gotSide, placer.gotQty)
	}
	act := store.lastAppended(t)
	if act.Action != "CONFIRM" || act.Actor != "telegram:42" {
		t.Fatalf("action = %+v", act)
	}
	if act.Details["order_id"] != uint64(12345) || act.Details["symbol"] != "US.AAPL260815C250000" {
		t.Fatalf("details = %+v", act.Details)
	}
	if toast := fake.lastToast(t); !strings.Contains(toast, "12345") {
		t.Fatalf("toast = %q; want order number", toast)
	}
}

func TestCallbackYesRealEnvRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{err: errLiveEnvNotAllowed}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	if placer.calls != 1 {
		t.Fatalf("PlaceOrder calls = %d; want 1 (guard lives in the placer)", placer.calls)
	}
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "live env not allowed" {
		t.Fatalf("action = %+v; want REJECTED with live-env reason", act)
	}
	if toast := fake.lastToast(t); !strings.Contains(toast, "实盘下单不允许") {
		t.Fatalf("toast = %q; want 实盘下单不允许", toast)
	}
}

func TestCallbackYesExpiredRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now.Add(-11*time.Minute))
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	if placer.calls != 0 {
		t.Fatalf("expired signal placed an order; calls = %d", placer.calls)
	}
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "signal expired" {
		t.Fatalf("action = %+v; want REJECTED with expired reason", act)
	}
	if toast := fake.lastToast(t); !strings.Contains(toast, "已过期") {
		t.Fatalf("toast = %q; want 已过期", toast)
	}
}

func TestCallbackYesReviewNotApprovedRejected(t *testing.T) {
	for name, review := range map[string]*wheelstore.ActionRecord{
		"reject": {Action: "LLM_REVIEW", Actor: "llm:test", Details: map[string]any{"verdict": "REJECT"}},
		"none":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			fake, server := startFakeTG(t)
			now := time.Now()
			store := newFakeTGStore()
			store.signals[7] = signalFixture(7, "US.AAPL", now)
			if review != nil {
				store.reviews[7] = review
			}
			placer := &fakePlacer{}
			s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

			s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
			if placer.calls != 0 {
				t.Fatalf("unapproved signal placed an order; calls = %d", placer.calls)
			}
			act := store.lastAppended(t)
			if act.Action != "REJECTED" || !strings.Contains(act.Note, "llm review") {
				t.Fatalf("action = %+v; want REJECTED with review reason", act)
			}
			if toast := fake.lastToast(t); !strings.Contains(toast, "审核未通过") {
				t.Fatalf("toast = %q; want 审核未通过", toast)
			}
		})
	}
}

func TestCallbackYesMissingSignalRejected(t *testing.T) {
	_, server := startFakeTG(t)
	store := newFakeTGStore() // no signal 7
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, time.Now())

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "signal not found" {
		t.Fatalf("action = %+v", act)
	}
}

func TestCallbackNoRecordsAndAnswers(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, time.Now())

	s.handleCallback(context.Background(), callback(42, "wheel:9:no"))
	act := store.lastAppended(t)
	if act.Action != "NO" || act.Actor != "telegram:42" || act.SignalID != 9 {
		t.Fatalf("action = %+v", act)
	}
	if toast := fake.lastToast(t); !strings.Contains(toast, "继续等待") {
		t.Fatalf("toast = %q", toast)
	}
}

func TestCallbackDismissSilencesToday(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Date(2026, 8, 11, 14, 30, 0, 0, time.FixedZone("UTC+8", 8*3600))
	store := newFakeTGStore()
	store.signals[5] = signalFixture(5, "US.AAPL", now)
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:5:dismiss"))
	// utcDate(now): 14:30 UTC+8 = 06:30 UTC on 2026-08-11.
	wantKey := "US.AAPL|2026-08-11"
	if !store.dismissed[wantKey] {
		t.Fatalf("dismissed = %v; want %q", store.dismissed, wantKey)
	}
	if len(store.appended) != 0 {
		t.Fatalf("dismiss wrote %d actions; want 0", len(store.appended))
	}
	if toast := fake.lastToast(t); !strings.Contains(toast, "今日不再提醒") {
		t.Fatalf("toast = %q", toast)
	}
}

func TestCallbackMalformedDataRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, time.Now())

	s.handleCallback(context.Background(), callback(42, "bogus"))
	if len(store.appended) != 0 {
		t.Fatalf("malformed data wrote %d actions", len(store.appended))
	}
	if toast := fake.lastToast(t); !strings.Contains(toast, "无效") {
		t.Fatalf("toast = %q", toast)
	}
}

func TestAlertMessageFormat(t *testing.T) {
	created := time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC)
	sig := signalFixture(7, "US.AAPL", created)
	text, err := alertMessage(sig, "IV 高位", "风险 < 可控")
	if err != nil {
		t.Fatal(err)
	}
	want := `<b>📌 US.AAPL · 卖出认沽 (SELL PUT)</b>
━━━━━━━━━━━━━━━━━━━━
🎯 <b>订单</b>
候选      <b><code>US.AAPL260815C250000</code></b>
行权      <b><code>250.00</code></b>
到期      <code>2026-08-15</code> (剩 4 天)
数量      <b><code>2</code></b> 张
限价      <b><code>3.28</code></b> (估算)
┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
📊 <b>标的当前</b>
正股现价  <b><code>248.50</code></b>
bid/ask   <code>3.20</code>/<code>3.35</code>
希腊      Δ <code>0.42</code> · IV <code>0.25</code> · OI <code>1,234</code>
┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
🧭 <b>持仓与策略参数</b>
正股持仓  <code>5,000</code> 股
CALL 持仓 <code>-</code> 张 · <code>-</code>
PUT 持仓  <code>-</code> 张
目标持仓  <code>4,700</code> 股
库存缺口  <b><code>-300</code></b> 股
┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
🧠 <b>下单原因</b> · LLM 审核 <b>✅ APPROVE</b>
• IV 高位
• 风险 &lt; 可控
━━━━━━━━━━━━━━━━━━━━
信号 #7 · 配置 v1 · 08-11 15:30`
	if text != want {
		t.Fatalf("alert message mismatch\n--- got ---\n%s\n--- want ---\n%s", text, want)
	}
}

func TestAlertMessageMissingLastUsesDash(t *testing.T) {
	sig := signalFixture(7, "US.AAPL", time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC))
	quote := sig.Candidates[0]["quote"].(map[string]any)
	delete(quote, "last")
	text, err := alertMessage(sig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "限价      <b><code>-</code></b> (估算)") {
		t.Fatalf("missing last did not use dash:\n%s", text)
	}
}

func TestPushSignalRendersReviewReasonsAndV20Buttons(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(7, "US.AAPL", now)
	store.reviews[7] = &wheelstore.ActionRecord{Details: map[string]any{
		"verdict": "APPROVE", "reasons": []any{"reason one", "reason two"},
	}}
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, now)

	s.pushSignal(context.Background(), *sig)
	payload := fake.lastSend(t)
	text, _ := payload["text"].(string)
	if !strings.Contains(text, "• reason one\n• reason two") {
		t.Fatalf("message reasons missing: %s", text)
	}
	markup, ok := payload["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("reply_markup = %#v", payload["reply_markup"])
	}
	rows, _ := markup["inline_keyboard"].([]any)
	if len(rows) != 1 {
		t.Fatalf("inline_keyboard = %#v", markup["inline_keyboard"])
	}
	buttons, _ := rows[0].([]any)
	wantTexts := []string{"✅ 下单", "❌ 拒绝", "⚠️ Dismiss"}
	wantData := []string{"wheel:7:yes", "wheel:7:no", "wheel:7:dismiss"}
	for i := range wantTexts {
		button, _ := buttons[i].(map[string]any)
		if button["text"] != wantTexts[i] || button["callback_data"] != wantData[i] {
			t.Fatalf("button %d = %#v", i, button)
		}
	}
}

func TestPushSignalRetriesWhenReviewNotYetRecorded(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC)
	store := newFakeTGStore()
	// No review yet: the POST handler appends the signal, then runs the LLM
	// gate and records the disposition — the first push pass can race it.
	sig := signalFixture(9, "US.AAPL", now)
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, now)

	if retry := s.pushSignal(context.Background(), *sig); !retry {
		t.Fatal("pushSignal without a recorded review must retry")
	}
	if len(fake.sends) != 0 {
		t.Fatalf("signal pushed without review; sends = %d", len(fake.sends))
	}

	// The review lands on the next pass → pushed, not retried.
	store.reviews[9] = approvedReview()
	if retry := s.pushSignal(context.Background(), *sig); retry {
		t.Fatal("pushSignal with APPROVE review must not retry")
	}
	if len(fake.sends) != 1 {
		t.Fatalf("sends = %d, want 1", len(fake.sends))
	}
}

func TestPushSignalSkipsRejectedReview(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(10, "US.AAPL", now)
	store.appended = append(store.appended, wheelstore.ActionRecord{SignalID: 10, Action: "REJECTED", Actor: "llm:test"})
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, now)

	if retry := s.pushSignal(context.Background(), *sig); retry {
		t.Fatal("pushSignal with a REJECTED review must skip permanently")
	}
	if len(fake.sends) != 0 {
		t.Fatalf("rejected signal pushed; sends = %d", len(fake.sends))
	}
}

func TestPushSignalSkipsStaleMissingReview(t *testing.T) {
	fake, server := startFakeTG(t)
	// Signal older than the freshness window with no review: the review can
	// never land now (the POST would have recorded it), skip permanently.
	now := time.Now()
	old := now.Add(-2 * signalFreshWindow)
	store := newFakeTGStore()
	sig := signalFixture(11, "US.AAPL", old)
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, now)

	if retry := s.pushSignal(context.Background(), *sig); retry {
		t.Fatal("stale signal without review must skip permanently")
	}
	if len(fake.sends) != 0 {
		t.Fatalf("stale signal pushed; sends = %d", len(fake.sends))
	}
}

func TestFirstCandidateOrderFacts(t *testing.T) {
	sig := signalFixture(1, "US.AAPL", time.Now())
	c, err := firstCandidate(sig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != "US.AAPL260815C250000" || c.Side != "sell" || c.Quantity != 2 || c.Direction != "PUT" {
		t.Fatalf("candidate = %+v", c)
	}
	if _, err := firstCandidate(&wheelstore.SignalRecord{}); err == nil {
		t.Fatal("candidate-less signal accepted")
	}
	// Quantity defaults to 1 when the candidate omits it.
	noQty := signalFixture(1, "US.AAPL", time.Now())
	noQty.Candidates[0]["quantity"] = 0
	c, err = firstCandidate(noQty)
	if err != nil || c.Quantity != 1 {
		t.Fatalf("default qty: c=%+v err=%v", c, err)
	}
}

func TestParseCallbackData(t *testing.T) {
	for _, tc := range []struct {
		data   string
		ok     bool
		id     int64
		action string
	}{
		{"wheel:42:yes", true, 42, "yes"},
		{"wheel:42:no", true, 42, "no"},
		{"wheel:42:dismiss", true, 42, "dismiss"},
		{"wheel:0:yes", false, 0, ""},
		{"wheel:x:yes", false, 0, ""},
		{"bogus", false, 0, ""},
		{"wheel:42:maybe", false, 0, ""},
	} {
		id, action, err := parseCallbackData(tc.data)
		if tc.ok && (err != nil || id != tc.id || action != tc.action) {
			t.Fatalf("%s: id=%d action=%s err=%v", tc.data, id, action, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%s accepted", tc.data)
		}
	}
}

func TestUtcDateBoundary(t *testing.T) {
	// A late-evening local instant still lands on the UTC calendar day.
	in := time.Date(2026, 8, 11, 23, 59, 0, 0, time.FixedZone("UTC+8", 8*3600))
	if got := utcDate(in); got.Format("2006-01-02") != "2026-08-11" {
		t.Fatalf("utcDate = %v; want 2026-08-11", got)
	}
}

func TestVerdictOf(t *testing.T) {
	if verdictOf(nil) != "" {
		t.Fatal("verdictOf(nil) != empty")
	}
	if verdictOf(&wheelstore.ActionRecord{Details: map[string]any{"verdict": "approve"}}) != "APPROVE" {
		t.Fatal("verdictOf did not normalize")
	}
	if verdictOf(&wheelstore.ActionRecord{Details: map[string]any{"verdict": 42}}) != "" {
		t.Fatal("verdictOf non-string detail accepted")
	}
}

func TestRunPushSeedsCursorFromMaxSignalID(t *testing.T) {
	_, server := startFakeTG(t)
	store := newFakeTGStore()
	store.maxIDFailures = 2 // DB blip at startup
	store.maxID = 7
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.runPush(ctx, 5*time.Millisecond) }()
	deadline := time.After(5 * time.Second)
	for {
		store.mu.Lock()
		n := len(store.queryCalls)
		store.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("push loop never polled")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.queryCalls) == 0 || store.queryCalls[0] != 7 {
		t.Fatalf("first poll cursor = %v; want 7 (must retry MaxSignalID, never poll with 0)", store.queryCalls)
	}
	for _, c := range store.queryCalls {
		if c == 0 {
			t.Fatal("polled with cursor 0 (would replay history after DB recovery)")
		}
	}
}

func TestCallbackYesDoubleConfirmRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{orderID: 12345}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	first := callback(42, "wheel:7:yes")
	second := callback(42, "wheel:7:yes")
	s.handleCallback(context.Background(), first)
	s.handleCallback(context.Background(), second)
	if placer.calls != 1 {
		t.Fatalf("PlaceOrder calls = %d; want 1 (second press must be refused)", placer.calls)
	}
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "already confirmed" {
		t.Fatalf("last action = %+v; want REJECTED already confirmed", act)
	}
	if toast := fake.lastToast(t); !strings.Contains(toast, "请勿重复确认") {
		t.Fatalf("toast = %q; want 请勿重复确认", toast)
	}
}
