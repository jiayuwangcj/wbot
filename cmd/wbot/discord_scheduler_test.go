package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// fakeDCSchedulerServer records channel message payloads and webhook deletes
// (test fixture: fake bot token, no real Discord).
type fakeDCSchedulerServer struct {
	mu         sync.Mutex
	sends      []map[string]any
	deletes    []string
	authErr    string
	failCreate int // remaining create-message failures to inject
}

func (f *fakeDCSchedulerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := r.Header.Get("Authorization"); got != "Bot "+tgTestToken {
		f.authErr = got
		http.Error(w, "bad auth", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodDelete {
		f.deletes = append(f.deletes, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if f.failCreate > 0 {
		f.failCreate--
		http.Error(w, "simulated create-message failure", http.StatusInternalServerError)
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

// hasDeleteReply reports whether the in-progress interaction reply was deleted
// (DELETE /webhooks/{app}/{token}/messages/@original).
func (f *fakeDCSchedulerServer) hasDeleteReply() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.deletes {
		if strings.Contains(p, "/messages/@original") {
			return true
		}
	}
	return false
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

func (f *fakeDCSchedulerServer) sendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

type blockingAssistant struct {
	started chan string
	release chan struct{}
	reply   string
}

func (a *blockingAssistant) Ask(ctx context.Context, prompt string) (string, error) {
	a.started <- prompt
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-a.release:
		return a.reply, nil
	}
}

type discordLogRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *discordLogRecorder) logf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *discordLogRecorder) contains(parts ...string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.lines {
		matched := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func (r *discordLogRecorder) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
}

// hasClearRequest reports whether a PATCH with an empty components array (the
// remove-buttons payload) was sent.
func (f *fakeDCSchedulerServer) hasClearRequest() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.sends {
		comps, ok := p["components"]
		if !ok {
			continue
		}
		if arr, ok := comps.([]any); ok && len(arr) == 0 {
			return true
		}
	}
	return false
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

func (p *syncPlacer) CancelOrder(ctx context.Context, symbol, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.CancelOrder(ctx, symbol, orderID)
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

func (p *blockPlacer) CancelOrder(ctx context.Context, symbol, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fake.CancelOrder(ctx, symbol, orderID)
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

// waitFor polls cond until it holds or the deadline fires.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal(msg)
		case <-time.After(2 * time.Millisecond):
		}
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
	s := newDiscordScheduler(ctx, dc, verifier, "app-1", testChannelID, store, placer, fakeUnderlyingQuoter{})
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
	return signedInteractionRequestAt(t, priv, body, time.Now())
}

// signedInteractionRequestAt 用给定的时钟签名:校验方按 s.now() 判新鲜度,
// 冻结时钟(openMarketNow)的测试必须传同一时钟,否则 401。
func signedInteractionRequestAt(t *testing.T, priv ed25519.PrivateKey, body []byte, now time.Time) *http.Request {
	t.Helper()
	ts := strconv.FormatInt(now.Unix(), 10)
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
	if title := header["title"]; title != "🔴 模拟盘 · 📌 信号 #7 · US.AAPL · 卖出认沽 (SELL PUT) · ⚙️ 固化策略" {
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
	if _, ok := underlying["title"]; ok || underlying["description"] != "```\n标的  AAPL\n现价  248.50\n缺口  -300 股\n目标  4,700 / 持仓 5,000\n```" {
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
	if title := discordEmbedAt(t, embeds, 0)["title"]; title != "🔴 模拟盘 · 📌 信号 #8 · US.AAPL · BUY · ⚙️ 固化策略" {
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

// TestDiscordPushRejectedActionSilentlySkips: REJECTED 已落库(无 LLM_REVIEW 行)
// 时静默推进游标不推卡(2026-08-14 老板指令:LLM 审核决定是否推送)。
func TestDiscordPushRejectedActionSilentlySkips(t *testing.T) {
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
		t.Fatal("rejected signal must not retry (cursor advances)")
	}
	if fake.sendCount() != 0 {
		t.Fatalf("rejected signal sends = %d, want 0 (silent skip)", fake.sendCount())
	}
}

// TestDiscordPushRejectedVerdictSilentlySkips: LLM_REVIEW 已落库但裁决非 APPROVE
// 时同样静默推进游标(verdictOf != "APPROVE" 分支)。
func TestDiscordPushRejectedVerdictSilentlySkips(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(11, "US.AAPL", now)
	store.reviews[11] = &wheelstore.ActionRecord{Details: map[string]any{
		"verdict": "REJECT",
		"reasons": []any{"risk limit"},
	}}
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("non-APPROVE review must not retry (cursor advances)")
	}
	if fake.sendCount() != 0 {
		t.Fatalf("non-APPROVE signal sends = %d, want 0 (silent skip)", fake.sendCount())
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

// TestDiscordPushRetriesOnCreateMessageFailure: 769 实测根因回归——APPROVE 卡
// 片推送瞬时失败(create message: request failed)不得永久丢卡,必须保持游标
// 下轮重推;API 恢复后卡片送达且不再 retry。
func TestDiscordPushRetriesOnCreateMessageFailure(t *testing.T) {
	fake, _ := startFakeDC(t)
	fake.failCreate = 1 // first push attempt hits a transient API failure
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(7, "US.AAPL", now)
	store.reviews[7] = &wheelstore.ActionRecord{Details: map[string]any{"verdict": "APPROVE"}}
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); !retry {
		t.Fatal("pushSignalDiscord must retry (hold cursor) when create message fails")
	}
	if fake.sendCount() != 0 {
		t.Fatalf("failed attempt recorded a send; sends = %d", fake.sendCount())
	}
	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("pushSignalDiscord must not retry after the push succeeds")
	}
	if fake.sendCount() != 1 {
		t.Fatalf("successful sends = %d; want 1", fake.sendCount())
	}
}

func TestDiscordPushSkipsDismissedSignal(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	store.dismissed["US.AAPL|2026-08-12"] = true
	sig := signalFixture(12, "US.AAPL", now)
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("dismissed signal must skip permanently")
	}
	if fake.sendCount() != 0 {
		t.Fatalf("dismissed signal pushed; sends = %d", fake.sendCount())
	}
}

func TestDiscordPushSkipsStaleMissingReview(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	old := now.Add(-2 * signalFreshWindow)
	store := newFakeTGStore()
	sig := signalFixture(13, "US.AAPL", old)
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("stale signal without review must skip permanently")
	}
	if fake.sendCount() != 0 {
		t.Fatalf("stale signal pushed; sends = %d", fake.sendCount())
	}
}

// TestDiscordPushGateRetryWindowHoldsCursor: 772 实测回归——LLM gate 失败后
// runner 窗口内同步重试(sleep 3s + 300s http.Client 超时),重试成功会落正常
// 审核记录;FAILED 新鲜(窗口内)时推送器必须保持游标(return true),不能 skip
// 推进游标导致重试成功也丢卡(2026-08-14: 772 17:11:57 skip → 17:14:25 重试
// 成功落 REJECTED,卡片不推)。
func TestDiscordPushGateRetryWindowHoldsCursor(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(20, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 20, Action: "LLM_REVIEW_FAILED", Actor: "llm:test",
		CreatedAt: now.Add(-time.Minute),
	})
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); !retry {
		t.Fatal("fresh FAILED must hold the cursor (gate retry in progress)")
	}
	if fake.sendCount() != 0 {
		t.Fatalf("FAILED signal pushed; sends = %d", fake.sendCount())
	}
}

// TestDiscordPushGateRetryWindowExpiredSkips: FAILED 超过重试窗口(6 分钟)仍无
// 审核记录 → 重试不可能再成功,与旧语义一致永久跳过(游标推进)。
func TestDiscordPushGateRetryWindowExpiredSkips(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(21, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 21, Action: "LLM_REVIEW_FAILED", Actor: "llm:test",
		CreatedAt: now.Add(-7 * time.Minute),
	})
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("stale FAILED beyond the gate retry window must skip permanently (cursor advances)")
	}
	// 超窗跳过不再静默:发运营告警 embed(无按钮卡片)。
	if fake.sendCount() != 1 {
		t.Fatalf("sends = %d; want 1 ops alert", fake.sendCount())
	}
	payload := fake.lastSend(t)
	if _, ok := payload["components"]; ok {
		t.Fatalf("alert must not carry confirm buttons: %#v", payload)
	}
	if title := lastEmbed(t, fake)["title"]; title != "⚠️ LLM 审核门故障" {
		t.Fatalf("alert title = %#v; want ⚠️ LLM 审核门故障", title)
	}
}

// TestDiscordPushGateAlertFiresWithFields: 超窗永久跳过的 FAILED 必须发运营告警
// embed,字段完整(标的/信号/错误原因/失败时间/影响)——08-18 欠费 132 次失败静默
// 吞掉,人工处置链路断了也无感知。
func TestDiscordPushGateAlertFiresWithFields(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(31, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 31, Action: "LLM_REVIEW_FAILED", Actor: "llm:deepseek",
		CreatedAt: now.Add(-7 * time.Minute),
		Details:   map[string]any{"error": "llmreview: status 402 Payment Required: Insufficient Balance"},
	})
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("stale FAILED must skip permanently")
	}
	embed := lastEmbed(t, fake)
	if embed["title"] != "⚠️ LLM 审核门故障" {
		t.Fatalf("title = %#v", embed["title"])
	}
	if color := embed["color"]; color != float64(discord.ColorAlert) {
		t.Fatalf("color = %v; want alert red", color)
	}
	fields, _ := embed["fields"].([]any)
	byName := map[string]string{}
	for _, f := range fields {
		field, _ := f.(map[string]any)
		name, _ := field["name"].(string)
		value, _ := field["value"].(string)
		byName[name] = value
	}
	for name, want := range map[string]string{
		"标的":   "US.AAPL",
		"信号":   "#31",
		"错误类别": "402",
		"错误原因": "llmreview: status 402 Payment Required: Insufficient Balance",
		"失败时间": "2026-08-19 09:53:00",
		"影响":   "信号推送被跳过,人工处置链路中断",
	} {
		if byName[name] != want {
			t.Fatalf("field %q = %q; want %q", name, byName[name], want)
		}
	}
}

// TestDiscordPushGateAlertThrottled: 同类错误冷却窗口内不重复推(欠费/5xx 是
// 全局性问题,每个失败信号都触发;节流防告警风暴)。
func TestDiscordPushGateAlertThrottled(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(32, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 32, Action: "LLM_REVIEW_FAILED", Actor: "llm:deepseek",
		CreatedAt: now.Add(-7 * time.Minute),
		Details:   map[string]any{"error": "llmreview: status 402 Payment Required: Insufficient Balance"},
	})
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)
	logs := &discordLogRecorder{}
	s.logf = logs.logf

	// 第一次:触发告警。
	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("first FAILED must skip permanently")
	}
	if fake.sendCount() != 1 {
		t.Fatalf("first alert sends = %d; want 1", fake.sendCount())
	}
	// 第二次(另一信号,同类错误,冷却窗口内):跳过卡片但不再发告警。
	sig2 := signalFixture(33, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 33, Action: "LLM_REVIEW_FAILED", Actor: "llm:deepseek",
		CreatedAt: now.Add(-7 * time.Minute),
		Details:   map[string]any{"error": "llmreview: status 402 Payment Required: Insufficient Balance"},
	})
	if retry := s.pushSignalDiscord(context.Background(), *sig2); retry {
		t.Fatal("second FAILED must skip permanently")
	}
	if fake.sendCount() != 1 {
		t.Fatalf("throttled sends = %d; want 1 (same category within cooldown)", fake.sendCount())
	}
	if !logs.contains("402 alert throttled") {
		t.Fatalf("missing throttle log:\n%s", logs.joined())
	}
}

// TestDiscordPushGateAlertDifferentCategoryFires: 冷却只按同类错误分组,不同
// 类别(如 402 → 5xx)仍应立即告警。
func TestDiscordPushGateAlertDifferentCategoryFires(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(34, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 34, Action: "LLM_REVIEW_FAILED", Actor: "llm:deepseek",
		CreatedAt: now.Add(-7 * time.Minute),
		Details:   map[string]any{"error": "llmreview: status 402 Payment Required"},
	})
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)
	s.pushSignalDiscord(context.Background(), *sig)
	if fake.sendCount() != 1 {
		t.Fatalf("402 alert sends = %d; want 1", fake.sendCount())
	}
	sig2 := signalFixture(35, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 35, Action: "LLM_REVIEW_FAILED", Actor: "llm:deepseek",
		CreatedAt: now.Add(-7 * time.Minute),
		Details:   map[string]any{"error": "llmreview: status 502 Bad Gateway: upstream down"},
	})
	s.pushSignalDiscord(context.Background(), *sig2)
	if fake.sendCount() != 2 {
		t.Fatalf("5xx alert sends = %d; want 2 (different category not throttled)", fake.sendCount())
	}
}

// TestDiscordPushGateAlertSendFailureDoesNotThrottle: 首次告警发送失败
// (Discord 瞬时故障)不占冷却窗口——稍后同类错误重试仍可触发(评审 P2,否则
// 首次失败也静默冷却 30min 不再重发)。
func TestDiscordPushGateAlertSendFailureDoesNotThrottle(t *testing.T) {
	fake, _ := startFakeDC(t)
	fake.failCreate = 1 // first alert create fails
	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(36, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 36, Action: "LLM_REVIEW_FAILED", Actor: "llm:deepseek",
		CreatedAt: now.Add(-7 * time.Minute),
		Details:   map[string]any{"error": "llmreview: status 402 Payment Required: Insufficient Balance"},
	})
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("stale FAILED must skip permanently even when alert send fails")
	}
	if fake.sendCount() != 0 {
		t.Fatalf("failed alert sends = %d; want 0 (create failed)", fake.sendCount())
	}
	// 同类错误再触发:上次失败不占冷却,这次发送成功。
	sig2 := signalFixture(37, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 37, Action: "LLM_REVIEW_FAILED", Actor: "llm:deepseek",
		CreatedAt: now.Add(-7 * time.Minute),
		Details:   map[string]any{"error": "llmreview: status 402 Payment Required: Insufficient Balance"},
	})
	if retry := s.pushSignalDiscord(context.Background(), *sig2); retry {
		t.Fatal("second FAILED must skip permanently")
	}
	if fake.sendCount() != 1 {
		t.Fatalf("sends = %d; want 1 (send failure must not consume cooldown)", fake.sendCount())
	}
}

func TestClassifyLLMError(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{"llmreview: status 402 Payment Required: Insufficient Balance", "402"},
		{"llmreview: status 500 Internal Server Error: boom", "5xx"},
		{"llmreview: status 502 Bad Gateway: upstream", "5xx"},
		{"llmreview: status 503 Service Unavailable: overload", "5xx"},
		{"llmreview: request: Post \"https://api.deepseek.com/chat/completions\": context deadline exceeded", "timeout"},
		{"llmreview: request: Post \"https://api.deepseek.com\": dial tcp: lookup api.deepseek.com: i/o timeout", "timeout"},
		{"llmreview: unexpected verdict \"MAYBE\"", "verdict"},
		{"dial tcp: connection refused", "other"},
		{"", "other"},
	}
	for _, tc := range cases {
		if got := classifyLLMError(tc.reason); got != tc.want {
			t.Errorf("classifyLLMError(%q) = %q; want %q", tc.reason, got, tc.want)
		}
	}
}

// TestLLMErrorCategoryFromAction: llmErrorCategory 从 FAILED 动作的 Details 提取
// error,缺失时归 other(不 panic)。
func TestLLMErrorCategoryFromAction(t *testing.T) {
	if got := llmErrorCategory(&wheelstore.ActionRecord{Details: map[string]any{"error": "llmreview: status 402 Payment Required"}}); got != "402" {
		t.Fatalf("category = %q; want 402", got)
	}
	if got := llmErrorCategory(&wheelstore.ActionRecord{}); got != "other" {
		t.Fatalf("empty action category = %q; want other", got)
	}
	if got := llmErrorCategory(nil); got != "other" {
		t.Fatalf("nil action category = %q; want other", got)
	}
}

// TestDiscordPushGateRetrySuccessPushes: 重试成功落 LLM_REVIEW 记录后,
// LatestLLMReview 判断在 FAILED 之前 → 走正常 APPROVE 推送路径,卡片必须送达
// (772 丢卡实况的修复验收)。
func TestDiscordPushGateRetrySuccessPushes(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	store := newFakeTGStore()
	sig := signalFixture(22, "US.AAPL", now.Add(-time.Minute))
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 22, Action: "LLM_REVIEW_FAILED", Actor: "llm:test",
		CreatedAt: now.Add(-time.Minute),
	})
	store.reviews[22] = approvedReview()
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, now)

	if retry := s.pushSignalDiscord(context.Background(), *sig); retry {
		t.Fatal("signal with a recorded review after gate retry must push, not retry")
	}
	if fake.sendCount() != 1 {
		t.Fatalf("sends = %d; want 1 (review won over FAILED)", fake.sendCount())
	}
}

// TestDiscordRunPushHeartbeatLogsIdleState: 循环存活可观测性(2026-08-14 实
// 测 discord 推送静默 3.5 分钟零日志,「无信号」与「循环卡死」不可区分)。
// 空转时每 5 个 tick 必须打一行带 cursor/pending/signals 的心跳日志。
func TestDiscordRunPushHeartbeatLogsIdleState(t *testing.T) {
	fake, _ := startFakeDC(t)
	store := newFakeTGStore()
	s, _ := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, time.Now())
	logs := &discordLogRecorder{}
	s.logf = logs.logf

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.runDiscordPush(ctx, time.Millisecond) }()

	waitFor(t, func() bool { return logs.contains("push: heartbeat") }, "heartbeat log")
	if !logs.contains("push: heartbeat", "pending=false") || !logs.contains("push: heartbeat", "cursor=0") {
		t.Fatalf("heartbeat missing cursor/pending state:\n%s", logs.joined())
	}
}

func TestDiscordConfirmPlacesLimitOrder(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := openMarketNow
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
	// 处理中消息(「已记录,正在下单」)在下单成功后必须删除(老板指令 2026-08-13)。
	waitFor(t, fake.hasDeleteReply, "in-progress reply deleted after successful confirm")
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

func TestDiscordConfirmReplaceCancelsThenPlaces(t *testing.T) {
	// 改单:确认信号携带 replace 时先撤旧挂单再下新单,成功消息标注改单。
	fake, _ := startFakeDC(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.signals[7].Replace = &wheelstore.ReplaceRecord{OrderID: "206158430256", Contract: "US.AAPL260815C240000"}
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{orderID: 12345, orderIDEx: "ord-12345"}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	s.confirmOrderDiscord(context.Background(), dcInteraction("42", "wheel:7:yes"), 7)
	if placer.cancelCalls != 1 || placer.cancelID != "206158430256" {
		t.Fatalf("CancelOrder calls=%d id=%q; want 1/206158430256", placer.cancelCalls, placer.cancelID)
	}
	if placer.calls != 1 {
		t.Fatalf("PlaceOrder calls = %d; want 1 after cancel", placer.calls)
	}
	if act := store.lastAppended(t); act.Action != "CONFIRM" {
		t.Fatalf("action = %+v; want CONFIRM", act)
	}
	embed := lastEmbed(t, fake)
	desc, _ := embed["description"].(string)
	for _, want := range []string{"改单", "206158430256"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("confirm push missing %q: %s", want, desc)
		}
	}
}

func TestDiscordConfirmReplaceCancelFailureRefuses(t *testing.T) {
	// 撤旧挂单失败 = 不执行新单(旧单仍在,再下单即重复敞口)。
	fake, _ := startFakeDC(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.signals[7].Replace = &wheelstore.ReplaceRecord{OrderID: "206158430256", Contract: "US.AAPL260815C240000"}
	store.reviews[7] = approvedReview()
	placer := &fakePlacer{cancelErr: errors.New("sim cancel failed")}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	s.confirmOrderDiscord(context.Background(), dcInteraction("42", "wheel:7:yes"), 7)
	if placer.cancelCalls != 1 {
		t.Fatalf("CancelOrder calls = %d; want 1", placer.cancelCalls)
	}
	if placer.calls != 0 {
		t.Fatalf("PlaceOrder calls = %d; want 0 (cancel failed, no new order)", placer.calls)
	}
	if act := store.lastAppended(t); act.Action != "REJECTED" || act.Note != "cancel pending order failed" {
		t.Fatalf("action = %+v; want REJECTED cancel pending order failed", act)
	}
}

func TestDiscordConfirmExpiredRejected(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now.Add(-16*time.Minute))
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
	// 失败路径同样要删处理中消息(老板指令 2026-08-13)。
	waitFor(t, fake.hasDeleteReply, "in-progress reply deleted after failed confirm")
}

func TestDiscordConfirmMissingLimitPriceRejected(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := openMarketNow
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
	now := openMarketNow
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

func TestDiscordHandleInteractionAskDefersBeforeAnswer(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, priv := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())
	asker := &blockingAssistant{started: make(chan string, 1), release: make(chan struct{}), reply: "测试回答"}
	s.asker = asker
	s.allowed = parseDiscordAllowedUsers("42")
	logs := &discordLogRecorder{}
	s.logf = logs.logf

	body := []byte(`{"id":"ask-1","type":2,"token":"interaction-token","member":{"user":{"id":"42"}},"data":{"name":"ask","options":[{"name":"question","value":"你好"}]}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatalf("deferred response = %d %s", rec.Code, rec.Body.String())
	}
	select {
	case prompt := <-asker.started:
		if prompt != "你好" {
			t.Fatalf("prompt = %q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("assistant was not started")
	}
	// ack 立即回(已收到,处理中),但最终答案必须等 assistant 完成。
	waitFor(t, func() bool { return fake.sendCount() == 1 }, "ack PATCH")
	if got, _ := fake.lastSend(t)["content"].(string); !strings.Contains(got, "已收到问题") {
		t.Fatalf("ack content = %#v", got)
	}
	close(asker.release)
	waitFor(t, func() bool { return fake.sendCount() == 2 }, "assistant followup PATCH")
	if got := fake.lastSend(t)["content"]; got != "测试回答" {
		t.Fatalf("followup content = %#v", got)
	}
	waitFor(t, func() bool { return logs.contains("interaction ask-1: /ask followup sent", "truncated=false") }, "followup success log")
	for _, want := range [][]string{
		{"interaction ask-1: /ask queued", `question="你好"`},
		{"interaction ask-1: /ask started", `question="你好"`, "queue_depth="},
		{"interaction ask-1: /ask completed", "elapsed=", "answer_runes=4"},
	} {
		if !logs.contains(want...) {
			t.Fatalf("missing log parts %q in:\n%s", want, logs.joined())
		}
	}
}

func TestDiscordAskQueueFullLogsWithoutToken(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, _ := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())
	asker := &blockingAssistant{started: make(chan string, 32), release: make(chan struct{}), reply: "ok"}
	s.asker = asker
	logs := &discordLogRecorder{}
	s.logf = logs.logf

	s.queueAsk(context.Background(), &discord.Interaction{ID: "ask-running", Token: "running-secret"}, "正在处理")
	select {
	case <-asker.started:
	case <-time.After(time.Second):
		t.Fatal("first question not started")
	}
	for i := 0; i < cap(s.askCh); i++ {
		s.queueAsk(context.Background(), &discord.Interaction{ID: fmt.Sprintf("ask-%d", i), Token: "queued-secret"}, "排队")
	}
	s.queueAsk(context.Background(), &discord.Interaction{ID: "ask-full", Token: "full-secret"}, "队满")

	if !logs.contains("interaction ask-full: /ask queue-full") {
		t.Fatalf("queue-full log missing:\n%s", logs.joined())
	}
	if got := logs.joined(); strings.Contains(got, "running-secret") || strings.Contains(got, "queued-secret") || strings.Contains(got, "full-secret") {
		t.Fatalf("interaction token leaked in logs:\n%s", got)
	}
	close(asker.release)
}

func TestDiscordAskQueueSerializes(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, priv := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())
	asker := &blockingAssistant{started: make(chan string, 4), release: make(chan struct{}), reply: "回答"}
	s.asker = asker
	s.allowed = parseDiscordAllowedUsers("42")

	send := func(id, q string) {
		body := []byte(`{"id":"` + id + `","type":2,"token":"` + id + `-token","member":{"user":{"id":"42"}},"data":{"name":"ask","options":[{"name":"question","value":"` + q + `"}]}}`)
		rec := httptest.NewRecorder()
		s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	send("ask-a", "问题A")
	select {
	case got := <-asker.started:
		if got != "问题A" {
			t.Fatalf("first prompt = %q; want 问题A", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first question not started")
	}
	send("ask-b", "问题B")
	// 第二个问题必须排队:worker 未释放前不启动(只开一个进程,不并行)
	select {
	case got := <-asker.started:
		t.Fatalf("second question started before first completed: %q", got)
	case <-time.After(100 * time.Millisecond):
	}
	close(asker.release)
	select {
	case got := <-asker.started:
		if got != "问题B" {
			t.Fatalf("second prompt = %q; want 问题B", got)
		}
	case <-time.After(time.Second):
		t.Fatal("queued question never started after release")
	}
}

func TestDiscordHandleInteractionAskWhitelistRejects(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, priv := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())
	asker := &blockingAssistant{started: make(chan string, 1), release: make(chan struct{})}
	s.asker = asker
	s.allowed = parseDiscordAllowedUsers("99")

	body := []byte(`{"id":"ask-2","type":2,"token":"interaction-token","member":{"user":{"id":"42"}},"data":{"name":"ask","options":[{"name":"question","value":"你好"}]}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "没有权限") {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-asker.started:
		t.Fatal("unauthorized question reached assistant")
	default:
	}
}

func TestDiscordHandleInteractionAskEmptyWhitelistAllowsWithLog(t *testing.T) {
	fake, _ := startFakeDC(t)
	s, priv := newTestDiscordScheduler(t, fake, newFakeTGStore(), &fakePlacer{}, time.Now())
	asker := &blockingAssistant{started: make(chan string, 1), release: make(chan struct{}), reply: "ok"}
	s.asker = asker
	logs := make(chan string, 1)
	s.logf = func(format string, args ...any) { logs <- fmt.Sprintf(format, args...) }

	body := []byte(`{"id":"ask-3","type":2,"token":"interaction-token","user":{"id":"42"},"data":{"name":"ask","options":[{"name":"question","value":"你好"}]}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
	if !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatalf("response = %s", rec.Body.String())
	}
	select {
	case logLine := <-logs:
		if !strings.Contains(logLine, "backlog") || !strings.Contains(logLine, "allowing") {
			t.Fatalf("log = %q", logLine)
		}
	case <-time.After(time.Second):
		t.Fatal("empty-whitelist log missing")
	}
	close(asker.release)
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
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &syncPlacer{fake: &fakePlacer{orderID: 99, orderIDEx: "ord-99"}}
	s, priv := newTestDiscordScheduler(t, fake, store, placer, now)

	body := []byte(`{"id":"i1","type":3,"channel_id":"chan-1","member":{"user":{"id":"42"}},"data":{"custom_id":"wheel:7:yes"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequestAt(t, priv, body, now))
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

// TestDiscordHandleInteractionClearsButtonsOnPress: 老板指令 2026-08-13 —
// 无论按哪个按钮(yes/no/dismiss),收到交互后都删掉卡片按钮(按钮在 = 未
// 处理,按钮没了 = 已处理)。
func TestDiscordHandleInteractionClearsButtonsOnPress(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[7] = signalFixture(7, "US.AAPL", now)
	store.reviews[7] = approvedReview()
	placer := &syncPlacer{fake: &fakePlacer{orderID: 99, orderIDEx: "ord-99"}}
	s, priv := newTestDiscordScheduler(t, fake, store, placer, now)

	body := []byte(`{"id":"i1","type":3,"channel_id":"chan-1","member":{"user":{"id":"42"}},"data":{"custom_id":"wheel:7:yes"},"message":{"id":"m7","channel_id":"chan-1"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequestAt(t, priv, body, now))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	deadline := time.After(5 * time.Second)
	for !fake.hasClearRequest() {
		select {
		case <-deadline:
			t.Fatal("clear-components PATCH never arrived after yes press")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// TestDiscordHandleInteractionNoClearsButtons: no 分支同样删按钮(响应后异步)。
func TestDiscordHandleInteractionNoClearsButtons(t *testing.T) {
	fake, _ := startFakeDC(t)
	store := newFakeTGStore()
	s, priv := newTestDiscordScheduler(t, fake, store, &fakePlacer{}, time.Now())

	body := []byte(`{"id":"i2","type":3,"channel_id":"chan-1","member":{"user":{"id":"42"}},"data":{"custom_id":"wheel:9:no"},"message":{"id":"m9","channel_id":"chan-1"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequest(t, priv, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	deadline := time.After(5 * time.Second)
	for !fake.hasClearRequest() {
		select {
		case <-deadline:
			t.Fatal("clear-components PATCH never arrived after no press")
		case <-time.After(2 * time.Millisecond):
		}
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

func TestDiscordHandleInteractionNoConfirmedCancelsOrder(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[9] = signalFixture(9, "US.AAPL", now)
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 9, Action: "CONFIRM", Actor: "discord:42",
		Details: map[string]any{"order_id": float64(12345)},
	})
	placer := &fakePlacer{}
	s, priv := newTestDiscordScheduler(t, fake, store, placer, now)

	body := []byte(`{"id":"i2","type":3,"channel_id":"chan-1","member":{"user":{"id":"42"}},"data":{"custom_id":"wheel:9:no"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequestAt(t, priv, body, now))
	if placer.cancelCalls != 1 || placer.cancelID != "12345" {
		t.Fatalf("CancelOrder calls=%d id=%q; want 1/12345", placer.cancelCalls, placer.cancelID)
	}
	act := store.lastAppended(t)
	if act.Action != "NO" || act.Note != "撤单成功 订单号 12345" {
		t.Fatalf("action = %+v; want NO 撤单成功", act)
	}
	if !strings.Contains(rec.Body.String(), "已撤单 订单号 12345") {
		t.Fatalf("response = %s; want 已撤单 toast", rec.Body.String())
	}
}

func TestDiscordHandleInteractionNoCancelFailureTellsManual(t *testing.T) {
	fake, _ := startFakeDC(t)
	now := openMarketNow
	store := newFakeTGStore()
	store.signals[9] = signalFixture(9, "US.AAPL", now)
	store.appended = append(store.appended, wheelstore.ActionRecord{
		SignalID: 9, Action: "CONFIRM", Actor: "discord:42",
		Details: map[string]any{"order_id": float64(12345)},
	})
	placer := &fakePlacer{cancelErr: errors.New("sim cancel failed")}
	s, priv := newTestDiscordScheduler(t, fake, store, placer, now)

	body := []byte(`{"id":"i2","type":3,"channel_id":"chan-1","member":{"user":{"id":"42"}},"data":{"custom_id":"wheel:9:no"}}`)
	rec := httptest.NewRecorder()
	s.handleInteraction(rec, signedInteractionRequestAt(t, priv, body, now))
	act := store.lastAppended(t)
	if act.Action != "NO" || !strings.Contains(act.Note, "请手动在模拟盘撤单") {
		t.Fatalf("action = %+v; want NO with manual-cancel note", act)
	}
	if !strings.Contains(rec.Body.String(), "撤单失败") {
		t.Fatalf("response = %s; want 撤单失败 toast", rec.Body.String())
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
		{"assistant.discord.allowed_user_ids", "42, 43"},
		{"assistant.claude.cli_path", "/fake/claude"},
		{"assistant.claude.api_key", "fake-key"},
	} {
		if err := cfg.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := startDiscordScheduler(context.Background(), nil, futu.EnvSim)
	if err != nil {
		t.Fatalf("configured scheduler: %v", err)
	}
	if ds == nil {
		t.Fatal("scheduler is nil; want fully wired")
	}
	claude, ok := ds.asker.(*claudeAssistant)
	if ds.channelID != "chan-1" || ds.dc == nil || ds.verifier == nil || !ok || claude.cliPath != "/fake/claude" || claude.apiKey != "fake-key" {
		t.Fatalf("scheduler = %+v; want fully wired", ds)
	}
	if _, ok := ds.allowed["42"]; !ok || len(ds.allowed) != 2 {
		t.Fatalf("allowed users = %#v", ds.allowed)
	}
}

func TestDiscordWatchFillPushesFill(t *testing.T) {
	fake, _ := startFakeDC(t)
	store := newFakeTGStore()
	now := openMarketNow
	placer := &fakePlacer{orderIDEx: "ord-1", statusCode: int32(trdcommon.OrderStatus_OrderStatus_Filled_All)}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, now)

	s.watchFillDiscord(context.Background(), 7, "US.AAPL", "buy", 100, 457.4, "ord-1", 12345)
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

// TestDiscordWatchFillCancelsAtMarketClose: 收盘订单立即无理由取消(老板
// 指令 2026-08-13)——市场已收盘时未成交挂单立刻撤单并推送 embed。
func TestDiscordWatchFillCancelsAtMarketClose(t *testing.T) {
	fake, _ := startFakeDC(t)
	store := newFakeTGStore()
	placer := &fakePlacer{orderIDEx: "ord-1"}
	s, _ := newTestDiscordScheduler(t, fake, store, placer, closedMarketNow)

	s.watchFillDiscord(context.Background(), 7, "US.AAPL", "sell", 1, 1.7, "ord-1", 206158430256)
	if placer.cancelCalls != 1 || placer.cancelID != "206158430256" {
		t.Fatalf("CancelOrder calls=%d id=%q; want 1/206158430256 at close", placer.cancelCalls, placer.cancelID)
	}
	embed := lastEmbed(t, fake)
	if !strings.Contains(embed["title"].(string), "已撤单") || !strings.Contains(embed["description"].(string), "市场收盘") {
		t.Fatalf("cancel embed = %#v; want 已撤单 + 市场收盘", embed)
	}
	if a := store.lastAppended(t); a.Action != "NO" || !strings.Contains(a.Note, "市场收盘") {
		t.Fatalf("action = %+v; want NO with 市场收盘 note", a)
	}
}
