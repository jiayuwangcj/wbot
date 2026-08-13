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
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
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

// fakeTGStore is an in-memory SignalRepository for handler tests.
type fakeTGStore struct {
	mu             sync.Mutex
	signals        map[int64]*wheelstore.SignalRecord
	reviews        map[int64]*wheelstore.ActionRecord
	dismissed      map[string]bool
	appended       []wheelstore.ActionRecord
	maxID          int64
	maxIDFailures  int
	querySince     []wheelstore.SignalRecord
	queryCalls     []int64
	claims         map[int64]bool
	appendFailures int
}

func newFakeTGStore() *fakeTGStore {
	return &fakeTGStore{
		signals:   map[int64]*wheelstore.SignalRecord{},
		reviews:   map[int64]*wheelstore.ActionRecord{},
		dismissed: map[string]bool{},
		claims:    map[int64]bool{},
	}
}

func (f *fakeTGStore) ClaimOrder(_ context.Context, signalID int64, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claims[signalID] {
		return false, nil
	}
	f.claims[signalID] = true
	return true, nil
}

func (f *fakeTGStore) CompleteOrderClaim(_ context.Context, _ int64, _ uint64, _ string, _ map[string]any) error {
	return nil
}

func (f *fakeTGStore) GetSignal(_ context.Context, id int64) (*wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sig, ok := f.signals[id]
	if !ok {
		return nil, wheelstore.ErrNotFound
	}
	return sig, nil
}

func (f *fakeTGStore) LatestConfig(context.Context, string) (*wheelstore.ConfigRecord, error) {
	return nil, wheelstore.ErrNotFound
}

func (f *fakeTGStore) ListSignals(_ context.Context, symbol, action, capability string, limit int) ([]wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []wheelstore.SignalRecord
	for _, signal := range f.signals {
		if (symbol != "" && signal.Symbol != symbol) || (action != "" && signal.Action != action) || (capability != "" && signal.CapabilityStatus != capability) {
			continue
		}
		out = append(out, *signal)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeTGStore) AppendSignal(_ context.Context, r wheelstore.SignalRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ID <= 0 {
		r.ID = f.maxID + 1
	}
	if r.ID > f.maxID {
		f.maxID = r.ID
	}
	f.signals[r.ID] = &r
	return r.ID, nil
}

func (f *fakeTGStore) LatestLLMReview(_ context.Context, signalID int64) (*wheelstore.ActionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.reviews[signalID]
	if !ok {
		return nil, wheelstore.ErrNotFound
	}
	return r, nil
}

func (f *fakeTGStore) LatestAction(_ context.Context, signalID int64, action string) (*wheelstore.ActionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if action == "LLM_REVIEW" {
		if r, ok := f.reviews[signalID]; ok {
			return r, nil
		}
	}
	for i := len(f.appended) - 1; i >= 0; i-- {
		r := f.appended[i]
		if r.SignalID == signalID && r.Action == action {
			return &r, nil
		}
	}
	return nil, wheelstore.ErrNotFound
}

func (f *fakeTGStore) AppendAction(_ context.Context, r wheelstore.ActionRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendFailures > 0 {
		f.appendFailures--
		return 0, errors.New("append action failed")
	}
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

func (f *fakeTGStore) ListPendingOrders(context.Context, string) ([]wheelstore.PendingOrder, error) {
	return []wheelstore.PendingOrder{}, nil
}

func (f *fakeTGStore) QuerySignalsSince(_ context.Context, _ string, afterID int64, _ int) ([]wheelstore.SignalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queryCalls = append(f.queryCalls, afterID)
	var out []wheelstore.SignalRecord
	for _, sig := range f.querySince {
		if sig.ID > afterID {
			out = append(out, sig)
		}
	}
	return out, nil
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dismissed[symbol+"|"+date.Format("2006-01-02")] = true
	return nil
}

func (f *fakeTGStore) IsDismissed(_ context.Context, symbol string, date time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

// appendedCount: 异步测试在 waitFor 里轮询已追加动作数(与 goroutine 写入同锁)。
func (f *fakeTGStore) appendedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.appended)
}

type fakePlacer struct {
	mu        sync.Mutex // confirm/watch run in goroutines; async tests poll fields
	err       error
	orderIDEx string
	orderID   uint64
	gotSymbol string
	gotSide   string
	gotQty    float64
	gotPrice  float64
	calls     int
	// status lookup for watchFill: default not found; set to simulate the
	// gateway's view of the order (0 = not found yet).
	statusCode  int32
	statusErr   error
	statusCalls int
	// cancel handling: recorded id + error for the 撤单 path.
	cancelID    string
	cancelErr   error
	cancelCalls int
}

func (p *fakePlacer) PlaceOrder(_ context.Context, symbol, side string, qty, price float64) (string, uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.gotSymbol, p.gotSide, p.gotQty, p.gotPrice = symbol, side, qty, price
	return p.orderIDEx, p.orderID, p.err
}

func (p *fakePlacer) OrderStatus(_ context.Context, _, orderIDEx string) (int32, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statusCalls++
	if p.statusErr != nil {
		return 0, false, p.statusErr
	}
	if orderIDEx == p.orderIDEx && p.statusCode != 0 {
		return p.statusCode, true, nil
	}
	return 0, false, nil
}

func (p *fakePlacer) CancelOrder(_ context.Context, _, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelCalls++
	p.cancelID = orderID
	return p.cancelErr
}

// 异步测试读访问器(与 goroutine 内的写入加同一把锁)。
func (p *fakePlacer) callsCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.calls }
func (p *fakePlacer) statusCallsCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusCalls
}
func (p *fakePlacer) cancelCallsCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancelCalls
}
func (p *fakePlacer) cancelIDValue() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancelID
}
func (p *fakePlacer) gotOrder() (string, string, float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gotSymbol, p.gotSide, p.gotQty
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
	s := newTelegramScheduler(tg, store, placer, fakeUnderlyingQuoter{}, chatIDs)
	s.now = func() time.Time { return now }
	s.logf = func(string, ...any) {}
	return s
}

// signalFixture is an ALERT with a full accepted candidate (Accepted=true 是
// 执行前置条件:firstCandidate 只取策略实际选中的候选,757 教训)。
func signalFixture(id int64, symbol string, created time.Time) *wheelstore.SignalRecord {
	return &wheelstore.SignalRecord{
		ID: id, Symbol: symbol, Action: "ALERT", ConfigVersion: 1, CapabilityStatus: "READY",
		Inventory: wheelstore.InventorySnapshot{
			CurrentPrice: f64ptr(248.5), ActualInventory: f64ptr(5000),
			TargetInventory: f64ptr(4700), InventoryGap: f64ptr(-300),
		},
		Candidates: []wheelstore.Candidate{{
			Direction: "PUT",
			Quantity:  2,
			Accepted:  true,
			Quote: &wheelstore.Quote{
				Symbol: "US.AAPL260815C250000", OptionType: "CALL", Strike: 250.0,
				Expiry: "2026-08-15T00:00:00Z", Bid: 3.2, Ask: 3.35, Last: 3.28, Delta: 0.42,
				ImpliedVol: 0.25, OpenInterest: 1234,
			},
		}},
		Reason: "gap", CreatedAt: created,
	}
}

// openMarketNow is a fixed weekday instant when US.AAPL is trading
// (2026-08-12 Wed 11:00 America/New_York). Confirm/watch tests freeze time
// here so the 收盘闸门 does not reject the simulated order.
var openMarketNow = time.Date(2026, 8, 12, 15, 0, 0, 0, time.UTC)

// closedMarketNow is a fixed instant when the US market is closed
// (2026-08-12 Wed 18:00 America/New_York): unfilled resting orders must be
// cancelled immediately without reason (老板指令 2026-08-13).
var closedMarketNow = time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)

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
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{orderID: 12345, orderIDEx: "ord-12345"}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	// 异步执行(点击立即回执,处理走 goroutine):先断言即时 toast,再等结果推送。
	if toast := fake.lastToast(t); toast != "已收到,处理中…" {
		t.Fatalf("toast = %q; want 已收到,处理中…", toast)
	}
	waitFor(t, func() bool { return placer.callsCount() == 1 }, "PlaceOrder never happened")
	if sym, side, qty := placer.gotOrder(); sym != "US.AAPL260815C250000" || side != "sell" || qty != 2 {
		t.Fatalf("order = %s %s %v", sym, side, qty)
	}
	act := store.lastAppended(t)
	if act.Action != "CONFIRM" || act.Actor != "telegram:42" {
		t.Fatalf("action = %+v", act)
	}
	if act.Details["order_id"] != uint64(12345) || act.Details["symbol"] != "US.AAPL260815C250000" {
		t.Fatalf("details = %+v", act.Details)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "已下单") || !strings.Contains(text, "12345") {
		t.Fatalf("push = %q; want 已下单 + 订单号", text)
	}
}

func TestCallbackYesReplaceCancelsThenPlaces(t *testing.T) {
	// 改单(老板指令 2026-08-13):确认信号携带 replace 时,先撤旧挂单再下
	// 新单;成功消息标注改单。撤单与新单顺序由 placer 记录验证。
	fake, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.signals[7].Replace = &wheelstore.ReplaceRecord{OrderID: "206158430256", Contract: "US.AAPL260815C240000"}
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{orderID: 12345, orderIDEx: "ord-12345"}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool { return placer.cancelCallsCount() == 1 && placer.callsCount() == 1 }, "cancel+place never completed")
	if id := placer.cancelIDValue(); id != "206158430256" {
		t.Fatalf("CancelOrder id=%q; want 206158430256", id)
	}
	act := store.lastAppended(t)
	if act.Action != "CONFIRM" || act.Details["order_id"] != uint64(12345) {
		t.Fatalf("action = %+v; want CONFIRM order 12345", act)
	}
	// 改单行随推送消息(卡片)发出,toast 仅简短文。
	msg := fake.lastSend(t)
	text, _ := msg["text"].(string)
	if !strings.Contains(text, "改单") || !strings.Contains(text, "206158430256") {
		t.Fatalf("push = %q; want 改单 + 旧单号", text)
	}
}

func TestCallbackYesReplaceCancelFailureRefuses(t *testing.T) {
	// 撤旧挂单失败 = 不执行新单:旧单仍在,再下单即重复敞口(JD 747-752
	// 重复暴露教训)。拒绝并留痕,由用户人工处理旧挂单。
	fake, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.signals[7].Replace = &wheelstore.ReplaceRecord{OrderID: "206158430256", Contract: "US.AAPL260815C240000"}
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{cancelErr: errors.New("sim cancel failed")}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool { return placer.cancelCallsCount() == 1 }, "CancelOrder never happened")
	if calls := placer.callsCount(); calls != 0 {
		t.Fatalf("PlaceOrder calls = %d; want 0 (cancel failed, no new order)", calls)
	}
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "cancel pending order failed" {
		t.Fatalf("action = %+v; want REJECTED cancel pending order failed", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "撤单失败") {
		t.Fatalf("push = %q; want 撤单失败", text)
	}
}

func TestCallbackYesRealEnvRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{err: errLiveEnvNotAllowed}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool { return placer.callsCount() == 1 }, "PlaceOrder never happened")
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "live env not allowed" {
		t.Fatalf("action = %+v; want REJECTED with live-env reason", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "实盘下单不允许") {
		t.Fatalf("push = %q; want 实盘下单不允许", text)
	}
}

func TestCallbackYesExpiredRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now.Add(-11*time.Minute))
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool { return store.appendedCount() > 0 }, "REJECTED never recorded")
	if placer.callsCount() != 0 {
		t.Fatalf("expired signal placed an order; calls = %d", placer.callsCount())
	}
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "signal expired" {
		t.Fatalf("action = %+v; want REJECTED with expired reason", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "已过期") {
		t.Fatalf("push = %q; want 已过期", text)
	}
}

func TestCallbackYesReviewNotApprovedRejected(t *testing.T) {
	for name, review := range map[string]*wheelstore.ActionRecord{
		"reject": {Action: "LLM_REVIEW", Actor: "llm:test", Details: map[string]any{"verdict": "REJECT"}},
		"none":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			fake, server := startFakeTG(t)
			now := openMarketNow
			store := newFakeTGStore()
			store.signals[7] = signalFixture(7, "US.AAPL", now)
			if review != nil {
				store.reviews[7] = review
			}
			placer := &fakePlacer{}
			s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

			s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
			waitFor(t, func() bool { return store.appendedCount() > 0 }, "REJECTED never recorded")
			if placer.callsCount() != 0 {
				t.Fatalf("unapproved signal placed an order; calls = %d", placer.callsCount())
			}
			act := store.lastAppended(t)
			if act.Action != "REJECTED" || !strings.Contains(act.Note, "llm review") {
				t.Fatalf("action = %+v; want REJECTED with review reason", act)
			}
			text, _ := fake.lastSend(t)["text"].(string)
			if !strings.Contains(text, "审核未通过") {
				t.Fatalf("push = %q; want 审核未通过", text)
			}
		})
	}
}

func TestCallbackYesMissingSignalRejected(t *testing.T) {
	_, server := startFakeTG(t)
	store := newFakeTGStore() // no signal 7
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, openMarketNow)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool { return store.appendedCount() > 0 }, "REJECTED never recorded")
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "signal not found" {
		t.Fatalf("action = %+v", act)
	}
}

func TestCallbackNoRecordsAndAnswers(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, openMarketNow)

	s.handleCallback(context.Background(), callback(42, "wheel:9:no"))
	waitFor(t, func() bool { return store.appendedCount() > 0 }, "NO never recorded")
	act := store.lastAppended(t)
	if act.Action != "NO" || act.Actor != "telegram:42" || act.SignalID != 9 {
		t.Fatalf("action = %+v", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "继续等待机会") {
		t.Fatalf("push = %q; want 继续等待机会", text)
	}
}

func TestCallbackNoConfirmedCancelsOrder(t *testing.T) {
	// 老板指令 2026-08-13: 已确认未成交的挂单(701→702 双挂场景),❌ 升级为
	// 撤单:撤销模拟盘挂单 + 记录 NO 解除 pending-order 阻塞。
	fake, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[9] = signalFixture(9, "US.AAPL", now)
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 9, Action: "CONFIRM", Actor: "telegram:42",
		Details: map[string]any{"order_id": float64(12345)}, // JSONB 落库后为 float64
	})
	placer := &fakePlacer{}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:9:no"))
	waitFor(t, func() bool { return placer.cancelCallsCount() == 1 }, "CancelOrder never happened")
	if id := placer.cancelIDValue(); id != "12345" {
		t.Fatalf("CancelOrder id=%q; want 12345", id)
	}
	act := store.lastAppended(t)
	if act.Action != "NO" || act.Note != "撤单成功 订单号 12345" {
		t.Fatalf("action = %+v; want NO 撤单成功", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "撤单成功") {
		t.Fatalf("push = %q; want 撤单成功", text)
	}
}

func TestCallbackNoCancelFailureTellsManual(t *testing.T) {
	// 撤单失败必须显式告知手动撤,挂单不能被静默遗留(否则再次双挂)。
	fake, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[9] = signalFixture(9, "US.AAPL", now)
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 9, Action: "CONFIRM", Actor: "telegram:42",
		Details: map[string]any{"order_id": float64(12345)},
	})
	placer := &fakePlacer{cancelErr: errors.New("sim cancel failed")}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:9:no"))
	waitFor(t, func() bool { return placer.cancelCallsCount() == 1 }, "CancelOrder never happened")
	act := store.lastAppended(t)
	if act.Action != "NO" || !strings.Contains(act.Note, "请手动在模拟盘撤单") {
		t.Fatalf("action = %+v; want NO with manual-cancel note", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "撤单失败") {
		t.Fatalf("push = %q; want 撤单失败", text)
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

func TestStrategyBadge(t *testing.T) {
	if got := strategyBadge("llm"); got != "🤖 LLM 策略" {
		t.Fatalf("llm badge = %q", got)
	}
	if got := strategyBadge("wheel"); got != "⚙️ 固化策略" {
		t.Fatalf("wheel badge = %q", got)
	}
	if got := strategyBadge(""); got != "⚙️ 固化策略" {
		t.Fatalf("empty badge = %q, want wheel default", got)
	}
}

func TestAlertMessageFormat(t *testing.T) {
	created := time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC)
	sig := signalFixture(7, "US.AAPL", created)
	text, err := alertMessage(sig, "苹果公司", "IV 高位", "风险 < 可控")
	if err != nil {
		t.Fatal(err)
	}
	want := `<b>📌 US.AAPL · 卖出认沽 (SELL PUT) · 信号 #7 · ⚙️ 固化策略</b>
━━━━━━━━━━━━━━━━━━━━
🎯 <b>订单</b>
候选      <b><code>US.AAPL260815C250000</code></b>
行权      <b><code>250.00</code></b>
到期      <code>2026-08-15</code> (剩 4 天)
数量      <b><code>2</code></b> 张
限价      <b><code>3.28</code></b> (估算)
┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
📊 <b>标的当前</b>
标的      <b>苹果公司 · AAPL</b>
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
💡 <b>下单原因</b> · LLM 审核 <b>✅ APPROVE</b>
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
	sig.Candidates[0].Quote.Last = 0
	text, err := alertMessage(sig, "")
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

func TestPushSignalPushesRejectedReviewReasons(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(10, "US.AAPL", now)
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 10,
		Action:   "REJECTED",
		Actor:    "llm:test",
		Details: map[string]any{
			"verdict": "REJECT",
			"reasons": []any{"risk limit", "missing cash buffer"},
		},
	})
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, now)

	if retry := s.pushSignal(context.Background(), *sig); retry {
		t.Fatal("pushSignal with a REJECTED review must not retry")
	}
	if len(fake.sends) != 1 {
		t.Fatalf("rejected signal sends = %d, want 1", len(fake.sends))
	}
	text, _ := fake.lastSend(t)["text"].(string)
	for _, want := range []string{"信号 #10", "risk limit", "missing cash buffer"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rejection push = %q; want %q", text, want)
		}
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
	noQty.Candidates[0].Quantity = 0
	c, err = firstCandidate(noQty)
	if err != nil || c.Quantity != 1 {
		t.Fatalf("default qty: c=%+v err=%v", c, err)
	}
}

// TestFirstCandidatePrefersAcceptedOverHead: signal 757 回归——被排除的挂单
// 合约排在候选列表首位(Accepted=false),LLM 审核批准的是列表后部的 accepted
// 候选;firstCandidate 必须返回 accepted 候选,绝不回退列表首位(757:审核
// 批 28.5 的 P28500,执行却落 29.0 的 P29000)。
func TestFirstCandidatePrefersAcceptedOverHead(t *testing.T) {
	sig := signalFixture(1, "US.AAPL", time.Now())
	sig.Candidates = []wheelstore.Candidate{
		{ // 挂单合约被排除,排在列表头部
			Direction: "PUT", Quantity: 1,
			Quote: &wheelstore.Quote{Symbol: "US.JD260821P29000", OptionType: "PUT", Strike: 29.0, Expiry: "2026-08-21T00:00:00Z", Last: 1.5},
		},
		{ // 策略实际选中的候选(LLM 审核批准)
			Direction: "PUT", Quantity: 1, Accepted: true,
			Quote: &wheelstore.Quote{Symbol: "US.JD260821P28500", OptionType: "PUT", Strike: 28.5, Expiry: "2026-08-21T00:00:00Z", Last: 1.7},
		},
	}
	c, err := firstCandidate(sig)
	if err != nil {
		t.Fatal(err)
	}
	if c.Code != "US.JD260821P28500" {
		t.Fatalf("candidate = %q; want the accepted P28500, never the excluded head P29000", c.Code)
	}
}

// TestFirstCandidateRefusesNoAccepted: 资金安全(老板指令 2026-08-13: 不设
// 退化策略,异常直接取消订单)——全无 accepted 候选时 firstCandidate 必须
// 报错,执行层拒绝下单,而不是回退列表首位。
func TestFirstCandidateRefusesNoAccepted(t *testing.T) {
	sig := signalFixture(1, "US.AAPL", time.Now())
	sig.Candidates[0].Accepted = false
	if _, err := firstCandidate(sig); err == nil {
		t.Fatal("no-accepted signal must error; refusing the order is the only safe move")
	}
	// 候选带符号但 accepted=false + 空候选混合:同样必须拒绝。
	sig.Candidates = append(sig.Candidates, wheelstore.Candidate{
		Direction: "PUT", Quantity: 1,
		Quote: &wheelstore.Quote{Symbol: "US.AAPL260815C260000", OptionType: "CALL", Strike: 260.0, Last: 1.1},
	})
	if _, err := firstCandidate(sig); err == nil {
		t.Fatal("mixed no-accepted signal must error")
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

func TestRunPushWaterlineRetainsPendingPrefixAndDeduplicatesTail(t *testing.T) {
	fake, server := startFakeTG(t)
	now := time.Date(2026, 8, 11, 15, 30, 0, 0, time.UTC)
	store := newFakeTGStore()
	store.querySince = []wheelstore.SignalRecord{
		*signalFixture(1, "US.AAPL", now),
		*signalFixture(2, "US.MSFT", now),
	}
	store.reviews[2] = approvedReview()
	s := newTestScheduler(t, server, store, &fakePlacer{}, map[int64]bool{42: true}, now)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.runPush(ctx, 2*time.Millisecond) }()
	defer func() {
		cancel()
		<-done
	}()

	waitForSends := func(want int) {
		t.Helper()
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		for {
			fake.mu.Lock()
			got := len(fake.sends)
			fake.mu.Unlock()
			if got >= want {
				return
			}
			select {
			case <-deadline.C:
				t.Fatalf("sendMessage count = %d; want at least %d", got, want)
			case <-time.After(time.Millisecond):
			}
		}
	}
	waitForSends(1)

	fake.mu.Lock()
	firstText, _ := fake.sends[0]["text"].(string)
	fake.mu.Unlock()
	if !strings.Contains(firstText, "信号 #2") {
		t.Fatalf("first push = %q; want the later signal #2", firstText)
	}

	// Signal #1 was pending in the same batch. Once its review lands, the
	// held waterline must revisit it while the already-pushed #2 is not sent
	// again.
	store.mu.Lock()
	store.reviews[1] = approvedReview()
	store.mu.Unlock()
	waitForSends(2)

	fake.mu.Lock()
	texts := make([]string, 0, len(fake.sends))
	for _, send := range fake.sends {
		text, _ := send["text"].(string)
		texts = append(texts, text)
	}
	fake.mu.Unlock()
	if len(texts) != 2 {
		t.Fatalf("sendMessage count = %d; want exactly 2", len(texts))
	}
	countSignal := func(id string) int {
		count := 0
		for _, text := range texts {
			if strings.Contains(text, id) {
				count++
			}
		}
		return count
	}
	if countSignal("信号 #1") != 1 || countSignal("信号 #2") != 1 {
		t.Fatalf("pushes = %#v; want one push for each signal", texts)
	}
}

func TestCallbackYesDoubleConfirmRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{orderID: 12345}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	first := callback(42, "wheel:7:yes")
	second := callback(42, "wheel:7:yes")
	s.handleCallback(context.Background(), first)
	waitFor(t, func() bool { return placer.callsCount() == 1 }, "first PlaceOrder never happened")
	s.handleCallback(context.Background(), second)
	waitFor(t, func() bool { return store.appendedCount() >= 2 }, "second press never recorded")
	if placer.callsCount() != 1 {
		t.Fatalf("PlaceOrder calls = %d; want 1 (second press must be refused)", placer.callsCount())
	}
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "already confirmed" {
		t.Fatalf("last action = %+v; want REJECTED already confirmed", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "请勿重复确认") {
		t.Fatalf("push = %q; want 请勿重复确认", text)
	}
}

func TestCallbackYesAuditFailureStillCannotRepeatOrder(t *testing.T) {
	_, server := startFakeTG(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	store.appendFailures = 1 // broker succeeds, CONFIRM audit append fails
	placer := &fakePlacer{orderID: 12345}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool { return placer.callsCount() == 1 }, "PlaceOrder never happened")
	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		if len(store.appended) == 0 {
			return false
		}
		return store.appended[len(store.appended)-1].Action == "REJECTED"
	}, "second press never rejected")
	if placer.callsCount() != 1 {
		t.Fatalf("PlaceOrder calls = %d; want durable claim to block retry", placer.callsCount())
	}
	if act := store.lastAppended(t); act.Action != "REJECTED" || act.Note != "already confirmed" {
		t.Fatalf("last action = %+v", act)
	}
}

// TestWatchFillPushesFill: a confirmed order that fills must push 已成交 and
// record a FILL action (老板指令 2026-08-12: 成交成功必须推送)。
func TestWatchFillPushesFill(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	now := openMarketNow
	placer := &fakePlacer{orderIDEx: "ord-1", statusCode: int32(trdcommon.OrderStatus_OrderStatus_Filled_All)}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.watchFill(context.Background(), 7, "US.AAPL", "buy", 100, 457.4, "ord-1", 12345)
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "已成交") || !strings.Contains(text, "信号 #7") {
		t.Fatalf("fill push = %q; want 已成交 + 信号 #7", text)
	}
	if !strings.Contains(text, "ord-1") {
		t.Fatalf("fill push = %q; want order id ex", text)
	}
	if a := store.lastAppended(t); a.Action != "FILL" {
		t.Fatalf("action = %s; want FILL", a.Action)
	}
	if placer.statusCalls != 1 {
		t.Fatalf("OrderStatus calls = %d; want 1", placer.statusCalls)
	}
}

// TestWatchFillReportsPendingThenKeepsWatching: an unfilled order inside the
// watch window pushes 挂单中未成交 once, then keeps watching (until close /
// terminal state / ctx cancel) instead of going silent.
func TestWatchFillReportsPendingThenKeepsWatching(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	now := openMarketNow
	placer := &fakePlacer{orderIDEx: "ord-1"} // statusCode 0 → gateway never knows it
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)
	s.watchEvery = 5 * time.Millisecond
	s.watchReport = 12 * time.Millisecond
	// 测试时钟需随轮询前进,否则 s.now().Sub(started) 恒为 0,挂单报告永不触发。
	var clockMu sync.Mutex
	clock := now
	s.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		clock = clock.Add(25 * time.Millisecond)
		return clock
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.watchFill(ctx, 7, "US.AAPL", "buy", 100, 457.4, "ord-1", 12345)
		close(done)
	}()
	waitFor(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for _, send := range fake.sends {
			text, _ := send["text"].(string)
			if strings.Contains(text, "挂单中未成交") {
				return true
			}
		}
		return false
	}, "挂单中未成交 never pushed")
	// 报告后仍继续轮询(未成交 → 观察至收盘/终态),不静默退出。
	polls := placer.statusCallsCount()
	waitFor(t, func() bool { return placer.statusCallsCount() > polls }, "watch stopped polling after report")
	cancel()
	<-done
	if c := placer.cancelCallsCount(); c != 0 {
		t.Fatalf("CancelOrder calls = %d; want 0 (market still open)", c)
	}
}

// TestWatchFillCancelsAtMarketClose: 收盘订单立即无理由取消(老板指令
// 2026-08-13)——市场已收盘时未成交挂单立刻撤单并推送。
func TestWatchFillCancelsAtMarketClose(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	placer := &fakePlacer{orderIDEx: "ord-1"} // resting, unfilled
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, closedMarketNow)

	s.watchFill(context.Background(), 7, "US.AAPL", "sell", 1, 1.7, "ord-1", 206158430256)
	if placer.cancelCalls != 1 || placer.cancelID != "206158430256" {
		t.Fatalf("CancelOrder calls=%d id=%q; want 1/206158430256 at close", placer.cancelCalls, placer.cancelID)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "已撤单") || !strings.Contains(text, "市场收盘") {
		t.Fatalf("cancel push = %q; want 已撤单 + 市场收盘", text)
	}
	if a := store.lastAppended(t); a.Action != "NO" || !strings.Contains(a.Note, "市场收盘") {
		t.Fatalf("action = %+v; want NO with 市场收盘 note", a)
	}
}

// TestWatchFillCancelsOnStatusError: 异常订单立即无理由取消(fail-closed,
// 资金安全)——状态查询失败(无法确认订单状态)立即撤单,不假装挂单受控。
func TestWatchFillCancelsOnStatusError(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	placer := &fakePlacer{orderIDEx: "ord-1", statusErr: errors.New("gateway blip")}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, openMarketNow)

	s.watchFill(context.Background(), 7, "US.AAPL", "sell", 1, 1.7, "ord-1", 12345)
	if placer.cancelCalls != 1 || placer.cancelID != "12345" {
		t.Fatalf("CancelOrder calls=%d id=%q; want 1/12345 on status error", placer.cancelCalls, placer.cancelID)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "已撤单") || !strings.Contains(text, "状态异常") {
		t.Fatalf("cancel push = %q; want 已撤单 + 状态异常", text)
	}
}

// TestWatchFillMissingOrderIDRefusesCancel: 没有 numeric 订单号时不能发起
// 撤单(可能撤错),推送手动撤单提示——不静默。
func TestWatchFillMissingOrderIDRefusesCancel(t *testing.T) {
	fake, server := startFakeTG(t)
	store := newFakeTGStore()
	placer := &fakePlacer{orderIDEx: "ord-1"}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, closedMarketNow)

	s.watchFill(context.Background(), 7, "US.AAPL", "sell", 1, 1.7, "ord-1", 0)
	if placer.cancelCalls != 0 {
		t.Fatalf("CancelOrder calls = %d; want 0 (no numeric id)", placer.cancelCalls)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "请手动在模拟盘撤单") {
		t.Fatalf("push = %q; want 手动撤单提示", text)
	}
}

// TestCallbackYesMarketClosedRejected: 收盘订单立即无理由取消(老板指令
// 2026-08-13)——市场已收盘时确认按钮拒绝下单,不产生收盘即失效的挂单。
func TestCallbackYesMarketClosedRejected(t *testing.T) {
	fake, server := startFakeTG(t)
	now := closedMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{}
	s := newTestScheduler(t, server, store, placer, map[int64]bool{42: true}, now)

	s.handleCallback(context.Background(), callback(42, "wheel:7:yes"))
	waitFor(t, func() bool { return store.appendedCount() > 0 }, "REJECTED never recorded")
	if placer.callsCount() != 0 {
		t.Fatalf("closed-market signal placed an order; calls = %d", placer.callsCount())
	}
	act := store.lastAppended(t)
	if act.Action != "REJECTED" || act.Note != "market closed" {
		t.Fatalf("action = %+v; want REJECTED market closed", act)
	}
	text, _ := fake.lastSend(t)["text"].(string)
	if !strings.Contains(text, "市场已收盘") {
		t.Fatalf("push = %q; want 市场已收盘", text)
	}
}
