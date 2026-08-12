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
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/discord"
	"github.com/jiayu/wbot/internal/futu"
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
	store     wheelTelegramStore
	orders    wheelOrderPlacer
	now       func() time.Time
	logf      func(format string, a ...any)
}

func newDiscordScheduler(ctx context.Context, dc *discord.Client, verifier *discord.Verifier, appID, channelID string, store wheelTelegramStore, orders wheelOrderPlacer) *discordScheduler {
	return &discordScheduler{
		ctx: ctx, dc: dc, verifier: verifier, appID: appID, channelID: channelID,
		store: store, orders: orders,
		now: time.Now,
		logf: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, "discord: "+format+"\n", a...)
		},
	}
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
	return newDiscordScheduler(ctx, dc, verifier, strings.TrimSpace(appID), strings.TrimSpace(channelRaw), wheelstore.New(database), futuOrderPlacer{addr: futuProtoAddr(), env: env}), nil
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
	}
}

// pushSignalDiscord sends one ALERT embed with buttons to the channel when it
// survives the dismissal and LLM-approval gates. Retry semantics mirror the
// telegram loop: a review not yet recorded holds the cursor back, REJECTED and
// dismissed signals skip permanently.
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
			rejection, rerr := s.store.LatestAction(ctx, sig.ID, "REJECTED")
			if rerr != nil {
				s.logf("push: %s signal=%d: rejected action lookup: %v", sig.Symbol, sig.ID, rerr)
				return true
			}
			s.pushRejectedDiscord(ctx, sig, rejection)
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
		s.logf("push: %s signal=%d: LLM review %s; pushing reasons", sig.Symbol, sig.ID, verdictOf(review))
		s.pushRejectedDiscord(ctx, sig, review)
		return false
	}
	text, err := alertMessage(&sig, reviewReasons(review)...)
	if err != nil {
		s.logf("push: %s signal=%d: %v", sig.Symbol, sig.ID, err)
		return false
	}
	msg := discord.Message{
		Embeds: []discord.Embed{{
			Title:       fmt.Sprintf("📌 信号 #%d · %s", sig.ID, sig.Symbol),
			Description: discord.ToMarkdown(text),
			Color:       discord.ColorApprove,
			Timestamp:   s.now().UTC().Format(time.RFC3339),
		}},
		Components: [][]discord.Button{{
			{Style: 3, Label: "✅ 下单", CustomID: fmt.Sprintf("wheel:%d:yes", sig.ID)},
			{Style: 4, Label: "❌ 拒绝", CustomID: fmt.Sprintf("wheel:%d:no", sig.ID)},
			{Style: 2, Label: "⚠️ Dismiss", CustomID: fmt.Sprintf("wheel:%d:dismiss", sig.ID)},
		}},
	}
	if err := s.pushEmbedDiscord(ctx, msg); err != nil {
		s.logf("push: %s signal=%d: %v", sig.Symbol, sig.ID, err)
	}
	return false
}

// pushRejectedDiscord reports a fail-closed LLM disposition (gray embed; the
// REJECTED action's Details.reasons are the source of truth).
func (s *discordScheduler) pushRejectedDiscord(ctx context.Context, sig wheelstore.SignalRecord, rejection *wheelstore.ActionRecord) {
	verdict := verdictOf(rejection)
	if verdict == "" {
		verdict = "REJECT"
	}
	reasons := reviewReasons(rejection)
	if len(reasons) == 0 && rejection != nil && strings.TrimSpace(rejection.Note) != "" {
		reasons = []string{rejection.Note}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** · %s", sig.Symbol, verdict)
	for _, reason := range reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			fmt.Fprintf(&b, "\n• %s", reason)
		}
	}
	_ = s.pushEmbedDiscord(ctx, discord.Message{Embeds: []discord.Embed{{
		Title:       fmt.Sprintf("❌ 信号 #%d 被 LLM 审核拒绝", sig.ID),
		Description: b.String(),
		Color:       discord.ColorRejected,
		Timestamp:   s.now().UTC().Format(time.RFC3339),
	}}})
}

// pushEmbedDiscord sends one message to the configured channel.
func (s *discordScheduler) pushEmbedDiscord(ctx context.Context, msg discord.Message) error {
	if s.dc == nil || s.channelID == "" {
		return errors.New("channel not configured")
	}
	return s.dc.CreateMessage(ctx, s.channelID, msg)
}

// handleInteraction serves POST /v1/discord/interactions: Ed25519 verification
// first (this endpoint is reachable through the public haproxy path, so every
// failed check is 401), then PING → PONG and message components → the wheel
// confirm flow. The response is written before any order work so Discord's
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
	if in.Type != discord.TypeMessageComponent || in.Data == nil {
		http.Error(w, "unsupported interaction", http.StatusBadRequest)
		return
	}
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
		if _, err := s.store.AppendAction(s.ctx, wheelstore.ActionRecord{SignalID: signalID, Action: "NO", Actor: discordActor(&in), Note: "继续等待机会"}); err != nil {
			s.logf("interaction %s: no: %v", in.ID, err)
			discord.WriteResponse(w, discord.EphemeralMessage("记录失败"))
			return
		}
		discord.WriteResponse(w, discord.EphemeralMessage("已记录,继续等待机会"))
		s.noticeDiscord(s.ctx, "❌ 已拒绝", fmt.Sprintf("信号 #%d · %s\n老板拒绝该信号,继续等待机会", signalID, s.now().Format("2006-01-02 15:04:05")))
	case "dismiss":
		s.recordDismissDiscord(w, &in, signalID)
	default:
		discord.WriteResponse(w, discord.EphemeralMessage("未知操作"))
	}
}

// confirmOrderDiscord is the yes path with the same semantics as the telegram
// confirm: freshness window, LLM APPROVE gate, CONFIRM dedup, limit price from
// the candidate quote (市价单禁止), sim-env placement, then the outcome is
// pushed to the channel; any refusal is recorded REJECTED.
func (s *discordScheduler) confirmOrderDiscord(ctx context.Context, in *discord.Interaction, signalID int64) {
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
	if confirmed, err := s.store.HasAction(ctx, signalID, "CONFIRM"); err != nil {
		s.rejectDiscord(ctx, in, signalID, "confirm check failed")
		return
	} else if confirmed {
		s.rejectDiscord(ctx, in, signalID, "already confirmed")
		return
	}
	if s.orders == nil {
		s.rejectDiscord(ctx, in, signalID, "order placer unavailable")
		return
	}
	cand, err := firstCandidate(sig)
	if err != nil {
		s.rejectDiscord(ctx, in, signalID, "no usable candidate")
		return
	}
	price := cand.Quote.Last
	if price <= 0 {
		s.rejectDiscord(ctx, in, signalID, "no usable limit price")
		return
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
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{
		SignalID: signalID, Action: "CONFIRM", Actor: discordActor(in),
		Details: map[string]any{"order_id": orderID, "order_id_ex": orderIDEx, "symbol": cand.Code, "side": cand.Side, "qty": cand.Quantity},
	}); err != nil {
		s.logf("interaction %s: confirm record: %v", in.ID, err)
	}
	sideName := "买入"
	if cand.Side == "sell" {
		sideName = "卖出"
	}
	s.noticeDiscord(ctx, "✅ 已下单", fmt.Sprintf(
		"信号 #%d\n%s %s %s %d 股 @ 限价 %.2f\n订单号 `%s`(%d)\n时间 %s",
		signalID, sideName, cand.Code, cand.Side, cand.Quantity, price, orderIDEx, orderID,
		s.now().Format("2006-01-02 15:04:05"),
	))
	go s.watchFillDiscord(ctx, signalID, cand.Code, cand.Side, float64(cand.Quantity), price, orderIDEx)
}

// watchFillDiscord polls the placed order until it fills, cancels or the watch
// window closes, pushing the outcome to the channel (same cadence and window
// as the telegram watcher; 成交结果要推送, 老板指令 2026-08-12).
func (s *discordScheduler) watchFillDiscord(ctx context.Context, signalID int64, symbol, side string, qty, price float64, orderIDEx string) {
	const (
		pollEvery = 15 * time.Second
		maxPolls  = 8
	)
	sideName := "买入"
	if side == "sell" {
		sideName = "卖出"
	}
	for i := 0; i < maxPolls; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollEvery):
			}
		}
		status, found, err := s.orders.OrderStatus(ctx, symbol, orderIDEx)
		if err != nil {
			s.logf("watch fill %s: %v", orderIDEx, err)
			continue
		}
		if !found {
			continue
		}
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
	}
	s.noticeDiscord(ctx, "⏳ 订单挂单中未成交", fmt.Sprintf(
		"信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 `%s`\n观察 %s",
		signalID, sideName, symbol, qty, price, orderIDEx,
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
