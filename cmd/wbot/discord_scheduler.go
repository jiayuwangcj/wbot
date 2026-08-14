package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jiayu/wbot/internal/discord"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/wheelrun"
	"github.com/jiayu/wbot/internal/wheelstore"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

// discordPushInterval is the wheel ALERT poll cadence (same as telegram).
const discordPushInterval = 30 * time.Second

// discordScheduler pushes wheel ALERT signals to the configured Discord
// channel and disposes button interactions. It mirrors the Telegram confirm
// loop's semantics (same store gates, same limit-order discipline) with an
// independent entry point: interactions arrive over POST
// /v1/discord/interactions (Ed25519-verified) instead of getUpdates, so the
// Telegram path stays untouched.
type discordScheduler struct {
	ctx       context.Context // serve lifetime, for async confirm/fill work
	dc        *discord.Client
	verifier  *discord.Verifier
	appID     string // reserved for interaction follow-up webhooks (doc/tasks/2026-08-12-discord-channel.md)
	channelID string
	store     wheelstore.SignalRepository
	orders    wheelOrderPlacer
	quoter    underlyingQuoter
	now       func() time.Time
	logf      func(format string, a ...any)
	asker     assistant
	allowed   map[string]struct{}
	askCh     chan askRequest // FIFO 问题队列;单 worker 串行处理(老板指令 2026-08-13)
	confirmMu sync.Mutex      // one confirm at a time: dedup is HasAction→AppendAction across a network PlaceOrder
	// watchEvery/watchReport 与 telegram 侧同语义(收盘/异常立即撤单,
	// 老板指令 2026-08-13);测试可缩短。
	watchEvery  time.Duration
	watchReport time.Duration
	// missingWarnAfter 与 telegram 侧同语义:订单在券商端连续查询不到
	// missingWarnAfter 轮 → 警示 + 尝试撤单(2026-08-14 美股期权 stub 教训)。
	missingWarnAfter int
}

// askRequest is one queued assistant question. 所有 /ask 都 append 进队列,
// 只有一个 worker goroutine 消费——同一时刻只开一个 claude CLI 进程
// (并发 -p 会互等 CLI/网关锁,120s 内超时被杀,实测第二个问题 context
// deadline exceeded)。
type askRequest struct {
	in       *discord.Interaction
	question string
}

func newDiscordScheduler(ctx context.Context, dc *discord.Client, verifier *discord.Verifier, appID, channelID string, store wheelstore.SignalRepository, orders wheelOrderPlacer, quoter underlyingQuoter) *discordScheduler {
	s := &discordScheduler{
		ctx: ctx, dc: dc, verifier: verifier, appID: appID, channelID: channelID,
		store: store, orders: orders, quoter: quoter,
		askCh:            make(chan askRequest, 16),
		now:              time.Now,
		watchEvery:       30 * time.Second,
		watchReport:      2 * time.Minute,
		missingWarnAfter: 4, // ≈2 分钟(30s×4),与 telegram 侧一致
		logf: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, "discord: "+format+"\n", a...)
		},
	}
	go s.askWorker(ctx)
	return s
}

// startDiscordScheduler loads the discord credentials from ~/.wbot/wbot.conf
// and returns the scheduler; missing config logs once and returns nil (the
// serve startup degrade is identical to telegram's). The caller registers the
// interactions endpoint and runs the push loop only when it is non-nil.
func startDiscordScheduler(ctx context.Context, database *sql.DB, env futu.Env) (*discordScheduler, error) {
	cfg, err := openTelegramConfig()
	if err != nil {
		return nil, fmt.Errorf("discord: config: %w", err)
	}
	appID, appSet, err := cfg.Lookup("credentials.discord.app_id")
	if err != nil {
		return nil, fmt.Errorf("discord: app_id: %w", err)
	}
	pubKey, keySet, err := cfg.Lookup("credentials.discord.public_key")
	if err != nil {
		return nil, fmt.Errorf("discord: public_key: %w", err)
	}
	token, tokenSet, err := cfg.Lookup("credentials.discord.bot_token")
	if err != nil {
		return nil, fmt.Errorf("discord: bot_token: %w", err)
	}
	channelRaw, channelSet, err := cfg.Lookup("credentials.discord.channel_id")
	if err != nil {
		return nil, fmt.Errorf("discord: channel_id: %w", err)
	}
	if !appSet || !keySet || !tokenSet || !channelSet ||
		strings.TrimSpace(appID) == "" || strings.TrimSpace(pubKey) == "" ||
		strings.TrimSpace(token) == "" || strings.TrimSpace(channelRaw) == "" {
		fmt.Fprintf(os.Stderr, "discord: not configured (set credentials.discord.app_id, credentials.discord.public_key, credentials.discord.bot_token and credentials.discord.channel_id via the admin wizard, then restart serve --telegram-run)\n")
		return nil, nil
	}
	verifier, err := discord.NewVerifier(pubKey)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	dc, err := discord.New(token, strings.TrimSpace(os.Getenv("DISCORD_API_BASE_URL")), nil)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	cliPath, _, err := cfg.Lookup("assistant.claude.cli_path")
	if err != nil {
		return nil, fmt.Errorf("discord: assistant claude cli_path: %w", err)
	}
	apiKey, _, err := cfg.Lookup("assistant.claude.api_key")
	if err != nil {
		return nil, fmt.Errorf("discord: assistant claude api_key: %w", err)
	}
	allowedRaw, _, err := cfg.Lookup("assistant.discord.allowed_user_ids")
	if err != nil {
		return nil, fmt.Errorf("discord: assistant allowed_user_ids: %w", err)
	}
	s := newDiscordScheduler(ctx, dc, verifier, strings.TrimSpace(appID), strings.TrimSpace(channelRaw), wheelstore.New(database), futuOrderPlacer{addr: futuProtoAddr(), env: env, logf: orderLogf}, futuQuoter{client: futu.NewClient(resolveFutuGateway(""))})
	s.asker = newClaudeAssistant(cliPath, apiKey)
	s.allowed = parseDiscordAllowedUsers(allowedRaw)
	return s, nil
}

func parseDiscordAllowedUsers(raw string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			allowed[id] = struct{}{}
		}
	}
	return allowed
}

func (s *discordScheduler) registerAssistantCommands(ctx context.Context) error {
	return s.dc.RegisterGlobalCommands(ctx, s.appID, []discord.ApplicationCommand{{
		Name: "ask", Description: "向智能助手提问",
		Options: []discord.ApplicationCommandOption{{Type: 3, Name: "question", Description: "要问的问题", Required: true}},
	}})
}

// runDiscordPush polls new ALERT signals and pushes APPROVE-approved ones to
// the configured channel. The cursor/waterline semantics mirror the telegram
// push loop: a restart never replays history, a pending LLM review holds the
// cursor back, and a later signal never jumps a retryable prefix.
func (s *discordScheduler) runDiscordPush(ctx context.Context, interval time.Duration) error {
	var cursor int64
	handled := make(map[int64]struct{})
	for {
		var err error
		cursor, err = s.store.MaxSignalID(ctx)
		if err == nil {
			break
		}
		s.logf("push: max signal id: %v", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	tick := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		signals, err := s.store.QuerySignalsSince(ctx, "ALERT", cursor, 50)
		if err != nil {
			s.logf("push: query: %v", err)
			continue
		}
		pending := false
		for _, sig := range signals {
			if sig.ID <= cursor {
				continue
			}
			_, alreadyHandled := handled[sig.ID]
			retry := false
			if !alreadyHandled {
				retry = s.pushSignalDiscord(ctx, sig)
				if !retry {
					handled[sig.ID] = struct{}{}
				}
			}
			if retry {
				pending = true
				continue
			}
			if !pending {
				cursor = sig.ID
			}
		}
		for id := range handled {
			if id <= cursor {
				delete(handled, id)
			}
		}
		// 循环存活心跳:空转时段无事件日志,「静默=无信号」与「循环死/卡」
		// 不可区分(2026-08-14 实测 10 个 tick 零日志);每 5 个 tick 打一行
		// 状态,卡死时心跳消失,serve 日志一眼可辨。
		if tick%5 == 0 {
			s.logf("push: heartbeat cursor=%d pending=%v signals=%d", cursor, pending, len(signals))
		}
		tick++
	}
}

// pushSignalDiscord sends one structured APPROVE message with buttons when it
// survives the dismissal and LLM-approval gates. Retry semantics mirror the
// telegram loop: a review not yet recorded or a transient push failure holds
// the cursor back for the next pass (769: a failed create message was
// previously skipped permanently and the card was lost); REJECTED and
// dismissed signals skip silently (2026-08-14 老板指令:LLM 审核决定是否推送),
// and FAILED reviews beyond the gate retry window skip permanently.
func (s *discordScheduler) pushSignalDiscord(ctx context.Context, sig wheelstore.SignalRecord) (retry bool) {
	if dismissed, err := s.store.IsDismissed(ctx, sig.Symbol, utcDate(s.now())); err != nil {
		s.logf("push: %s signal=%d: dismissed check: %v", sig.Symbol, sig.ID, err)
		return true
	} else if dismissed {
		s.logf("push: %s signal=%d: dismissed for today, skip", sig.Symbol, sig.ID)
		return false
	}
	review, err := s.store.LatestLLMReview(ctx, sig.ID)
	if err != nil {
		if !errors.Is(err, wheelstore.ErrNotFound) {
			s.logf("push: %s signal=%d: review lookup: %v", sig.Symbol, sig.ID, err)
			return true
		}
		if rejected, herr := s.store.HasAction(ctx, sig.ID, "REJECTED"); herr != nil {
			s.logf("push: %s signal=%d: rejected check: %v", sig.Symbol, sig.ID, herr)
			return true
		} else if rejected {
			// 2026-08-14 老板指令:LLM 审核决定是否推送,REJECTED 静默不推卡(审计已落库)。
			s.logf("push: %s signal=%d: LLM review REJECTED; silent skip", sig.Symbol, sig.ID)
			return false
		}
		// LLM_REVIEW_FAILED: 审核请求失败(网络/DNS/超时)而非模型裁决,不得
		// 当「模型拒绝」推卡片(2026-08-13: signal 741 DNS 超时曾被冒充
		// REJECTED;失败原因在 DB 审计可查)。runner 失败后 sleep 3s 同步重试
		// 一次(300s http.Client 超时, internal/llmreview/llmreview.go:78):
		// 重试成功会落正常 LLM_REVIEW 记录,FAILED 直接 skip 推进游标则重试
		// 成功也丢卡(2026-08-14: 772/764 实锤)。
		// 故窗口内保持游标等下轮复查,窗口外仍无审核记录才永久跳过。
		if failed, ferr := s.store.HasAction(ctx, sig.ID, "LLM_REVIEW_FAILED"); ferr != nil {
			s.logf("push: %s signal=%d: failed check: %v", sig.Symbol, sig.ID, ferr)
			return true
		} else if failed {
			failedAt, aerr := s.store.LatestAction(ctx, sig.ID, "LLM_REVIEW_FAILED")
			if aerr != nil {
				s.logf("push: %s signal=%d: failed action lookup: %v", sig.Symbol, sig.ID, aerr)
				return true
			}
			if s.now().Sub(failedAt.CreatedAt) < llmReviewRetryWindow {
				s.logf("push: %s signal=%d: LLM review failed; gate retry window open, will re-check", sig.Symbol, sig.ID)
				return true
			}
			s.logf("push: %s signal=%d: LLM review failed beyond retry window; skip push", sig.Symbol, sig.ID)
			return false
		}
		if s.now().Sub(sig.CreatedAt) > signalFreshWindow {
			s.logf("push: %s signal=%d: not pushed (no LLM review within freshness window)", sig.Symbol, sig.ID)
			return false
		}
		s.logf("push: %s signal=%d: LLM review not yet recorded, will retry", sig.Symbol, sig.ID)
		return true
	}
	if verdictOf(review) != "APPROVE" {
		// 2026-08-14 老板指令:LLM 审核决定是否推送,非 APPROVE 静默推进游标。
		s.logf("push: %s signal=%d: LLM review %s; silent skip", sig.Symbol, sig.ID, verdictOf(review))
		return false
	}
	embeds, err := signalDiscordEmbeds(&sig, underlyingName(ctx, s.quoter, sig.Symbol), reviewReasons(review), s.now())
	if err != nil {
		s.logf("push: %s signal=%d: %v", sig.Symbol, sig.ID, err)
		return false
	}
	yesLabel := "✅ 下单"
	if sig.ClosePosition {
		yesLabel = "✅ 买回平仓"
	}
	msg := discord.Message{
		Embeds: embeds,
		Components: [][]discord.Button{{
			{Type: 2, Style: 3, Label: yesLabel, CustomID: fmt.Sprintf("wheel:%d:yes", sig.ID)},
			{Type: 2, Style: 4, Label: "❌ 拒绝", CustomID: fmt.Sprintf("wheel:%d:no", sig.ID)},
			{Type: 2, Style: 2, Label: "⚠️ Dismiss", CustomID: fmt.Sprintf("wheel:%d:dismiss", sig.ID)},
		}},
	}
	if err := s.pushEmbedDiscord(ctx, msg); err != nil {
		s.logf("push: %s signal=%d: %v", sig.Symbol, sig.ID, err)
		return true
	}
	return false
}

const discordCodeLabelWidth = 6

// signalInfoBlocks renders the candidate/quote/inventory detail lines of the
// APPROVE card (2026-08-14 起推送仅 APPROVE 使用)。无可用候选时返回 err,
// 调用方降级为只推状态卡。与 signalDiscordEmbeds 共享
// firstCandidate/isStockDirection 语义,保证显示与下单执行一致。
func signalInfoBlocks(sig *wheelstore.SignalRecord, underlying string) ([][]string, error) {
	c, err := orderFacts(sig)
	if err != nil {
		return nil, err
	}
	// 平仓卡片块(2026-08-15 评审 P1-B):buy 语义,数量/限价取 close 载荷。
	if sig.ClosePosition {
		blocks := [][]string{{
			discordCodeRow("买回平仓", valueOrDash(c.Code)),
			discordCodeRow("数量", fmt.Sprintf("%s 张", commaInt(int64(c.Quantity)))),
			discordCodeRow("限价", fmt.Sprintf("%s (买回成本)", positiveDecimal(c.Quote.Last))),
			fmt.Sprintf("行权  %s  Δ %s", positiveDecimal(c.Quote.Strike), nonZeroDecimal(c.Quote.Delta)),
			fmt.Sprintf("到期  %s  IV %s", shortExpiry(c.Quote.Expiry), positiveDecimal(c.Quote.ImpliedVol)),
			fmt.Sprintf("报价  %s  OI %s", bidAsk(c.Quote.Bid, c.Quote.Ask), positiveCount(c.Quote.OpenInterest)),
		}}
		blocks = append(blocks, []string{
			discordCodeRow("标的", underlyingLabel(underlying, sig.Symbol)),
			discordCodeRow("现价", discordPrice(sig.Inventory.CurrentPrice)),
			discordCodeRow("缺口", discordShares(sig.Inventory.InventoryGap)),
			discordCodeRow("目标", fmt.Sprintf("%s / 持仓 %s", discordCount(sig.Inventory.TargetInventory), discordCount(sig.Inventory.ActualInventory))),
		})
		return blocks, nil
	}
	unit := "张"
	if isStockDirection(c.Direction) {
		unit = "股"
	}
	blocks := [][]string{{
		discordCodeRow("候选", valueOrDash(c.Code)),
		discordCodeRow("数量", fmt.Sprintf("%s %s", commaInt(int64(c.Quantity)), unit)),
		discordCodeRow("限价", positiveDecimal(c.Quote.Last)),
	}}
	if !isStockDirection(c.Direction) {
		blocks = append(blocks, []string{
			fmt.Sprintf("行权  %s  Δ %s", positiveDecimal(c.Quote.Strike), nonZeroDecimal(c.Quote.Delta)),
			fmt.Sprintf("到期  %s  IV %s", shortExpiry(c.Quote.Expiry), positiveDecimal(c.Quote.ImpliedVol)),
			fmt.Sprintf("报价  %s  OI %s", bidAsk(c.Quote.Bid, c.Quote.Ask), positiveCount(c.Quote.OpenInterest)),
		})
	}
	blocks = append(blocks, []string{
		discordCodeRow("标的", underlyingLabel(underlying, sig.Symbol)),
		discordCodeRow("现价", discordPrice(sig.Inventory.CurrentPrice)),
		discordCodeRow("缺口", discordShares(sig.Inventory.InventoryGap)),
		discordCodeRow("目标", fmt.Sprintf("%s / 持仓 %s", discordCount(sig.Inventory.TargetInventory), discordCount(sig.Inventory.ActualInventory))),
	})
	return blocks, nil
}

// signalDiscordEmbeds renders the approved signal as compact mobile-friendly
// sections. It deliberately shares firstCandidate and isStockDirection with
// the Telegram path so display and order execution keep the same semantics.
func signalDiscordEmbeds(sig *wheelstore.SignalRecord, underlying string, reasons []string, sentAt time.Time) ([]discord.Embed, error) {
	c, err := orderFacts(sig)
	if err != nil {
		return nil, err
	}
	created := "—"
	if !sig.CreatedAt.IsZero() {
		created = sig.CreatedAt.Format("01-02 15:04")
	}
	common := func(description string) discord.Embed {
		return discord.Embed{Description: description, Color: discord.ColorApprove}
	}
	replaceLine := ""
	if sig.Replace != nil && sig.Replace.Contract != "" {
		replaceLine = fmt.Sprintf("\n↻ 改单:撤销挂单 `%s`(%s),改挂候选", discordInlineCode(sig.Replace.Contract), sig.Replace.OrderID)
	}
	title := fmt.Sprintf("🔴 模拟盘 · 📌 信号 #%d · %s · %s · %s", sig.ID, sig.Symbol, directionLabel(c.Direction), strategyBadge(sig.Strategy))
	description := fmt.Sprintf("LLM 审核 ✅ APPROVE — 候选 `%s` 已就绪,缺口方向一致%s", discordInlineCode(c.Code), replaceLine)
	if sig.ClosePosition {
		// 平仓 embed(2026-08-15 评审 P1-B):buy 语义独立标题,不复用卖向文案。
		title = fmt.Sprintf("🔴 模拟盘 · 📌 信号 #%d · %s · 买回平仓 · %s", sig.ID, sig.Symbol, strategyBadge(sig.Strategy))
		description = fmt.Sprintf("LLM 审核 ✅ APPROVE — 买回空腿 `%s`(风险降低),限价 %s%s", discordInlineCode(c.Code), positiveDecimal(c.Quote.Last), replaceLine)
	}
	embeds := []discord.Embed{{
		Author:      &discord.EmbedAuthor{Name: "🤖 Wheel Bot"},
		Title:       title,
		Description: description,
		Color:       discord.ColorApprove,
		Footer:      &discord.EmbedFooter{Text: fmt.Sprintf("配置 v%d · 信号 #%d · %s", sig.ConfigVersion, sig.ID, created)},
		Timestamp:   sentAt.UTC().Format(time.RFC3339),
	}}
	blocks, err := signalInfoBlocks(sig, underlying)
	if err != nil {
		return nil, err
	}
	for _, block := range blocks {
		embeds = append(embeds, common(discordCodeBlock(block...)))
	}
	embeds = append(embeds, common(discordReasonBullets(reasons)))
	return embeds, nil
}

func discordCodeBlock(lines ...string) string {
	return "```\n" + strings.Join(lines, "\n") + "\n```"
}

func discordCodeRow(label, value string) string {
	return label + strings.Repeat(" ", max(1, discordCodeLabelWidth-displayWidth(label))) + value
}

func discordInlineCode(value string) string {
	return strings.ReplaceAll(value, "`", "ˋ")
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func decimal(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

func positiveDecimal(value float64) string {
	if value <= 0 {
		return "—"
	}
	return decimal(value)
}

func nonZeroDecimal(value float64) string {
	if value == 0 {
		return "—"
	}
	return decimal(value)
}

func shortExpiry(raw string) string {
	expiry, parsed := expiryText(raw)
	if parsed.IsZero() {
		return valueOrDash(expiry)
	}
	return parsed.Format("01-02")
}

func bidAsk(bid, ask float64) string {
	return positiveDecimal(bid) + "/" + positiveDecimal(ask)
}

func positiveCount(value int64) string {
	if value <= 0 {
		return "—"
	}
	return commaInt(value)
}

func discordPrice(value *float64) string {
	text := priceText(value)
	if text == "-" {
		return "—"
	}
	return text
}

func discordCount(value *float64) string {
	text := countText(value)
	if text == "-" {
		return "—"
	}
	return text
}

func discordShares(value *float64) string {
	if value == nil {
		return "—"
	}
	return discordCount(value) + " 股"
}

func discordReasonBullets(reasons []string) string {
	var bullets []string
	for _, reason := range reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			bullets = append(bullets, "• "+reason)
		}
	}
	if len(bullets) == 0 {
		return "• —"
	}
	return strings.Join(bullets, "\n")
}

// pushEmbedDiscord sends one message to the configured channel.
func (s *discordScheduler) pushEmbedDiscord(ctx context.Context, msg discord.Message) error {
	if s.dc == nil || s.channelID == "" {
		return errors.New("channel not configured")
	}
	return s.dc.CreateMessage(ctx, s.channelID, msg)
}

// clearDiscordButtons strips the confirm buttons off the card that hosted the
// interaction (老板指令 2026-08-13: 无论按哪个按钮,收到后删按钮,一眼可辨
// 哪个单已处理)。交互不携带 message(如 PING)时为空操作。
func (s *discordScheduler) clearDiscordButtons(ctx context.Context, in *discord.Interaction) {
	if in == nil || in.Message == nil || in.Message.ID == "" || s.dc == nil {
		return
	}
	if err := s.dc.ClearMessageComponents(ctx, in.Message.ChannelID, in.Message.ID); err != nil {
		s.logf("interaction %s: clear buttons on message %s: %v", in.ID, in.Message.ID, err)
	}
}

// handleInteraction serves POST /v1/discord/interactions: Ed25519 verification
// first (this endpoint is reachable through the public haproxy path, so every
// failed check is 401), then PING → PONG and message components → the wheel
// confirm flow or /ask. The response is written before any background work so Discord's
// 3-second reply window is never missed.
func (s *discordScheduler) handleInteraction(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if err := s.verifier.VerifyRequest(
		r.Header.Get("X-Signature-Timestamp"),
		r.Header.Get("X-Signature-Ed25519"),
		body, s.now()); err != nil {
		s.logf("interaction: rejected: %v", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var in discord.Interaction
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "invalid interaction", http.StatusBadRequest)
		return
	}
	if in.Type == discord.TypePing {
		discord.WriteResponse(w, discord.Pong())
		return
	}
	if in.Type == discord.TypeApplicationCmd {
		s.handleApplicationCommand(w, &in)
		return
	}
	if in.Type != discord.TypeMessageComponent || in.Data == nil {
		http.Error(w, "unsupported interaction", http.StatusBadRequest)
		return
	}
	// Buttons disappear as soon as a verified component interaction arrives.
	defer func() { go s.clearDiscordButtons(s.ctx, &in) }()
	signalID, action, err := parseCallbackData(in.Data.CustomID)
	if err != nil {
		s.logf("interaction %s: malformed custom_id %q", in.ID, in.Data.CustomID)
		discord.WriteResponse(w, discord.EphemeralMessage("无效的按钮"))
		return
	}
	switch action {
	case "yes":
		discord.WriteResponse(w, discord.EphemeralMessage("已记录,正在下单"))
		go s.confirmOrderDiscord(s.ctx, &in, signalID)
	case "no":
		// 与 telegram 同语义(2026-08-13):已确认未成交单按 ❌ = 撤单,撤销
		// 模拟盘挂单并记录 NO 解除 pending-order 阻塞,保证挂单被取消。
		note, toast := "继续等待机会", "已记录,继续等待机会"
		if confirm, cerr := s.store.LatestAction(s.ctx, signalID, "CONFIRM"); cerr == nil {
			var oid uint64
			switch v := confirm.Details["order_id"].(type) {
			case float64: // JSONB 落库后数值读出为 float64
				oid = uint64(v)
			case uint64:
				oid = v
			}
			symbol := ""
			if sig, gerr := s.store.GetSignal(s.ctx, signalID); gerr == nil {
				symbol = sig.Symbol
			}
			switch {
			case oid == 0 || symbol == "":
				note, toast = "撤单失败:缺少订单号或标的(请手动在模拟盘撤单)", "缺少订单信息,请手动撤单"
			default:
				if cerr := s.orders.CancelOrder(s.ctx, symbol, strconv.FormatUint(oid, 10)); cerr != nil {
					s.logf("interaction %s: cancel order: %v", in.ID, cerr)
					note, toast = fmt.Sprintf("撤单失败:%v(请手动在模拟盘撤单)", cerr), "撤单失败,请手动撤单"
				} else {
					note, toast = fmt.Sprintf("撤单成功 订单号 %d", oid), fmt.Sprintf("已撤单 订单号 %d", oid)
				}
			}
		}
		if _, err := s.store.AppendAction(s.ctx, wheelstore.ActionRecord{SignalID: signalID, Action: "NO", Actor: discordActor(&in), Note: note}); err != nil {
			s.logf("interaction %s: no: %v", in.ID, err)
			discord.WriteResponse(w, discord.EphemeralMessage("记录失败"))
			return
		}
		discord.WriteResponse(w, discord.EphemeralMessage(toast))
		s.noticeDiscord(s.ctx, "❌ 已拒绝", fmt.Sprintf("信号 #%d · %s\n%s", signalID, s.now().Format("2006-01-02 15:04:05"), note))
	case "dismiss":
		s.recordDismissDiscord(w, &in, signalID)
	default:
		discord.WriteResponse(w, discord.EphemeralMessage("未知操作"))
	}
}

func (s *discordScheduler) handleApplicationCommand(w http.ResponseWriter, in *discord.Interaction) {
	if in.Data == nil || in.Data.Name != "ask" {
		discord.WriteResponse(w, discord.EphemeralMessage("不支持的命令"))
		return
	}
	userID := in.UserID()
	if len(s.allowed) == 0 {
		s.logf("assistant: allowed_user_ids empty; allowing user %q (backlog: require owner whitelist)", userID)
	} else if _, ok := s.allowed[userID]; !ok {
		s.logf("assistant: rejected user %q outside allowed_user_ids", userID)
		discord.WriteResponse(w, discord.EphemeralMessage("你没有权限使用此命令"))
		return
	}
	question := ""
	for _, option := range in.Data.Options {
		if option.Name == "question" {
			question = strings.TrimSpace(option.Value)
			break
		}
	}
	if question == "" {
		discord.WriteResponse(w, discord.EphemeralMessage("question 参数不能为空"))
		return
	}
	discord.WriteResponse(w, discord.DeferredChannelMessage())
	go s.queueAsk(s.ctx, in, question)
}

// queueAsk ack 后把问题 append 进 FIFO 队列;askWorker 单进程串行消费。
// 老板指令(2026-08-13):① deferred 后立即回 ack,避免「正在响应」长时间
// 悬空 ② 无论多少问题都排队,同一时刻只开一个 claude CLI,不并行。
func (s *discordScheduler) queueAsk(ctx context.Context, in *discord.Interaction, question string) {
	if s.asker == nil {
		reply := "调用失败: 助手未配置"
		if err := s.dc.EditInteractionReply(ctx, s.appID, in.Token, reply); err != nil {
			s.logf("interaction %s: /ask followup: %v", in.ID, err)
		}
		return
	}
	ack := "✅ 已收到问题,正在排队处理…\n「" + truncateRunes(question, 40) + "」"
	if err := s.dc.EditInteractionReply(ctx, s.appID, in.Token, ack); err != nil {
		s.logf("interaction %s: /ask ack: %v", in.ID, err)
	}
	select {
	case s.askCh <- askRequest{in: in, question: question}:
		s.logf("interaction %s: /ask queued: question=%q", in.ID, truncateRunes(question, 80))
	default:
		// 队列满:不阻塞交互处理,明确告知稍后再试
		s.logf("interaction %s: /ask queue-full", in.ID)
		if err := s.dc.EditInteractionReply(ctx, s.appID, in.Token, "❌ 处理队列已满,请稍后再试"); err != nil {
			s.logf("interaction %s: /ask queue-full: %v", in.ID, err)
		}
	}
}

// askWorker is the single consumer of askCh: one claude CLI process at a time,
// FIFO order, Ask 自带 180s 超时。
func (s *discordScheduler) askWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.askCh:
			s.logf("interaction %s: /ask started: question=%q queue_depth=%d", req.in.ID, truncateRunes(req.question, 80), len(s.askCh))
			started := time.Now()
			answer, err := s.asker.Ask(ctx, req.question)
			elapsed := time.Since(started)
			reply := answer
			if err != nil {
				s.logf("interaction %s: /ask failed: elapsed=%s: %v", req.in.ID, elapsed, err)
				reply = "调用失败: " + err.Error()
			} else {
				s.logf("interaction %s: /ask completed: elapsed=%s answer_runes=%d", req.in.ID, elapsed, len([]rune(answer)))
			}
			truncated := len([]rune(strings.TrimSpace(reply))) > discordAssistantMaxLen
			reply = truncateAssistantReply(reply)
			if err := s.dc.EditInteractionReply(ctx, s.appID, req.in.Token, reply); err != nil {
				s.logf("interaction %s: /ask followup: %v", req.in.ID, err)
			} else {
				s.logf("interaction %s: /ask followup sent: truncated=%t", req.in.ID, truncated)
			}
		}
	}
}

// truncateRunes shortens s to at most n runes for the ack preview.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// confirmOrderDiscord is the yes path with the same semantics as the telegram
// confirm: freshness window, LLM APPROVE gate, CONFIRM dedup, limit price from
// the candidate quote (市价单禁止), sim-env placement, then the outcome is
// pushed to the channel; any refusal is recorded REJECTED.
func (s *discordScheduler) confirmOrderDiscord(ctx context.Context, in *discord.Interaction, signalID int64) {
	s.confirmMu.Lock()
	defer s.confirmMu.Unlock()
	// 「已记录,正在下单」是处理中消息:无论下单成功还是失败,异步结果一
	// 落地就删除它,避免污染聊天记录(老板指令 2026-08-13)。
	defer func() {
		if err := s.dc.DeleteInteractionReply(ctx, s.appID, in.Token); err != nil {
			s.logf("interaction %s: delete in-progress reply: %v", in.ID, err)
		}
	}()
	sig, err := s.store.GetSignal(ctx, signalID)
	if err != nil {
		s.rejectDiscord(ctx, in, signalID, "signal not found")
		return
	}
	if s.now().Sub(sig.CreatedAt) > signalFreshWindow {
		s.rejectDiscord(ctx, in, signalID, "signal expired")
		return
	}
	review, err := s.store.LatestLLMReview(ctx, signalID)
	if err != nil || verdictOf(review) != "APPROVE" {
		s.rejectDiscord(ctx, in, signalID, "llm review not approved")
		return
	}
	// 收盘闸门(老板指令 2026-08-13: 收盘订单立即无理由取消)。
	if !wheelrun.MarketIsOpen(sig.Symbol, s.now(), nil) {
		s.rejectDiscord(ctx, in, signalID, "market closed")
		return
	}
	if s.orders == nil {
		s.rejectDiscord(ctx, in, signalID, "order placer unavailable")
		return
	}
	cand, err := orderFacts(sig)
	if err != nil {
		s.rejectDiscord(ctx, in, signalID, "no usable candidate")
		return
	}
	price := cand.Quote.Last
	if price <= 0 {
		s.rejectDiscord(ctx, in, signalID, "no usable limit price")
		return
	}
	claims, ok := s.store.(wheelstore.OrderClaimRepository)
	if !ok {
		s.rejectDiscord(ctx, in, signalID, "confirm claim unavailable")
		return
	}
	claimed, err := claims.ClaimOrder(ctx, signalID, discordActor(in))
	if err != nil {
		s.rejectDiscord(ctx, in, signalID, "confirm claim failed")
		return
	}
	if !claimed {
		s.rejectDiscord(ctx, in, signalID, "already confirmed")
		return
	}
	// 改单(老板指令 2026-08-13: 允许策略调整未成交订单,改单同样需 LLM
	// 审核——已由上面 LatestLLMReview APPROVE 把关)。先撤销旧挂单再下
	// 新单;撤单失败 = 不执行新单(旧单仍在,再下单即重复敞口),拒绝并
	// 留痕让用户人工处理。
	replaced := ""
	if sig.Replace != nil && sig.Replace.OrderID != "" {
		if cerr := s.orders.CancelOrder(ctx, sig.Symbol, sig.Replace.OrderID); cerr != nil {
			s.logf("interaction %s: cancel pending order %s: %v", in.ID, sig.Replace.OrderID, cerr)
			s.rejectDiscord(ctx, in, signalID, "cancel pending order failed")
			return
		}
		replaced = sig.Replace.OrderID
		s.logf("interaction %s: cancelled pending order %s before replace signal #%d", in.ID, replaced, signalID)
	}
	orderIDEx, orderID, err := s.orders.PlaceOrder(ctx, cand.Code, cand.Side, float64(cand.Quantity), price)
	if err != nil {
		reason := "place order failed"
		if errors.Is(err, errLiveEnvNotAllowed) {
			reason = "live env not allowed"
		}
		s.logf("interaction %s: %s: %v", in.ID, reason, err)
		s.rejectDiscord(ctx, in, signalID, reason)
		return
	}
	// 订单号校验(2026-08-14 美股期权 stub 教训,与 telegram 侧同语义):
	// 网关对未获券商确认的订单返回占位 stub(order_id_ex="0"),订单从未
	// 真实生效。无真实券商订单号 = 下单未确认,按失败处理,绝不推送
	// 「已下单」。
	if orderIDEx == "" || orderIDEx == "0" || orderID == 0 {
		s.logf("interaction %s: order unconfirmed: order_id_ex=%q order_id=%d; treating as failure", in.ID, orderIDEx, orderID)
		s.rejectDiscord(ctx, in, signalID, "order unconfirmed")
		return
	}
	details := map[string]any{"order_id": orderID, "order_id_ex": orderIDEx, "symbol": cand.Code, "side": cand.Side, "qty": cand.Quantity}
	if err := claims.CompleteOrderClaim(ctx, signalID, orderID, orderIDEx, details); err != nil {
		s.logf("interaction %s: complete order claim: %v", in.ID, err)
	}
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{
		SignalID: signalID, Action: "CONFIRM", Actor: discordActor(in),
		Details: details,
	}); err != nil {
		s.logf("interaction %s: confirm record: %v", in.ID, err)
	}
	sideName := "买入"
	if cand.Side == "sell" {
		sideName = "卖出"
	}
	replaceLine := ""
	if replaced != "" {
		replaceLine = fmt.Sprintf("\n↻ 改单:已撤旧挂单 `%s`", replaced)
	}
	s.noticeDiscord(ctx, "✅ 已下单", fmt.Sprintf(
		"信号 #%d\n%s %s %s %d 股 @ 限价 %.2f\n订单号 `%s`(%d)%s\n时间 %s",
		signalID, sideName, cand.Code, cand.Side, cand.Quantity, price, orderIDEx, orderID, replaceLine,
		s.now().Format("2006-01-02 15:04:05"),
	))
	go s.watchFillDiscord(ctx, signalID, cand.Code, cand.Side, float64(cand.Quantity), price, orderIDEx, orderID)
}

// watchFillDiscord polls the placed order until it fills, cancels or the
// market closes, pushing the outcome to the channel (same semantics as the
// telegram watcher; 收盘订单和异常订单立即无理由取消, 老板指令 2026-08-13)。
func (s *discordScheduler) watchFillDiscord(ctx context.Context, signalID int64, symbol, side string, qty, price float64, orderIDEx string, orderID uint64) {
	pollEvery := s.watchEvery
	reportAfter := s.watchReport
	if pollEvery <= 0 {
		pollEvery = 30 * time.Second
	}
	if reportAfter <= 0 {
		reportAfter = 2 * time.Minute
	}
	sideName := "买入"
	if side == "sell" {
		sideName = "卖出"
	}
	started := s.now()
	reportedPending := false
	// missingWarnAfter 零值兜底(测试构造可不传)。
	missingWarnAfter := s.missingWarnAfter
	if missingWarnAfter <= 0 {
		missingWarnAfter = 4
	}
	missing := 0
	for {
		// 收盘闸门:市场已收盘,未成交挂单立即无理由撤单。
		if !wheelrun.MarketIsOpen(symbol, s.now(), nil) {
			s.cancelRestingDiscord(ctx, signalID, sideName, symbol, side, qty, price, orderIDEx, orderID, "市场收盘,挂单自动撤单")
			return
		}
		status, found, err := s.orders.OrderStatus(ctx, symbol, orderIDEx)
		if err != nil {
			s.logf("watch fill %s: %v; cancelling", orderIDEx, err)
			s.cancelRestingDiscord(ctx, signalID, sideName, symbol, side, qty, price, orderIDEx, orderID, "订单状态异常,自动撤单")
			return
		}
		if found {
			missing = 0
			switch trdcommon.OrderStatus(status) {
			case trdcommon.OrderStatus_OrderStatus_Filled_All, trdcommon.OrderStatus_OrderStatus_Filled_Part:
				s.noticeDiscord(ctx, "✅ 已成交", fmt.Sprintf(
					"信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 `%s`\n成交时间 %s",
					signalID, sideName, symbol, qty, price, orderIDEx, s.now().Format("2006-01-02 15:04:05"),
				))
				if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{
					SignalID: signalID, Action: "FILL", Actor: "system:watch",
					Details: map[string]any{"order_id_ex": orderIDEx, "status": trdcommon.OrderStatus(status).String(), "symbol": symbol, "side": side, "qty": qty, "price": price},
				}); err != nil {
					s.logf("watch fill %s: fill record: %v", orderIDEx, err)
				}
				return
			case trdcommon.OrderStatus_OrderStatus_Cancelled_Part, trdcommon.OrderStatus_OrderStatus_Cancelled_All,
				trdcommon.OrderStatus_OrderStatus_Cancelling_Part, trdcommon.OrderStatus_OrderStatus_Cancelling_All,
				trdcommon.OrderStatus_OrderStatus_SubmitFailed:
				s.noticeDiscord(ctx, "⚠️ 订单未成交("+trdcommon.OrderStatus(status).String()+")", fmt.Sprintf(
					"信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 `%s`\n时间 %s",
					signalID, sideName, symbol, qty, price, orderIDEx, s.now().Format("2006-01-02 15:04:05"),
				))
				return
			}
		} else {
			// 订单在券商端不可见(与 telegram 侧同语义,2026-08-14 美股期权
			// stub 教训):连续 missingWarnAfter 轮 → 警示 + NOTE 留痕 + 尝试
			// 撤单;撤单成功即结束,失败继续观察(订单可能晚确认)。
			missing++
			if missing >= missingWarnAfter {
				s.logf("watch fill %s: order not visible after %d polls; warning+cancel", orderIDEx, missing)
				s.noticeDiscord(ctx, "⚠️ 订单券商端未确认", fmt.Sprintf(
					"信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 `%s`\n订单在券商端查询不到,可能未生效,尝试撤单",
					signalID, sideName, symbol, qty, price, orderIDEx,
				))
				if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{
					SignalID: signalID, Action: "NOTE", Actor: "system:watch",
					Note:    "order not visible to broker; attempting cancel",
					Details: map[string]any{"order_id_ex": orderIDEx, "order_id": orderID, "symbol": symbol, "side": side, "missing_polls": missing},
				}); err != nil {
					s.logf("watch fill %s: note record: %v", orderIDEx, err)
				}
				if orderID != 0 {
					if cerr := s.orders.CancelOrder(ctx, symbol, strconv.FormatUint(orderID, 10)); cerr != nil {
						s.logf("watch fill %s: cancel unconfirmed order: %v", orderIDEx, cerr)
						s.noticeDiscord(ctx, "⚠️ 撤单失败", fmt.Sprintf(
							"信号 #%d\n订单号 `%s` · 请手动在模拟盘撤单\n错误 %v",
							signalID, orderIDEx, cerr,
						))
					} else {
						s.noticeDiscord(ctx, "❌ 已撤单", fmt.Sprintf(
							"信号 #%d\n订单号 `%s` · 订单未获券商确认,已撤单",
							signalID, orderIDEx,
						))
						return
					}
				} else {
					// 缺订单号无法撤单 → 显式提示手动撤单,不静默遗留。
					s.noticeDiscord(ctx, "⚠️ 挂单未撤(缺少订单号)", fmt.Sprintf(
						"信号 #%d\n订单号 `%s` · 请手动在模拟盘撤单",
						signalID, orderIDEx,
					))
					return
				}
			}
		}
		if !reportedPending && s.now().Sub(started) >= reportAfter {
			s.noticeDiscord(ctx, "⏳ 订单挂单中未成交", fmt.Sprintf(
				"信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 `%s`\n观察 %s",
				signalID, sideName, symbol, qty, price, orderIDEx,
				s.now().Format("2006-01-02 15:04:05"),
			))
			reportedPending = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollEvery):
		}
	}
}

// cancelRestingDiscord cancels an unfilled resting order immediately (无理由
// 撤单, 老板指令 2026-08-13) and pushes the outcome; failed cancels are a
// manual action item, never silent.
func (s *discordScheduler) cancelRestingDiscord(ctx context.Context, signalID int64, sideName, symbol, side string, qty, price float64, orderIDEx string, orderID uint64, note string) {
	if orderID == 0 {
		s.logf("watch fill %s: no numeric order id to cancel; %s", orderIDEx, note)
		s.noticeDiscord(ctx, "⚠️ 挂单未撤(缺少订单号)", fmt.Sprintf(
			"信号 #%d\n%s\n订单号 `%s` · 请手动在模拟盘撤单",
			signalID, note, orderIDEx,
		))
		return
	}
	if err := s.orders.CancelOrder(ctx, symbol, strconv.FormatUint(orderID, 10)); err != nil {
		s.logf("watch fill %s: cancel: %v", orderIDEx, err)
		s.noticeDiscord(ctx, "⚠️ 撤单失败", fmt.Sprintf(
			"信号 #%d\n%s\n%s %s %.0f 股 @ %.2f\n订单号 `%s` · 请手动在模拟盘撤单\n错误 %v",
			signalID, note, sideName, symbol, qty, price, orderIDEx, err,
		))
		return
	}
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{
		SignalID: signalID, Action: "NO", Actor: "system:watch",
		Note: note, Details: map[string]any{"order_id_ex": orderIDEx, "order_id": orderID, "symbol": symbol},
	}); err != nil {
		s.logf("watch fill %s: cancel record: %v", orderIDEx, err)
	}
	s.noticeDiscord(ctx, "❌ 已撤单", fmt.Sprintf(
		"信号 #%d · %s\n%s %s %.0f 股 @ %.2f\n订单号 `%s`\n时间 %s",
		signalID, note, sideName, symbol, qty, price, orderIDEx,
		s.now().Format("2006-01-02 15:04:05"),
	))
}

// noticeDiscord pushes one outcome embed to the channel (green for success
// titles, gray otherwise; the emoji prefix carries the state).
func (s *discordScheduler) noticeDiscord(ctx context.Context, title, desc string) {
	color := discord.ColorApprove
	if !strings.HasPrefix(title, "✅") {
		color = discord.ColorRejected
	}
	if err := s.pushEmbedDiscord(ctx, discord.Message{Embeds: []discord.Embed{{
		Title:       title,
		Description: desc,
		Color:       color,
		Timestamp:   s.now().UTC().Format(time.RFC3339),
	}}}); err != nil {
		s.logf("push: %s: %v", title, err)
	}
}

// recordDismissDiscord silences the signal's symbol for today and answers.
func (s *discordScheduler) recordDismissDiscord(w http.ResponseWriter, in *discord.Interaction, signalID int64) {
	sig, err := s.store.GetSignal(s.ctx, signalID)
	if err != nil {
		discord.WriteResponse(w, discord.EphemeralMessage("信号不存在"))
		return
	}
	if err := s.store.Dismiss(s.ctx, sig.Symbol, utcDate(s.now())); err != nil {
		s.logf("interaction %s: dismiss: %v", in.ID, err)
		discord.WriteResponse(w, discord.EphemeralMessage("记录失败"))
		return
	}
	discord.WriteResponse(w, discord.EphemeralMessage("今日不再提醒该标的"))
	s.noticeDiscord(s.ctx, "⚠️ 已忽略", fmt.Sprintf("信号 #%d · %s · %s\n今日不再提醒 %s", signalID, s.now().Format("2006-01-02 15:04:05"), sig.Symbol, sig.Symbol))
}

// rejectDiscord records REJECTED with the reason and pushes the refusal so the
// boss sees why the order failed (重要消息必须推送, 老板指令 2026-08-12).
func (s *discordScheduler) rejectDiscord(ctx context.Context, in *discord.Interaction, signalID int64, reason string) {
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{SignalID: signalID, Action: "REJECTED", Actor: discordActor(in), Note: reason}); err != nil {
		s.logf("interaction %s: rejected record: %v", in.ID, err)
	}
	symbol := fmt.Sprintf("#%d", signalID)
	if sig, err := s.store.GetSignal(ctx, signalID); err == nil {
		symbol = sig.Symbol
	}
	s.noticeDiscord(ctx, "⛔ 下单失败", fmt.Sprintf("信号 #%d · %s · %s\n%s", signalID, s.now().Format("2006-01-02 15:04:05"), symbol, reason))
}

// discordActor names the audit actor for a button press (interactions are
// already signature-verified; the user id identifies who pressed).
func discordActor(in *discord.Interaction) string {
	return fmt.Sprintf("discord:%s", in.UserID())
}
