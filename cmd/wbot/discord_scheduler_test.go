package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/discord"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/wheelstore"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

const testChannelID = "chan-1"

// fakeDCSchedulerServer records channel message payloads (test fixture: fake
// bot token, no real Discord).
type fakeDCSchedulerServer struct {
	mu      sync.Mutex
	sends   []map[string]any
	authErr string
}

func (f *fakeDCSchedulerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := r.Header.Get("Authorization"); got != "Bot "+tgTestToken {
		f.authErr = got
		http.Error(w, "bad auth", http.StatusUnauthorized)
		return
	}
	var p map[string]any
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f.sends = append(f.sends, p)
	w.Write([]byte(`{"id":"m1"}`))
}

func (f *fakeDCSchedulerServer) lastSend(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) == 0 {
		t.Fatal("no discord message received")
	}
	return f.sends[len(f.sends)-1]
}

// syncPlacer guards a fakePlacer whose methods run on the confirm goroutine
// while the test polls its call count (race-detector clean).
type syncPlacer struct {
	mu   sync.Mutex
	fake *fakePlacer
}

func (p *syncPlacer) PlaceOrder(ctx context.Context, symbol, side string, qty, price float64) (string, uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.PlaceOrder(ctx, symbol, side, qty, price)
}

func (p *syncPlacer) OrderStatus(ctx context.Context, symbol, orderIDEx string) (int32, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.OrderStatus(ctx, symbol, orderIDEx)
}

func (p *syncPlacer) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.calls
}

// blockPlacer parks confirm goroutines inside PlaceOrder until release, so a
// concurrent second press deterministically lands in the dedup race window
// (HasAction→AppendAction across the network call).
type blockPlacer struct {
	mu          sync.Mutex
	fake        *fakePlacer
	entered     int
	releaseOnce sync.Once
	releaseCh   chan struct{}
}

func (p *blockPlacer) PlaceOrder(ctx context.Context, symbol, side string, qty, price float64) (string, uint64, error) {
	p.mu.Lock()
	p.entered++
	p.mu.Unlock()
	<-p.releaseCh
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.PlaceOrder(ctx, symbol, side, qty, price)
}

func (p *blockPlacer) OrderStatus(ctx context.Context, symbol, orderIDEx string) (int32, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.OrderStatus(ctx, symbol, orderIDEx)
}

func (p *blockPlacer) enteredCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.entered
}

func (p *blockPlacer) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.calls
}

// release lets blocked confirm goroutines finish their order placement.
func (p *blockPlacer) release() {
	p.releaseOnce.Do(func() { close(p.releaseCh) })
}

// waitEntered polls until n confirms sit inside PlaceOrder (the network-call
// window between HasAction and AppendAction).
func (p *blockPlacer) waitEntered(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for p.enteredCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("PlaceOrder entered = %d; want %d", p.enteredCount(), n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitAppended polls until the store has recorded at least n actions.
func waitAppended(t *testing.T, store *fakeTGStore, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		store.mu.Lock()
		got := len(store.appended)
		store.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("appended actions = %d; want %d", got, n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func startFakeDC(t *testing.T) (*fakeDCSchedulerServer, *httptest.Server) {
	t.Helper()
	fake := &fakeDCSchedulerServer{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

// newTestDiscordScheduler wires a scheduler to a fake Discord server with a
// fake keypair; the returned private key signs test requests.
func newTestDiscordScheduler(t *testing.T, fake *fakeDCSchedulerServer, store *fakeTGStore, placer wheelOrderPlacer, now time.Time) (*discordScheduler, ed25519.PrivateKey) {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dc, err := discord.New(tgTestToken, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	verifier, priv := discordKeypair(t)
	s := newDiscordScheduler(ctx, dc, verifier, "app-1", testChannelID, store, placer)
	s.now = func() time.Time { return now }
	s.logf = func(string, ...any) {}
	return s, priv
}

// discordKeypair is the fake public-key fixture for the verifier.
func discordKeypair(t *testing.T) (*discord.Verifier, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v, err := discord.NewVerifier(hex.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	return v, priv
}

func dcInteraction(userID, customID string) *discord.Interaction {
	return &discord.Interaction{
		ID: "i-" + customID, Type: discord.TypeMessageComponent, Token: "tok",
		ChannelID: testChannelID, Member: &discord.InteractionMember{User: discord.InteractionUser{ID: userID}},
		Data: &discord.InteractionData{CustomID: customID},
	}
}

// signInteraction signs timestamp||body like Discord does (test fixture key).
func signInteraction(t *testing.T, priv ed25519.PrivateKey, ts string, body []byte) string {
	t.Helper()
	msg := append([]byte(ts), body...)
	return hex.EncodeToString(ed25519.Sign(priv, msg))
}

func signedInteractionRequest(t *testing.T, priv ed25519.PrivateKey, body []byte) *http.Request {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r := httptest.NewRequest(http.MethodPost, "/v1/discord/interactions", strings.NewReader(string(body)))
	r.Header.Set("X-Signature-Timestamp", ts)
	r.Header.Set("X-Signature-Ed25519", signInteraction(t, priv, ts, body))
	return r
}

func lastEmbed(t *testing.T, fake *fakeDCSchedulerServer) map[string]any {
	t.Helper()
	payload := fake.lastSend(t)
	embeds, _ := payload["embeds"].([]any)
	embed, _ := embeds[0].(map[string]any)
	return embed
}

func lastEmbedDesc(t *testing.T, fake *fakeDCSchedulerServer) string {
	t.Helper()
	desc, _ := lastEmbed(t, fake)["description"].(string)
	return desc
}

func discordEmbeds(t *testing.T, payload map[string]any) []any {
	t.Helper()
	embeds, ok := payload["embeds"].([]any)
	if !ok {
		t.Fatalf("embeds = %#v", payload["embeds"])
	}
	return embeds
}

func discordEmbedAt(t *testing.T, embeds []any, index int) map[string]any {
	t.Helper()
	if index >= len(embeds) {
		t.Fatalf("embed index %d out of range (len=%d)", index, len(embeds))
	}
	embed, ok := embeds[index].(map[string]any)
	if !ok {
		t.Fatalf("embed %d = %#v", index, embeds[index])
	}
	return embed
}

func TestDiscordPushApprovedSignalPushesEmbed(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(7, "US.AAPL", now)
	store.reviews[7] = &wheelstore.ActionRecord{Details: map[string]any{
		"verdict": "APPROVE", "reasons": []any{"reason one"},
	}}
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("approved signal must not retry")
	}
	payload := fake.lastSend(t)
	embeds := discordEmbeds(t, payload)
	if len(embeds) != 5 {
		t.Fatalf("embed count = %d; want 5", len(embeds))
	}
	header := discordEmbedAt(t, embeds, 0)
	if header["color"] != float64(discord.ColorApprove) {
		t.Fatalf("embed color = %v; want approve green", header["color"])
	}
	author, _ := header["author"].(map[string]any)
	footer, _ := header["footer"].(map[string]any)
	if author["name"] != "🤖 Wheel Bot" || footer["text"] != "配置 v1 · 信号 #7 · 08-12 10:00" {
		t.Fatalf("header author/footer = %#v / %#v", author, footer)
	}
	if header["timestamp"] != "2026-08-12T10:00:00Z" {
		t.Fatalf("header timestamp = %#v", header["timestamp"])
	}
	if title := header["title"]; title != "🔴 模拟盘 · 📌 信号 #7 · US.AAPL · 卖出认沽 (SELL PUT)" {
		t.Fatalf("header title = %#v", title)
	}
	if desc := header["description"].(string); !strings.Contains(desc, "候选 `US.AAPL260815C250000` 已就绪") {
		t.Fatalf("header description = %q", desc)
	}
	order := discordEmbedAt(t, embeds, 1)
	if _, ok := order["title"]; ok || order["description"] != "```\n候选  US.AAPL260815C250000\n数量  2 张\n限价  3.28\n```" {
		t.Fatalf("order embed = %#v", order)
	}
	option := discordEmbedAt(t, embeds, 2)
	if _, ok := option["title"]; ok || option["description"] != "```\n行权  250.00  Δ 0.42\n到期  08-15  IV 0.25\n报价  3.20/3.35  OI 1,234\n```" {
		t.Fatalf("option embed = %#v", option)
	}
	underlying := discordEmbedAt(t, embeds, 3)
	if _, ok := underlying["title"]; ok || underlying["description"] != "```\n现价  248.50\n缺口  -300 股\n目标  4,700 / 持仓 5,000\n```" {
		t.Fatalf("underlying embed = %#v", underlying)
	}
	reasons := discordEmbedAt(t, embeds, 4)
	if _, ok := reasons["title"]; ok || reasons["description"] != "• reason one" {
		t.Fatalf("reasons embed = %#v", reasons)
	}
	rows, _ := payload["components"].([]any)
	if len(rows) != 1 {
		t.Fatalf("action row count = %d; want 1", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if row["type"] != float64(1) {
		t.Fatalf("action row = %#v", row)
	}
	buttons, _ := row["components"].([]any)
	wantIDs := []string{"wheel:7:yes", "wheel:7:no", "wheel:7:dismiss"}
	wantStyles := []float64{3, 4, 2}
	for i, want := range wantIDs {
		button, _ := buttons[i].(map[string]any)
		if button["type"] != float64(2) || button["custom_id"] != want || button["style"] != wantStyles[i] {
			t.Fatalf("button %d = %#v; want id=%s style=%v type=2", i, button, want, wantStyles[i])
		}
	}
	if fake.authErr != "" {
		t.Fatalf("authorization = %q; want Bot token", fake.authErr)
	}
}

func TestDiscordPushApprovedStockSignalOmitsOptionEmbed(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(8, "US.AAPL", now)
	sig.Candidates[0].Direction = "BUY"
	sig.Candidates[0].Quantity = 100
	sig.Candidates[0].Quote.Symbol = "US.AAPL"
	sig.Candidates[0].Quote.Last = 248.5
	store.reviews[8] = &wheelstore.ActionRecord{Details: map[string]any{"verdict": "APPROVE"}}
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("approved stock signal must not retry")
	}
	embeds := discordEmbeds(t, fake.lastSend(t))
	if len(embeds) != 4 {
		t.Fatalf("stock embed count = %d; want 4", len(embeds))
	}
	if title := discordEmbedAt(t, embeds, 0)["title"]; title != "🔴 模拟盘 · 📌 信号 #8 · US.AAPL · BUY" {
		t.Fatalf("header title = %#v", title)
	}
	for i := 1; i < len(embeds); i++ {
		if _, ok := discordEmbedAt(t, embeds, i)["title"]; ok {
			t.Fatalf("embed %d unexpectedly has a title", i)
		}
	}
	order := discordEmbedAt(t, embeds, 1)["description"]
	if order != "```\n候选  US.AAPL\n数量  100 股\n限价  248.50\n```" {
		t.Fatalf("stock order = %q", order)
	}
}

func TestDiscordPushRejectedReviewPushesGrayEmbed(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(10, "US.AAPL", now)
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 10, Action: "REJECTED", Actor: "llm:test",
		Details: map[string]any{"verdict": "REJECT", "reasons": []any{"risk limit"}},
	})
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("rejected signal must not retry")
	}
	payload := fake.lastSend(t)
	embeds, _ := payload["embeds"].([]any)
	embed, _ := embeds[0].(map[string]any)
	if embed["color"] != float64(discord.ColorRejected) {
		t.Fatalf("embed color = %v; want rejection gray", embed["color"])
	}
	author, _ := embed["author"].(map[string]any)
	if author["name"] != "🤖 Wheel Bot" || embed["title"] != "🔴 模拟盘 · ❌ 信号 #10 被 LLM 审核拒绝" {
		t.Fatalf("rejection author/title = %#v / %#v", author, embed["title"])
	}
	desc, _ := embed["description"].(string)
	if !strings.Contains(desc, "• risk limit") || !strings.Contains(desc, "US.AAPL · REJECT") {
		t.Fatalf("rejection embed = %#v", embed)
	}
}

func TestDiscordPushRetriesMissingReview(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(9, "US.AAPL", now)
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); !retry {
		t.Fatal("signal without a recorded review must retry")
	}
	if len(fake.sends) != 0 {
		t.Fatalf("signal pushed without review; sends = %d", len(fake.sends))
	}
}

func TestDiscordConfirmPlacesLimitOrder(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{orderID: 12345, orderIDEx: "ord-12345"}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	s.confirmOrderDiscord(context.Background(), dcInteraction("42", "wheel:7:yes"), 7)
	if placer.calls != 1 {
		t.Fatalf("PlaceOrder calls = %d; want 1", placer.calls)
	}
	if placer.gotSymbol != "US.AAPL260815C250000" || placer.gotSide != "sell" || placer.gotQty != 2 || placer.gotPrice != 3.28 {
		t.Fatalf("order = %s %s %v @ %v; want limit 3.28", placer.gotSymbol, placer.gotSide, placer.gotQty, placer.gotPrice)
	}
	act := store.lastAppended(t)
	if act.Action != "CONFIRM" || act.Actor != "discord:42" {
		t.Fatalf("action = %+v", act)
	}
	embed := lastEmbed(t, fake)
	desc, _ := embed["description"].(string)
	if !strings.Contains(embed["title"].(string), "已下单") {
		t.Fatalf("confirm title = %v", embed["title"])
	}
	for _, want := range []string{"信号 #7", "12345"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("confirm push missing %q: %s", want, desc)
		}
	}
}

func TestDiscordConfirmExpiredRejected(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now.Add(-11*time.Minute))
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	s.confirmOrderDiscord(context.Background(), dcInteraction("42", "wheel:7:yes"), 7)
	if placer.calls != 0 {
		t.Fatalf("expired signal placed an order; calls = %d", placer.calls)
	}
	if act := store.lastAppended(t); act.Action != "REJECTED" || act.Note != "signal expired" {
		t.Fatalf("action = %+v; want REJECTED expired", act)
	}
	if title := lastEmbed(t, fake)["title"].(string); !strings.Contains(title, "下单失败") {
		t.Fatalf("rejection title = %q", title)
	}
}

func TestDiscordConfirmMissingLimitPriceRejected(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Now()
	store := newFakeTGStore()
	sig := signalFixture(7, "US.AAPL", now)
	sig.Candidates[0].Quote.Last = 0 // #36 强类型化后等价于旧 map 删 last 键:触发「no usable limit price」拒绝路径
	store.signals[7] = sig
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	s.confirmOrderDiscord(context.Background(), dcInteraction("42", "wheel:7:yes"), 7)
	if placer.calls != 0 {
		t.Fatalf("candidate without price placed an order; calls = %d", placer.calls)
	}
	if act := store.lastAppended(t); act.Action != "REJECTED" || act.Note != "no usable limit price" {
		t.Fatalf("action = %+v; want REJECTED no limit price", act)
	}
}

func TestDiscordConfirmConcurrentDoublePressRejected(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &blockPlacer{fake: &fakePlacer{orderID: 1}, releaseCh: make(chan struct{})}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	// First press parks inside PlaceOrder; the second press must then queue on
	// the confirm mutex instead of slipping a second order through the
	// HasAction→AppendAction window.
	go s.confirmOrderDiscord(context.Background(), dcInteraction("42", "wheel:7:yes"), 7)
	placer.waitEntered(t, 1)
	go s.confirmOrderDiscord(context.Background(), dcInteraction("42", "wheel:7:yes"), 7)
	deadline := time.Now().Add(500 * time.Millisecond)
	for placer.enteredCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	placer.release()
	waitAppended(t, store, 2) // both presses settled: CONFIRM then REJECTED

	if got := placer.calls(); got != 1 {
		t.Fatalf("PlaceOrder calls = %d; want 1 (second press serialized behind first)", got)
	}
	if act := store.lastAppended(t); act.Action != "REJECTED" || act.Note != "already confirmed" {
		t.Fatalf("last action = %+v; want REJECTED already confirmed", act)
	}
}

func TestDiscordHandleInteractionPingPong(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, priv := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())

	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, []byte(`{"type":1}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"type":1`) {
		t.Fatalf("pong body = %s", rec.Body.String())
	}
}

func TestDiscordHandleInteractionBadSignature401(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, _ := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/v1/discord/interactions", strings.NewReader(`{"type":1}`))
	req.Header.Set("X-Signature-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Signature-Ed25519", "deadbeef")
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", rec.Code)
	}
}

func TestDiscordHandleInteractionStaleSignature401(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, priv := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())
	body := []byte(`{"type":1}`)
	ts := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/v1/discord/interactions", strings.NewReader(string(body)))
	req.Header.Set("X-Signature-Timestamp", ts)
	req.Header.Set("X-Signature-Ed25519", signInteraction(t, priv, ts, body))
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401 for replayed request", rec.Code)
	}
}

func TestDiscordHandleInteractionYesDispatchesConfirm(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Now()
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &syncPlacer{fake: &fakePlacer{orderID: 99}}
	s, priv := newTestDiscordScheduler(t, fake, store, placer, now)

	body := []byte(`{"id":"i1","type":3,"channel_id":"chan-1","member":{"user":{"id":"42"}},"data":{"custom_id":"wheel:7:yes"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "已记录") {
		t.Fatalf("response body = %s", rec.Body.String())
	}
	deadline := time.After(5 * time.Second)
	for placer.calls() == 0 {
		select {
		case <-deadline:
			t.Fatal("confirm goroutine never placed the order")
		case <-time.After(2 * time.Millisecond):
		}
	}
	if act := store.lastAppended(t); act.Action != "CONFIRM" || act.Actor != "discord:42" {
		t.Fatalf("action = %+v; want CONFIRM by discord:42", act)
	}
}

func TestDiscordHandleInteractionNoRecordsAndAnswers(t *testing.T) {
	fake, _ := startFakeDC(t)
	store := newFakeTGStore()
	s, priv := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, time.Now())

	body := []byte(`{"id":"i2","type":3,"channel_id":"chan-1","member":{"user":{"id":"42"}},"data":{"custom_id":"wheel:9:no"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "继续等待") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	act := store.lastAppended(t)
	if act.Action != "NO" || act.Actor != "discord:42" || act.SignalID != 9 {
		t.Fatalf("action = %+v", act)
	}
	if desc := lastEmbedDesc(t, fake); !strings.Contains(desc, "信号 #9") {
		t.Fatalf("push = %q; want 信号 #9", desc)
	}
}

func TestDiscordHandleInteractionMalformedCustomID(t *testing.T) {
	fake, _ := startFakeDC(t)
	store := newFakeTGStore()
	s, priv := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, time.Now())

	body := []byte(`{"id":"i3","type":3,"member":{"user":{"id":"42"}},"data":{"custom_id":"bogus"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "无效") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(store.appended) != 0 {
		t.Fatalf("malformed custom_id wrote %d actions", len(store.appended))
	}
}

// TestStartDiscordSchedulerNotConfiguredSkips: no wbot.conf credentials →
// the scheduler is nil and serve just logs (telegram-style degrade; the
// interactions endpoint then stays unregistered).
func TestStartDiscordSchedulerNotConfiguredSkips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WBOT_CONFIG_DIR", dir)
	ds, err := startDiscordScheduler(context.Background(), nil, futu.EnvSim)
	if err != nil {
		t.Fatalf("missing config must not error: %v", err)
	}
	if ds != nil {
		t.Fatalf("scheduler = %+v; want nil (not configured)", ds)
	}
}

// TestStartDiscordSchedulerConfiguredWiresClient: the four credentials set →
// a fully wired scheduler (fake values; the real key would be validated by
// Discord at runtime).
func TestStartDiscordSchedulerConfiguredWiresClient(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WBOT_CONFIG_DIR", dir)
	cfg, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{
		{"credentials.discord.app_id", "app-1"},
		{"credentials.discord.public_key", strings.Repeat("ab", 32)},
		{"credentials.discord.bot_token", "tok"},
		{"credentials.discord.channel_id", "chan-1"},
	} {
		if err := cfg.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := startDiscordScheduler(context.Background(), nil, futu.EnvSim)
	if err != nil {
		t.Fatalf("configured scheduler: %v", err)
	}
	if ds == nil || ds.channelID != "chan-1" || ds.dc == nil || ds.verifier == nil {
		t.Fatalf("scheduler = %+v; want fully wired", ds)
	}
}

func TestDiscordWatchFillPushesFill(t *testing.T) {
	fake, _ := startFakeDC(t)
	store := newFakeTGStore()
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	placer := &fakePlacer{orderIDEx: "ord-1", statusCode: int32(trdcommon.OrderStatus_OrderStatus_Filled_All)}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	s.watchFillDiscord(context.Background(), 7, "HK.00700", "buy", 100, 457.4, "ord-1")
	embed := lastEmbed(t, fake)
	desc, _ := embed["description"].(string)
	if !strings.Contains(embed["title"].(string), "已成交") || !strings.Contains(desc, "信号 #7") || !strings.Contains(desc, "ord-1") {
		t.Fatalf("fill embed = title %v desc %q", embed["title"], desc)
	}
	if a := store.lastAppended(t); a.Action != "FILL" {
		t.Fatalf("action = %s; want FILL", a.Action)
	}
	if placer.statusCalls != 1 {
		t.Fatalf("OrderStatus calls = %d; want 1", placer.statusCalls)
	}
}
