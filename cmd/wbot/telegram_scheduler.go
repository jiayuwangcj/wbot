package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/telegram"
	"github.com/jiayu/wbot/internal/wheelstore"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

const (
	// telegramPushInterval is the wheel ALERT poll cadence.
	telegramPushInterval = 30 * time.Second
	// signalFreshWindow bounds how old an ALERT may be before yes is refused.
	signalFreshWindow = 10 * time.Minute
)

// errLiveEnvNotAllowed is returned by the order placer for real-env
// placement; the yes path records REJECTED with this exact reason.
var errLiveEnvNotAllowed = errors.New("live env not allowed")

// wheelOrderPlacer submits a sim-env market option order (futu proto adapter;
// fakes in tests).
type wheelOrderPlacer interface {
	// PlaceOrder places a limit order at price (市价单禁止, 老板指令
	// 2026-08-12: 所有策略只用限价单; price 必须 > 0, 由调用方从候选取价)。
	PlaceOrder(ctx context.Context, symbol, side string, qty, price float64) (orderIDEx string, orderID uint64, err error)
	// OrderStatus looks up orderIDEx in the env account of symbol (account
	// auto-resolved by market + security type, 多模拟账户 2026-08-12) and
	// returns its gateway status code. found=false when the gateway does not
	// know the order yet (caller retries); err only on gateway/account
	// failures (成交监控用, 老板指令 2026-08-12: 成交结果要推送)。
	OrderStatus(ctx context.Context, symbol, orderIDEx string) (status int32, found bool, err error)
}

// futuOrderPlacer adapts the proto TradeClient to wheelOrderPlacer: it opens
// the gateway per order, resolves the env account and submits a limit order.
// Real-env placement is refused with errLiveEnvNotAllowed (the
// wheel confirm path is sim-only by design).
type futuOrderPlacer struct {
	addr string
	env  futu.Env
}

func (p futuOrderPlacer) PlaceOrder(ctx context.Context, symbol, side string, qty, price float64) (string, uint64, error) {
	if p.env != futu.EnvSim {
		return "", 0, errLiveEnvNotAllowed
	}
	tc, err := futu.AcquireTrade(ctx, p.addr)
	if err != nil {
		return "", 0, fmt.Errorf("open trade: %w", err)
	}
	defer tc.Close()
	acc, err := tc.AccountForSymbol(ctx, p.env, symbol, 0)
	if err != nil {
		return "", 0, err
	}
	return tc.PlaceOrder(ctx, acc, futu.OrderRequest{Symbol: symbol, Side: side, Qty: qty, Price: price})
}

// OrderStatus polls the env account's order list for orderIDEx (watches reuse
// the placer's connection+env, so fakes in tests stay interface-compatible).
func (p futuOrderPlacer) OrderStatus(ctx context.Context, symbol, orderIDEx string) (int32, bool, error) {
	if p.env != futu.EnvSim {
		return 0, false, errLiveEnvNotAllowed
	}
	tc, err := futu.AcquireTrade(ctx, p.addr)
	if err != nil {
		return 0, false, fmt.Errorf("open trade: %w", err)
	}
	defer tc.Close()
	acc, err := tc.AccountForSymbol(ctx, p.env, symbol, 0)
	if err != nil {
		return 0, false, err
	}
	orders, err := tc.Orders(ctx, acc, false)
	if err != nil {
		return 0, false, err
	}
	for _, o := range orders {
		if o.GetOrderIDEx() == orderIDEx {
			return o.GetOrderStatus(), true, nil
		}
	}
	return 0, false, nil
}

// telegramScheduler pushes wheel ALERT signals to Telegram and disposes the
// yes/no/dismiss buttons. It owns no goroutines itself: startTelegramScheduler
// runs one push loop and one long-poll loop, both on the serve context.
type telegramScheduler struct {
	tg      *telegram.Client
	store   wheelstore.SignalRepository
	orders  wheelOrderPlacer
	chatIDs map[int64]bool
	now     func() time.Time
	logf    func(format string, a ...any)
}

// sendToChats pushes one text message to every whitelisted chat. Button
// presses, order outcomes, fills and LLM rejections all route through here
// (老板指令 2026-08-12: 重要事件必须推送,不能只靠回调 toast 或日志)。
func (s *telegramScheduler) sendToChats(ctx context.Context, text string) {
	for chatID := range s.chatIDs {
		if err := s.tg.SendMessage(ctx, strconv.FormatInt(chatID, 10), text, nil); err != nil {
			s.logf("push: chat=%d: %v", chatID, err)
		}
	}
}

func newTelegramScheduler(tg *telegram.Client, store wheelstore.SignalRepository, orders wheelOrderPlacer, chatIDs map[int64]bool) *telegramScheduler {
	return &telegramScheduler{
		tg:      tg,
		store:   store,
		orders:  orders,
		chatIDs: chatIDs,
		now:     time.Now,
		logf: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, "telegram: "+format+"\n", a...)
		},
	}
}

// startTelegramScheduler runs the wheel Telegram loop for serve until ctx is
// cancelled. Token and chat_ids come from ~/.wbot/wbot.conf via
// config.Store.Lookup (consumers open the store themselves; wbot.conf is
// written tmp+rename atomically, so a concurrent admin PUT never yields a
// torn read). Missing config is logged once and the loop is skipped.
func startTelegramScheduler(ctx context.Context, database *sql.DB, env futu.Env) {
	cfg, err := openTelegramConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "telegram: config: %v\n", err)
		return
	}
	token, tokenSet, err := cfg.Lookup("credentials.telegram.token")
	if err != nil {
		fmt.Fprintf(os.Stderr, "telegram: token: %v\n", err)
		return
	}
	chatRaw, chatSet, err := cfg.Lookup("credentials.telegram.chat_ids")
	if err != nil {
		fmt.Fprintf(os.Stderr, "telegram: chat_ids: %v\n", err)
		return
	}
	if !tokenSet || !chatSet || strings.TrimSpace(token) == "" || strings.TrimSpace(chatRaw) == "" {
		fmt.Fprintf(os.Stderr, "telegram: not configured (set credentials.telegram.token and credentials.telegram.chat_ids via the admin wizard, then restart serve --telegram-run)\n")
		return
	}
	chatIDs, err := telegram.ParseChatIDs(chatRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "telegram: %v\n", err)
		return
	}
	tg, err := telegram.New(token, strings.TrimSpace(os.Getenv("TELEGRAM_API_BASE_URL")), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "telegram: %v\n", err)
		return
	}
	s := newTelegramScheduler(tg, wheelstore.New(database), futuOrderPlacer{addr: futuProtoAddr(), env: env}, chatIDs)
	go func() {
		if err := s.runPush(ctx, telegramPushInterval); err != nil && !errors.Is(err, context.Canceled) {
			s.logf("push: %v", err)
		}
	}()
	go func() {
		err := s.tg.Poll(ctx, func(ctx context.Context, u telegram.Update) error {
			if u.CallbackQuery != nil {
				s.handleCallback(ctx, u.CallbackQuery)
			}
			return nil
		}, func(err error) { s.logf("poll: %v", err) })
		if err != nil && !errors.Is(err, context.Canceled) {
			s.logf("poll: %v", err)
		}
	}()
}

func openTelegramConfig() (*config.Store, error) {
	if dir := strings.TrimSpace(os.Getenv("WBOT_CONFIG_DIR")); dir != "" {
		return config.Open(dir)
	}
	return config.OpenDefault()
}

// runPush polls new ALERT signals and pushes those whose latest LLM review is
// APPROVE. The in-memory cursor starts at the newest signal id so a restart
// never replays history. During a batch it acts as a waterline: it advances
// only through the contiguous prefix that has no retryable signal, so a
// pending signal cannot be skipped by a later signal in the same batch. The
// in-memory handled set prevents those later signals from being pushed again
// while the waterline is held back. A signal whose LLM review is not yet
// recorded is treated as a race with the appending POST and retried (cursor
// held back) until the review lands or the retry window closes. A failed
// MaxSignalID retries before the ticker starts: polling with a zero cursor
// would replay every historical ALERT once the DB recovers.
func (s *telegramScheduler) runPush(ctx context.Context, interval time.Duration) error {
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
				retry = s.pushSignal(ctx, sig)
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

// pushSignal sends one ALERT to every whitelisted chat when it survives the
// dismissal and LLM-approval gates; skips are logged with the reason. It
// returns retry=true when the signal may become pushable on a later pass (its
// LLM review has not landed yet — the review runs after AppendSignal inside
// the POST handler, so the first push pass can race it); the caller then holds
// the cursor back so the signal is not lost. Signals that are permanently
// unpushable (REJECTED review, dismissed, no review after the retry window)
// return false so the cursor advances past them.
func (s *telegramScheduler) pushSignal(ctx context.Context, sig wheelstore.SignalRecord) (retry bool) {
	if dismissed, err := s.store.IsDismissed(ctx, sig.Symbol, utcDate(s.now())); err != nil {
		s.logf("push: %s signal=%d: dismissed check: %v", sig.Symbol, sig.ID, err)
		return true
	} else if dismissed {
		s.logf("push: %s signal=%d: dismissed for today, skip", sig.Symbol, sig.ID)
		return false
	}
	review, err := s.store.LatestLLMReview(ctx, sig.ID)
	if err != nil {
		// No LLM_REVIEW action yet. The appending POST records the disposition
		// after AppendSignal, so an ALERT can be picked up here in the window
		// before its review lands. Retry while the signal is fresh; once the
		// retry window closes or a REJECTED action exists, skip permanently.
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
			s.logf("push: %s signal=%d: LLM review REJECTED; pushing reasons", sig.Symbol, sig.ID)
			s.pushRejectedSignal(ctx, sig, rejection)
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
		// 老板指令 2026-08-12: LLM 拒绝必须推送,让老板了解策略为何失败。
		s.logf("push: %s signal=%d: LLM review %s; pushing reasons", sig.Symbol, sig.ID, verdictOf(review))
		s.pushRejectedSignal(ctx, sig, review)
		return false
	}
	text, err := alertMessage(&sig, reviewReasons(review)...)
	if err != nil {
		s.logf("push: %s signal=%d: %v", sig.Symbol, sig.ID, err)
		return
	}
	buttons := []telegram.Button{
		{Text: "✅ 下单", Data: fmt.Sprintf("wheel:%d:yes", sig.ID)},
		{Text: "❌ 拒绝", Data: fmt.Sprintf("wheel:%d:no", sig.ID)},
		{Text: "⚠️ Dismiss", Data: fmt.Sprintf("wheel:%d:dismiss", sig.ID)},
	}
	for chatID := range s.chatIDs {
		if err := s.tg.SendMessage(ctx, strconv.FormatInt(chatID, 10), text, buttons); err != nil {
			s.logf("push: %s signal=%d chat=%d: %v", sig.Symbol, sig.ID, chatID, err)
		}
	}
	return false
}

// pushRejectedSignal reports a fail-closed LLM disposition using the same
// signal card as an approval, only the LLM review section shows the rejection
// and there are no action buttons (老板指令 2026-08-13: 拒绝单与通过单式样统一).
// REJECTED is the persisted action for LLM rejects, so its Details.reasons
// are the source of truth when LatestLLMReview has no row.
func (s *telegramScheduler) pushRejectedSignal(ctx context.Context, sig wheelstore.SignalRecord, rejection *wheelstore.ActionRecord) {
	reasons := reviewReasons(rejection)
	if len(reasons) == 0 && rejection != nil && strings.TrimSpace(rejection.Note) != "" {
		reasons = []string{rejection.Note}
	}
	// A review pipeline failure (timeout, upstream error) is not a model
	// verdict: the fail-closed disposition carries an "error" detail, and the
	// user should see 审核失败 rather than 被拒绝 (2026-08-13: signal 453 was
	// REJECTED for a client timeout but displayed as a model rejection).
	label := "❌ REJECT"
	if rejection != nil {
		if e, ok := rejection.Details["error"]; ok && e != nil {
			label = "⚠️ 审核失败"
		}
	}
	text, err := alertCard(&sig, label, reasons...)
	if err != nil {
		// A rejection card cannot be built (missing candidate etc.): fall back
		// to the minimal fail-closed notice so the disposition is never lost.
		s.logf("push: %s signal=%d: reject card fallback: %v", sig.Symbol, sig.ID, err)
		title := fmt.Sprintf("❌ <b>信号 #%d 被 LLM 审核拒绝</b> · %s", sig.ID, s.now().Format("2006-01-02 15:04:05"))
		lines := []string{title, fmt.Sprintf("%s · <code>%s</code>", html.EscapeString(sig.Symbol), html.EscapeString(label))}
		for _, reason := range reasons {
			if reason = strings.TrimSpace(reason); reason != "" {
				lines = append(lines, "• "+html.EscapeString(reason))
			}
		}
		text = strings.Join(lines, "\n")
	}
	s.sendToChats(ctx, text)
}

// handleCallback routes one inline-button press. from.id must be in the chat
// whitelist; every disposition is recorded as a wheel_signal_action.
func (s *telegramScheduler) handleCallback(ctx context.Context, cq *telegram.CallbackQuery) {
	if !s.chatIDs[cq.From.ID] {
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "未授权用户,操作已拒绝")
		return
	}
	signalID, action, err := parseCallbackData(cq.Data)
	if err != nil {
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "无效的按钮")
		return
	}
	switch action {
	case "yes":
		s.confirmOrder(ctx, cq, signalID)
	case "no":
		if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{SignalID: signalID, Action: "NO", Actor: telegramActor(cq), Note: "继续等待机会"}); err != nil {
			s.logf("callback %s: no: %v", cq.ID, err)
			_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "记录失败")
			return
		}
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "已记录,继续等待机会")
		// 老板指令 2026-08-12: 按钮点击要推送(时间/信号号可见)。
		s.sendToChats(ctx, fmt.Sprintf("❌ <b>已拒绝</b> · 信号 #%d · %s\n%s", signalID, s.now().Format("2006-01-02 15:04:05"), "老板拒绝该信号,继续等待机会"))
	case "dismiss":
		s.recordDismiss(ctx, cq, signalID)
	default:
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "未知操作")
	}
}

// confirmOrder is the yes path: re-verify the signal is fresh and its latest
// LLM review is APPROVE, then place a sim-env limit order and record
// CONFIRM. Any refusal is recorded as REJECTED and answered with a toast.
func (s *telegramScheduler) confirmOrder(ctx context.Context, cq *telegram.CallbackQuery, signalID int64) {
	sig, err := s.store.GetSignal(ctx, signalID)
	if err != nil {
		s.reject(ctx, cq, signalID, "signal not found", "信号不存在")
		return
	}
	if s.now().Sub(sig.CreatedAt) > signalFreshWindow {
		s.reject(ctx, cq, signalID, "signal expired", "信号已过期(>10 分钟)")
		return
	}
	review, err := s.store.LatestLLMReview(ctx, signalID)
	if err != nil || verdictOf(review) != "APPROVE" {
		s.reject(ctx, cq, signalID, "llm review not approved", "LLM 审核未通过")
		return
	}
	if s.orders == nil {
		s.reject(ctx, cq, signalID, "order placer unavailable", "下单通道未配置")
		return
	}
	cand, err := firstCandidate(sig)
	if err != nil {
		s.reject(ctx, cq, signalID, "no usable candidate", "信号缺少可下单候选")
		return
	}
	// 限价单纪律(老板指令 2026-08-12: 所有策略禁止市价单): 限价取候选
	// 期权最新价(last; LLM 链路 = 注入的 premium), 无价可依则拒绝而非臆造。
	price := cand.Quote.Last
	if price <= 0 {
		s.reject(ctx, cq, signalID, "no usable limit price", "候选无可用限价,拒绝下单")
		return
	}
	claims, ok := s.store.(wheelstore.OrderClaimRepository)
	if !ok {
		s.reject(ctx, cq, signalID, "confirm claim unavailable", "下单幂等保护未配置")
		return
	}
	claimed, err := claims.ClaimOrder(ctx, signalID, telegramActor(cq))
	if err != nil {
		s.reject(ctx, cq, signalID, "confirm claim failed", "下单状态检查失败")
		return
	}
	if !claimed {
		s.reject(ctx, cq, signalID, "already confirmed", "该信号已被确认,请勿重复确认")
		return
	}
	orderIDEx, orderID, err := s.orders.PlaceOrder(ctx, cand.Code, cand.Side, float64(cand.Quantity), price)
	if err != nil {
		reason := "place order failed"
		toast := "下单失败"
		if errors.Is(err, errLiveEnvNotAllowed) {
			reason = "live env not allowed"
			toast = "实盘下单不允许(仅模拟盘)"
		}
		s.logf("callback %s: %s: %v", cq.ID, reason, err) // 真实错误落日志,防吞错
		s.reject(ctx, cq, signalID, reason, toast)
		return
	}
	details := map[string]any{"order_id": orderID, "order_id_ex": orderIDEx, "symbol": cand.Code, "side": cand.Side, "qty": cand.Quantity}
	if err := claims.CompleteOrderClaim(ctx, signalID, orderID, orderIDEx, details); err != nil {
		// The durable pre-broker claim is retained.  Retrying is forbidden even
		// when this enrichment write fails because the broker already accepted.
		s.logf("callback %s: complete order claim: %v", cq.ID, err)
	}
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{
		SignalID: signalID, Action: "CONFIRM", Actor: telegramActor(cq),
		Details: details,
	}); err != nil {
		s.logf("callback %s: confirm record: %v", cq.ID, err)
	}
	_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, fmt.Sprintf("已下单 订单号 %d", orderID))
	// 老板指令 2026-08-12: 下单成功必须推送(时间/订单号/价格可见)。
	sideName := "买入"
	if cand.Side == "sell" {
		sideName = "卖出"
	}
	s.sendToChats(ctx, fmt.Sprintf(
		"✅ <b>已下单</b> · 信号 #%d\n%s %s %s %d 股 @ 限价 %.2f\n订单号 <code>%s</code>(%d)\n时间 %s",
		signalID, sideName, cand.Code, cand.Side, cand.Quantity, price, orderIDEx, orderID,
		s.now().Format("2006-01-02 15:04:05"),
	))
	// 成交监控:轮询订单状态,成交/撤单/超时都推送结果。
	go s.watchFill(ctx, signalID, cand.Code, cand.Side, float64(cand.Quantity), price, orderIDEx)
}

// watchFill polls the placed order until it fills, cancels or the watch
// window closes, pushing the outcome (老板指令 2026-08-12: 成交成功等
// 重要消息需要推送)。Runs on the serve ctx in its own goroutine; the
// callback answer is never blocked on gateway polling.
func (s *telegramScheduler) watchFill(ctx context.Context, signalID int64, symbol, side string, qty, price float64, orderIDEx string) {
	const (
		pollEvery = 15 * time.Second
		maxPolls  = 8 // ~2 分钟观察窗:未成交则推送挂单状态收尾
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
			continue // 网关尚未出现该订单,继续轮询
		}
		switch trdcommon.OrderStatus(status) {
		case trdcommon.OrderStatus_OrderStatus_Filled_All, trdcommon.OrderStatus_OrderStatus_Filled_Part:
			s.sendToChats(ctx, fmt.Sprintf(
				"✅ <b>已成交</b> · 信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 <code>%s</code>\n成交时间 %s",
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
			s.sendToChats(ctx, fmt.Sprintf(
				"⚠️ <b>订单未成交(%s)</b> · 信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 <code>%s</code>\n时间 %s",
				trdcommon.OrderStatus(status).String(), signalID, sideName, symbol, qty, price, orderIDEx, s.now().Format("2006-01-02 15:04:05"),
			))
			return
		}
	}
	// 观察窗内未成交:挂单仍在市场,告知状态不假装成功。
	s.sendToChats(ctx, fmt.Sprintf(
		"⏳ <b>订单挂单中未成交</b> · 信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 <code>%s</code>\n观察 %s",
		signalID, sideName, symbol, qty, price, orderIDEx,
		s.now().Format("2006-01-02 15:04:05"),
	))
}

// recordDismiss silences the signal's symbol for today and answers.
func (s *telegramScheduler) recordDismiss(ctx context.Context, cq *telegram.CallbackQuery, signalID int64) {
	sig, err := s.store.GetSignal(ctx, signalID)
	if err != nil {
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "信号不存在")
		return
	}
	if err := s.store.Dismiss(ctx, sig.Symbol, utcDate(s.now())); err != nil {
		s.logf("callback %s: dismiss: %v", cq.ID, err)
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "记录失败")
		return
	}
	_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "今日不再提醒该标的")
	// 老板指令 2026-08-12: 按钮点击要推送。
	s.sendToChats(ctx, fmt.Sprintf("⚠️ <b>已忽略</b> · 信号 #%d · %s · %s\n今日不再提醒 %s", signalID, s.now().Format("2006-01-02 15:04:05"), sig.Symbol, sig.Symbol))
}

// reject records REJECTED with the reason and answers the callback, and
// pushes the refusal so the boss sees why the order failed (老板指令
// 2026-08-12: 重要消息必须推送)。
func (s *telegramScheduler) reject(ctx context.Context, cq *telegram.CallbackQuery, signalID int64, reason, toast string) {
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{SignalID: signalID, Action: "REJECTED", Actor: telegramActor(cq), Note: reason}); err != nil {
		s.logf("callback %s: rejected record: %v", cq.ID, err)
	}
	_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, toast)
	symbol := fmt.Sprintf("#%d", signalID)
	if sig, err := s.store.GetSignal(ctx, signalID); err == nil {
		symbol = sig.Symbol
	}
	s.sendToChats(ctx, fmt.Sprintf("⛔ <b>下单失败</b> · 信号 #%d · %s · %s\n%s(%s)", signalID, s.now().Format("2006-01-02 15:04:05"), symbol, toast, reason))
}

// parseCallbackData parses "wheel:<signalID>:<yes|no|dismiss>".
func parseCallbackData(data string) (int64, string, error) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != "wheel" {
		return 0, "", errors.New("telegram: malformed callback data")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return 0, "", errors.New("telegram: malformed callback signal id")
	}
	action := parts[2]
	if action != "yes" && action != "no" && action != "dismiss" {
		return 0, "", errors.New("telegram: malformed callback action")
	}
	return id, action, nil
}

// telegramActor names the audit actor for a button press.
func telegramActor(cq *telegram.CallbackQuery) string {
	return fmt.Sprintf("telegram:%d", cq.From.ID)
}

// verdictOf extracts the LLM_REVIEW verdict from an action's details
// (convention: actor llm:<model>, details {verdict, reasons, notes}).
func verdictOf(a *wheelstore.ActionRecord) string {
	if a == nil {
		return ""
	}
	v, _ := a.Details["verdict"].(string)
	return strings.ToUpper(strings.TrimSpace(v))
}

// utcDate returns the UTC calendar day of t (dismissals and the runner's
// daily order count share the same day boundary).
func utcDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// candidateOrder is the first candidate's typed order facts from a signal.
type candidateOrder struct {
	Code      string
	Side      string
	Quantity  int
	Direction string
	Quote     candidateQuote
}

type candidateQuote struct {
	Symbol       string
	OptionType   string
	Strike       float64
	Expiry       string
	Bid          float64
	Ask          float64
	Last         float64
	Delta        float64
	ImpliedVol   float64
	OpenInterest int64
}

// firstCandidate reads the signal's first candidate into order facts. The
// wheel sells the option in both directions (PUT sell / CALL sell), so side
// is always sell; quantity defaults to 1 when missing.
func firstCandidate(sig *wheelstore.SignalRecord) (*candidateOrder, error) {
	if sig == nil || len(sig.Candidates) == 0 {
		return nil, errors.New("signal has no candidates")
	}
	c := sig.Candidates[0]
	if c.Quote == nil || strings.TrimSpace(c.Quote.Symbol) == "" {
		return nil, errors.New("candidate has no option symbol")
	}
	quote := c.Quote
	direction := strings.ToUpper(strings.TrimSpace(c.Direction))
	// 正股信号(2026-08-12 llm-signal 端点扩展):BUY/SELL 下单方向即方向;
	// 期权 PUT/CALL 语义为卖出开仓(收权利金)。
	var side string
	switch direction {
	case "PUT", "CALL":
		side = "sell"
	case "BUY":
		side = "buy"
	case "SELL":
		side = "sell"
	default:
		return nil, fmt.Errorf("candidate direction %q unsupported", c.Direction)
	}
	qty := c.Quantity
	if qty < 1 {
		qty = 1
	}
	return &candidateOrder{
		Code: quote.Symbol, Side: side, Quantity: qty, Direction: direction,
		Quote: candidateQuote{Symbol: quote.Symbol, OptionType: quote.OptionType, Strike: quote.Strike, Expiry: quote.Expiry, Bid: quote.Bid, Ask: quote.Ask, Last: quote.Last, Delta: quote.Delta, ImpliedVol: quote.ImpliedVol, OpenInterest: quote.OpenInterest},
	}, nil
}

const (
	alertOuterRule  = "━━━━━━━━━━━━━━━━━━━━"
	alertInnerRule  = "┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄"
	alertLabelWidth = 10
)

// alertMessage renders the v20 Telegram HTML layout. Review reasons are
// variadic so callers that only need a structural preview may omit them.
// alertMessage renders the approved signal card with the LLM review section
// showing APPROVE.
func alertMessage(sig *wheelstore.SignalRecord, reasons ...string) (string, error) {
	return alertCard(sig, "✅ APPROVE", reasons...)
}

// alertCard renders the full signal card shared by approved and rejected
// pushes (老板指令 2026-08-13: 拒绝单与通过单式样统一,只变底部 LLM 审核区,
// 拒绝单无按钮): only the LLM review section label and reasons differ.
func alertCard(sig *wheelstore.SignalRecord, verdictLabel string, reasons ...string) (string, error) {
	c, err := firstCandidate(sig)
	if err != nil {
		return "", err
	}
	expiry, expiryTime := expiryText(c.Quote.Expiry)
	dte := "-"
	if !expiryTime.IsZero() && !sig.CreatedAt.IsZero() {
		days := int(math.Ceil(expiryTime.Sub(sig.CreatedAt).Hours() / 24))
		if days < 0 {
			days = 0
		}
		dte = strconv.Itoa(days)
	}
	limit := "-"
	if c.Quote.Last > 0 {
		limit = fmt.Sprintf("%.2f", c.Quote.Last)
	}
	created := "-"
	if !sig.CreatedAt.IsZero() {
		created = sig.CreatedAt.Format("01-02 15:04")
	}
	stock := countText(sig.Inventory.ActualInventory)
	target := countText(sig.Inventory.TargetInventory)
	gap := countText(sig.Inventory.InventoryGap)

	// 正股信号(BUY/SELL)渲染标的侧信息;期权信号渲染行权/到期/希腊。
	// 标题带信号编号(老板指令 2026-08-12: 推送必须带编号以区分订单)。
	lines := []string{
		fmt.Sprintf("<b>📌 %s · %s · 信号 #%d</b>", html.EscapeString(sig.Symbol), directionLabel(c.Direction), sig.ID),
		alertOuterRule,
		"🎯 <b>订单</b>",
		alertRow("候选", fmt.Sprintf("<b><code>%s</code></b>", html.EscapeString(c.Code))),
	}
	if isStockDirection(c.Direction) {
		lines = append(lines,
			alertRow("数量", fmt.Sprintf("<b><code>%s</code></b> 股", commaInt(int64(c.Quantity)))),
			alertRow("限价", fmt.Sprintf("<b><code>%s</code></b>", limit)),
			alertInnerRule,
			"📊 <b>标的当前</b>",
			alertRow("正股现价", fmt.Sprintf("<b><code>%s</code></b>", priceText(sig.Inventory.CurrentPrice))),
			alertInnerRule,
		)
	} else {
		lines = append(lines,
			alertRow("行权", fmt.Sprintf("<b><code>%.2f</code></b>", c.Quote.Strike)),
			alertRow("到期", fmt.Sprintf("<code>%s</code> (剩 %s 天)", html.EscapeString(expiry), dte)),
			alertRow("数量", fmt.Sprintf("<b><code>%s</code></b> 张", commaInt(int64(c.Quantity)))),
			alertRow("限价", fmt.Sprintf("<b><code>%s</code></b> (估算)", limit)),
			alertInnerRule,
			"📊 <b>标的当前</b>",
			alertRow("正股现价", fmt.Sprintf("<b><code>%s</code></b>", priceText(sig.Inventory.CurrentPrice))),
			alertRow("bid/ask", fmt.Sprintf("<code>%.2f</code>/<code>%.2f</code>", c.Quote.Bid, c.Quote.Ask)),
			alertRow("希腊", fmt.Sprintf("Δ <code>%.2f</code> · IV <code>%.2f</code> · OI <code>%s</code>", c.Quote.Delta, c.Quote.ImpliedVol, commaInt(c.Quote.OpenInterest))),
			alertInnerRule,
		)
	}
	lines = append(lines,
		"🧭 <b>持仓与策略参数</b>",
		alertRow("正股持仓", fmt.Sprintf("<code>%s</code> 股", stock)),
		alertRow("CALL 持仓", "<code>-</code> 张 · <code>-</code>"),
		alertRow("PUT 持仓", "<code>-</code> 张"),
		alertRow("目标持仓", fmt.Sprintf("<code>%s</code> 股", target)),
		alertRow("库存缺口", fmt.Sprintf("<b><code>%s</code></b> 股", gap)),
		alertInnerRule,
		fmt.Sprintf("💡 <b>下单原因</b> · LLM 审核 <b>%s</b>", verdictLabel),
	)
	for _, reason := range reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			lines = append(lines, "• "+html.EscapeString(reason))
		}
	}
	lines = append(lines,
		alertOuterRule,
		fmt.Sprintf("信号 #%d · 配置 v%d · %s", sig.ID, sig.ConfigVersion, created),
	)
	return strings.Join(lines, "\n"), nil
}

func reviewReasons(review *wheelstore.ActionRecord) []string {
	if review == nil {
		return nil
	}
	var out []string
	switch reasons := review.Details["reasons"].(type) {
	case []any:
		for _, reason := range reasons {
			if text, ok := reason.(string); ok {
				out = append(out, text)
			}
		}
	case []string:
		out = append(out, reasons...)
	}
	return out
}

func alertRow(label, value string) string {
	return label + strings.Repeat(" ", max(1, alertLabelWidth-displayWidth(label))) + value
}

func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if r <= 0x7f {
			width++
		} else {
			width += 2
		}
	}
	return width
}

func expiryText(raw string) (string, time.Time) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("2006-01-02"), t
		}
	}
	return raw, time.Time{}
}

func countText(p *float64) string {
	if p == nil {
		return "-"
	}
	return commaInt(int64(math.Round(*p)))
}

func commaInt(v int64) string {
	s := strconv.FormatInt(v, 10)
	start := 0
	if strings.HasPrefix(s, "-") {
		start = 1
	}
	for i := len(s) - 3; i > start; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// directionLabel renders the wheel direction for the alert text (both
// directions sell the option; stock directions render as-is).
func directionLabel(direction string) string {
	switch direction {
	case "PUT":
		return "卖出认沽 (SELL PUT)"
	case "CALL":
		return "卖出认购 (SELL CALL)"
	}
	return direction
}

// isStockDirection reports whether the candidate is an underlying-stock signal
// (BUY/SELL, llm-signal endpoint extension 2026-08-12) rather than an option
// (PUT/CALL).
func isStockDirection(direction string) bool {
	return direction == "BUY" || direction == "SELL"
}

func priceText(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *p)
}
