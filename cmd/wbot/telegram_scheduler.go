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
	"github.com/jiayu/wbot/internal/wheelrun"
	"github.com/jiayu/wbot/internal/wheelstore"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

const (
	// telegramPushInterval is the wheel ALERT poll cadence.
	telegramPushInterval = 30 * time.Second
	// signalFreshWindow bounds how old an ALERT may be before yes is refused.
	signalFreshWindow = 10 * time.Minute
	// llmReviewRetryWindow: runner 在 gate 失败后 sleep 3s 同步重试一次
	// (300s http.Client 超时, internal/llmreview/llmreview.go:78, 97e769b
	// 变更);窗口内 LLM_REVIEW_FAILED 视为重试进行中,推送器保持游标等重试
	// 落记录(2026-08-14: 772/764 重试成功也丢卡)。重试最长时间 = 3s sleep
	// + 300s 超时 = 303s < 360s,窗口常数必须 ≥ 重试最长时间。
	llmReviewRetryWindow = 6 * time.Minute
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
	// CancelOrder cancels a previously confirmed order in the env account of
	// symbol (挂单撤单按钮 2026-08-13: 已下单未成交必须能确定地取消,否则
	// 策略再发新单会重复下单)。orderID is the numeric order id recorded on
	// the CONFIRM action.
	CancelOrder(ctx context.Context, symbol, orderID string) error
}

// orderLogf 是真实下单留痕的统一出口(老板指令 2026-08-14: 真实下单的接口
// 必须有日志供查证),telegram/discord 两处 placer 构造共用。
var orderLogf = func(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "order: "+format+"\n", a...)
}

// futuOrderPlacer adapts the proto TradeClient to wheelOrderPlacer: it opens
// the gateway per order, resolves the env account and submits a limit order.
// Real-env placement is refused with errLiveEnvNotAllowed (the
// wheel confirm path is sim-only by design).
type futuOrderPlacer struct {
	addr string
	env  futu.Env
	// logf 是真实下单留痕(老板指令 2026-08-14: 真实下单的接口必须有日志供
	// 查证)——账户/合约/数量/价格/响应订单号/耗时/错误全部落日志,单证
	// 可查。零值 nil 时静默(log 方法兜底),测试构造无需传。
	logf func(format string, a ...any)
}

func (p futuOrderPlacer) log(format string, a ...any) {
	if p.logf != nil {
		p.logf(format, a...)
	}
}

func (p futuOrderPlacer) PlaceOrder(ctx context.Context, symbol, side string, qty, price float64) (string, uint64, error) {
	if p.env != futu.EnvSim {
		return "", 0, errLiveEnvNotAllowed
	}
	tc, err := futu.AcquireTrade(ctx, p.addr)
	if err != nil {
		p.log("place FAIL %s %s %v @ %v: open trade: %v", symbol, side, qty, price, err)
		return "", 0, fmt.Errorf("open trade: %w", err)
	}
	defer tc.Close()
	acc, err := tc.AccountForSymbol(ctx, p.env, symbol, 0)
	if err != nil {
		p.log("place FAIL %s %s %v @ %v: account resolve: %v", symbol, side, qty, price, err)
		return "", 0, err
	}
	start := time.Now()
	orderIDEx, orderID, err := tc.PlaceOrder(ctx, acc, futu.OrderRequest{Symbol: symbol, Side: side, Qty: qty, Price: price})
	elapsed := time.Since(start)
	if err != nil {
		p.log("place FAIL acc=%d %s %s %v @ %v: %v (%s)", acc.GetAccID(), symbol, side, qty, price, err, elapsed)
		return "", 0, err
	}
	p.log("place OK acc=%d %s %s %v @ %v -> order_id=%d order_id_ex=%q (%s)",
		acc.GetAccID(), symbol, side, qty, price, orderID, orderIDEx, elapsed)
	return orderIDEx, orderID, nil
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

// CancelOrder cancels the confirmed order in the env account of symbol
// (撤单按钮 2026-08-13: 挂单未成交必须能确定取消,否则新单重复下单)。
func (p futuOrderPlacer) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if p.env != futu.EnvSim {
		return errLiveEnvNotAllowed
	}
	tc, err := futu.AcquireTrade(ctx, p.addr)
	if err != nil {
		return fmt.Errorf("open trade: %w", err)
	}
	defer tc.Close()
	acc, err := tc.AccountForSymbol(ctx, p.env, symbol, 0)
	if err != nil {
		return err
	}
	return tc.CancelOrder(ctx, acc, orderID)
}

// telegramScheduler pushes wheel ALERT signals to Telegram and disposes the
// yes/no/dismiss buttons. It owns no goroutines itself: startTelegramScheduler
// runs one push loop and one long-poll loop, both on the serve context.
type telegramScheduler struct {
	tg      *telegram.Client
	store   wheelstore.SignalRepository
	orders  wheelOrderPlacer
	quoter  underlyingQuoter
	chatIDs map[int64]bool
	now     func() time.Time
	logf    func(format string, a ...any)
	// watchEvery/watchReport 控制成交监控节奏(零值在构造函数填默认):
	// 观察窗内未成交先推送挂单状态,随后继续观察至成交/撤单/市场收盘,
	// 收盘仍未成交立即无理由撤单(老板指令 2026-08-13)。测试可缩短。
	watchEvery  time.Duration
	watchReport time.Duration
	// missingWarnAfter:订单在券商端连续查询不到(missing)多少轮后判定
	// 异常——警示 + 尝试撤单(2026-08-14 美股期权 stub 教训:网关返回的
	// stub 订单号 30 秒后被 purge,订单从未生效,必须显式暴露并撤单)。
	missingWarnAfter int
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

func newTelegramScheduler(tg *telegram.Client, store wheelstore.SignalRepository, orders wheelOrderPlacer, quoter underlyingQuoter, chatIDs map[int64]bool) *telegramScheduler {
	return &telegramScheduler{
		tg:               tg,
		store:            store,
		orders:           orders,
		quoter:           quoter,
		chatIDs:          chatIDs,
		now:              time.Now,
		watchEvery:       30 * time.Second,
		watchReport:      2 * time.Minute,
		missingWarnAfter: 4, // ≈2 分钟(30s×4)
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
	s := newTelegramScheduler(tg, wheelstore.New(database), futuOrderPlacer{addr: futuProtoAddr(), env: env, logf: orderLogf}, futuQuoter{client: futu.NewClient(resolveFutuGateway(""))}, chatIDs)
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
	// sentChats 记录每个重试中 signal 的已送达 chat(评审 P1-1):某 chat
	// 推送失败时重试只补发失败 chat,健康 chat 绝不重复收到带按钮的卡。
	sentChats := make(map[int64]map[int64]bool)
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
				delivered := sentChats[sig.ID]
				if delivered == nil {
					delivered = make(map[int64]bool)
					sentChats[sig.ID] = delivered
				}
				retry = s.pushSignal(ctx, sig, delivered)
				if !retry {
					handled[sig.ID] = struct{}{}
					delete(sentChats, sig.ID)
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
		for id := range sentChats {
			if id <= cursor {
				delete(sentChats, id)
			}
		}
		// 循环存活心跳:空转时段无事件日志,「静默=无信号」与「循环死/卡」
		// 不可区分(2026-08-14 实测 discord 推送静默 3.5 分钟零日志);每 5
		// 个 tick 打一行状态,卡死时心跳消失,serve 日志一眼可辨。
		if tick%5 == 0 {
			s.logf("push: heartbeat cursor=%d pending=%v signals=%d", cursor, pending, len(signals))
		}
		tick++
	}
}

// pushSignal sends one ALERT to every whitelisted chat when it survives the
// dismissal and LLM-approval gates; skips are logged with the reason. delivered
// is the per-signal set of chats that already received the card (owned by the
// push loop across passes): a partial push failure re-sends only to the
// missing chats, so a healthy chat never receives a duplicate actionable card
// (评审 P1-1). It returns retry=true when the signal may become pushable on a
// later pass (its LLM review has not landed yet — the review runs after
// AppendSignal inside the POST handler, so the first push pass can race it —
// or a SendMessage call failed transiently, 769); the caller then holds the
// cursor back so the signal is not lost. Signals that are permanently
// unpushable (REJECTED review, dismissed, no review after the retry window,
// LLM_REVIEW_FAILED beyond the gate retry window) return false so the cursor
// advances past them.
func (s *telegramScheduler) pushSignal(ctx context.Context, sig wheelstore.SignalRecord, delivered map[int64]bool) (retry bool) {
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
	// 底层资产名字进卡片(老板指令 2026-08-13: 正股价格区多一份名字+编号);
	// 查询失败退化为只显示编号,推送不阻塞。
	name := underlyingName(ctx, s.quoter, sig.Symbol)
	text, err := alertMessage(&sig, name, reviewReasons(review)...)
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
		if delivered[chatID] {
			continue // 已送达:重试只补发失败 chat,不重复推卡(评审 P1-1)
		}
		if err := s.tg.SendMessage(ctx, strconv.FormatInt(chatID, 10), text, buttons); err != nil {
			s.logf("push: %s signal=%d chat=%d: %v", sig.Symbol, sig.ID, chatID, err)
			retry = true
			continue
		}
		if delivered != nil {
			delivered[chatID] = true
		}
	}
	return retry
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
	case "yes", "no":
		// 立即回执,避免 Telegram 客户端「wbot 无响应」:首次点击时网关会话
		// 冷启动,撤单/下单 RPC 可达数秒,而回调须在数秒内应答。处理结果
		// 走推送消息(✅ 已下单 / ⛔ 下单失败 / ❌ 已拒绝)。异步执行同时
		// 避免慢网关阻塞 Poll 循环拖住其他按钮与消息。
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "已收到,处理中…")
		if action == "yes" {
			go s.confirmOrder(ctx, cq, signalID)
		} else {
			go s.declineOrder(ctx, cq, signalID)
		}
	case "dismiss":
		s.recordDismiss(ctx, cq, signalID)
	default:
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "未知操作")
	}
}

// declineOrder is the no path: a confirmed-but-unfilled signal is escalated to
// cancel (老板指令 2026-08-13: 701 确认后未成交,702 仍被生成并确认 → 双挂)。
// 撤销模拟盘挂单并记录 NO,解除策略的 pending-order 阻塞;撤单失败也必须
// 告知用户手动撤,保证挂单被取消而不是静默遗留。结果经推送消息送达。
func (s *telegramScheduler) declineOrder(ctx context.Context, cq *telegram.CallbackQuery, signalID int64) {
	note := "继续等待机会"
	if confirm, cerr := s.store.LatestAction(ctx, signalID, "CONFIRM"); cerr == nil {
		var oid uint64
		switch v := confirm.Details["order_id"].(type) {
		case float64: // JSONB 落库后数值读出为 float64
			oid = uint64(v)
		case uint64:
			oid = v
		}
		symbol := ""
		if sig, gerr := s.store.GetSignal(ctx, signalID); gerr == nil {
			symbol = sig.Symbol
		}
		switch {
		case oid == 0 || symbol == "":
			note = "撤单失败:缺少订单号或标的(请手动在模拟盘撤单)"
		default:
			if cerr := s.orders.CancelOrder(ctx, symbol, strconv.FormatUint(oid, 10)); cerr != nil {
				s.logf("callback %s: cancel order: %v", cq.ID, cerr)
				note = fmt.Sprintf("撤单失败:%v(请手动在模拟盘撤单)", cerr)
			} else {
				note = fmt.Sprintf("撤单成功 订单号 %d", oid)
			}
		}
	}
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{SignalID: signalID, Action: "NO", Actor: telegramActor(cq), Note: note}); err != nil {
		s.logf("callback %s: no: %v", cq.ID, err)
		return
	}
	// 老板指令 2026-08-12: 按钮点击要推送(时间/信号号可见)。
	s.sendToChats(ctx, fmt.Sprintf("❌ <b>已拒绝</b> · 信号 #%d · %s\n%s", signalID, s.now().Format("2006-01-02 15:04:05"), note))
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
	// 收盘闸门(老板指令 2026-08-13: 收盘订单立即无理由取消):市场已收盘
	// 不再新下订单,避免产生收盘即失效、次日残留的挂单。沿用 wheelrun 的
	// 市场时段判定(交易所时区 + 节假日日历)。
	if !wheelrun.MarketIsOpen(sig.Symbol, s.now(), nil) {
		s.reject(ctx, cq, signalID, "market closed", "市场已收盘,拒绝下单")
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
	// 改单(老板指令 2026-08-13: 允许策略调整未成交订单,改单同样需 LLM
	// 审核——已由上面 LatestLLMReview APPROVE 把关)。先撤销旧挂单再下
	// 新单;撤单失败 = 不执行新单(旧单仍在,再下单即重复敞口),拒绝并
	// 留痕让用户人工处理。
	replaced := ""
	if sig.Replace != nil && sig.Replace.OrderID != "" {
		if cerr := s.orders.CancelOrder(ctx, sig.Symbol, sig.Replace.OrderID); cerr != nil {
			s.logf("callback %s: cancel pending order %s: %v", cq.ID, sig.Replace.OrderID, cerr)
			s.reject(ctx, cq, signalID, "cancel pending order failed", "撤单失败,未下单(请人工处理旧挂单)")
			return
		}
		replaced = sig.Replace.OrderID
		s.logf("callback %s: cancelled pending order %s before replace signal #%d", cq.ID, replaced, signalID)
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
	// 订单号校验(2026-08-14 美股期权 stub 教训):网关对未获券商确认的订单
	// 返回占位 stub(order_id_ex="0"),订单从未真实生效,30 秒后被网关
	// purge。无真实券商订单号 = 下单未确认,按失败处理留痕,绝不推送
	// 「已下单」。资金安全铁律:异常直接取消,不设退化。
	if orderIDEx == "" || orderIDEx == "0" || orderID == 0 {
		s.logf("callback %s: order unconfirmed: order_id_ex=%q order_id=%d; treating as failure", cq.ID, orderIDEx, orderID)
		s.reject(ctx, cq, signalID, "order unconfirmed", "下单未获券商确认,订单未生效(请人工核实)")
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
	replaceLine := ""
	if replaced != "" {
		replaceLine = fmt.Sprintf("\n↻ 改单:已撤旧挂单 <code>%s</code>", replaced)
	}
	s.sendToChats(ctx, fmt.Sprintf(
		"✅ <b>已下单</b> · 信号 #%d\n%s %s %s %d 股 @ 限价 %.2f\n订单号 <code>%s</code>(%d)%s\n时间 %s",
		signalID, sideName, cand.Code, cand.Side, cand.Quantity, price, orderIDEx, orderID, replaceLine,
		s.now().Format("2006-01-02 15:04:05"),
	))
	// 成交监控:轮询订单状态,成交/撤单/收盘/异常都推送结果。
	go s.watchFill(ctx, signalID, cand.Code, cand.Side, float64(cand.Quantity), price, orderIDEx, orderID)
}

// watchFill polls the placed order until it fills, cancels or the market
// closes, pushing the outcome (老板指令 2026-08-12: 成交成功等重要消息
// 需要推送;2026-08-13: 收盘订单和异常订单立即无理由取消)。观察窗内未
// 成交先推送挂单状态,随后继续观察至终态或市场收盘——收盘时挂单立即撤
// 单,状态查询异常(无法确认)也立即撤单(fail-closed,资金安全)。Runs on
// the serve ctx in its own goroutine; the callback answer is never blocked
// on gateway polling.
func (s *telegramScheduler) watchFill(ctx context.Context, signalID int64, symbol, side string, qty, price float64, orderIDEx string, orderID uint64) {
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
			s.cancelResting(ctx, signalID, sideName, symbol, side, qty, price, orderIDEx, orderID, "市场收盘,挂单自动撤单")
			return
		}
		status, found, err := s.orders.OrderStatus(ctx, symbol, orderIDEx)
		if err != nil {
			// 状态无法确认 = 异常(fail-closed):立即撤单,不假装挂单仍受控。
			s.logf("watch fill %s: %v; cancelling", orderIDEx, err)
			s.cancelResting(ctx, signalID, sideName, symbol, side, qty, price, orderIDEx, orderID, "订单状态异常,自动撤单")
			return
		}
		if found {
			missing = 0
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
		} else {
			// 订单在券商端不可见:正常订单下单后立即出现在订单列表(即便
			// 未成交),连续多轮查询不到 = 订单可能从未生效——2026-08-14
			// 美股期权 stub 教训:网关对未获券商确认的订单返回占位 stub,
			// 30 秒后被 purge,watchFill 却静默轮询无任何留痕。连续
			// missingWarnAfter 轮 → 推送警示 + NOTE 留痕 + 尝试撤单;撤单
			// 成功即结束,失败继续观察(订单可能晚确认,终态仍推送)。
			missing++
			if missing >= missingWarnAfter {
				s.logf("watch fill %s: order not visible after %d polls; warning+cancel", orderIDEx, missing)
				s.sendToChats(ctx, fmt.Sprintf(
					"⚠️ <b>订单券商端未确认</b> · 信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 <code>%s</code>\n订单在券商端查询不到,可能未生效,尝试撤单",
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
						s.sendToChats(ctx, fmt.Sprintf(
							"⚠️ <b>撤单失败</b> · 信号 #%d\n订单号 <code>%s</code> · 请手动在模拟盘撤单\n错误 %v",
							signalID, orderIDEx, cerr,
						))
					} else {
						s.sendToChats(ctx, fmt.Sprintf(
							"❌ <b>已撤单</b> · 信号 #%d\n订单号 <code>%s</code> · 订单未获券商确认,已撤单",
							signalID, orderIDEx,
						))
						return
					}
				} else {
					// 缺订单号无法撤单 → 显式提示手动撤单,不静默遗留。
					s.sendToChats(ctx, fmt.Sprintf(
						"⚠️ <b>挂单未撤(缺少订单号)</b> · 信号 #%d\n订单号 <code>%s</code> · 请手动在模拟盘撤单",
						signalID, orderIDEx,
					))
					return
				}
			}
		}
		// 观察窗内未成交:先推送挂单状态,不假装成功;随后继续观察至收盘。
		if !reportedPending && s.now().Sub(started) >= reportAfter {
			s.sendToChats(ctx, fmt.Sprintf(
				"⏳ <b>订单挂单中未成交</b> · 信号 #%d\n%s %s %.0f 股 @ %.2f\n订单号 <code>%s</code>\n观察 %s",
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

// cancelResting cancels an unfilled resting order immediately (无理由撤单,
// 老板指令 2026-08-13) and pushes the outcome so the boss always sees the
// state. A failed cancel is pushed as a manual action item, never silent.
func (s *telegramScheduler) cancelResting(ctx context.Context, signalID int64, sideName, symbol, side string, qty, price float64, orderIDEx string, orderID uint64, note string) {
	if orderID == 0 {
		s.logf("watch fill %s: no numeric order id to cancel; %s", orderIDEx, note)
		s.sendToChats(ctx, fmt.Sprintf(
			"⚠️ <b>挂单未撤(缺少订单号)</b> · 信号 #%d\n%s\n订单号 <code>%s</code> · 请手动在模拟盘撤单",
			signalID, note, orderIDEx,
		))
		return
	}
	if err := s.orders.CancelOrder(ctx, symbol, strconv.FormatUint(orderID, 10)); err != nil {
		s.logf("watch fill %s: cancel: %v", orderIDEx, err)
		s.sendToChats(ctx, fmt.Sprintf(
			"⚠️ <b>撤单失败</b> · 信号 #%d\n%s\n%s %s %.0f 股 @ %.2f\n订单号 <code>%s</code> · 请手动在模拟盘撤单\n错误 %v",
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
	s.sendToChats(ctx, fmt.Sprintf(
		"❌ <b>已撤单</b> · 信号 #%d · %s\n%s %s %.0f 股 @ %.2f\n订单号 <code>%s</code>\n时间 %s",
		signalID, note, sideName, symbol, qty, price, orderIDEx,
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

// firstCandidate reads the signal's *accepted* candidate into order facts.
// 资金安全(老板指令 2026-08-13: 所有必须完美匹配,不设退化策略,异常直接
// 取消订单):候选列表保留输入顺序,被排除的候选(如同合约挂单)排在列表
// 头部但 Accepted=false;执行/卡片/成交监控必须用策略实际选中的候选——
// signal 757 教训:LLM 审核批的 P28500(accepted),执行却落在列表首位的
// P29000(挂单合约被排除),审核与下单不一致。**全无 accepted 候选 = 审核
// 与策略不一致异常,直接返回错误由执行层拒绝下单,绝不回退列表首位**。
// wheel 卖出期权(PUT/CALL sell),quantity 缺省 1。
func firstCandidate(sig *wheelstore.SignalRecord) (*candidateOrder, error) {
	if sig == nil || len(sig.Candidates) == 0 {
		return nil, errors.New("signal has no candidates")
	}
	var c wheelstore.Candidate
	for i := range sig.Candidates {
		cand := sig.Candidates[i]
		if cand.Accepted && cand.Quote != nil && strings.TrimSpace(cand.Quote.Symbol) != "" {
			c = cand
			break
		}
	}
	if c.Quote == nil || strings.TrimSpace(c.Quote.Symbol) == "" {
		return nil, errors.New("signal has no approved candidate")
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
// showing APPROVE. underlying is the display name (腾讯控股) or "" to fall
// back to the bare code (老板指令 2026-08-13).
func alertMessage(sig *wheelstore.SignalRecord, underlying string, reasons ...string) (string, error) {
	return alertCard(sig, underlying, "✅ APPROVE", reasons...)
}

// alertCard renders the full signal card (2026-08-14 起仅 APPROVE 推送使用):
// only the LLM review section label and reasons differ per verdict label.
// underlying is the display name ("" falls back to the bare code).
func alertCard(sig *wheelstore.SignalRecord, underlying string, verdictLabel string, reasons ...string) (string, error) {
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
	// 标题带信号编号与策略来源(老板指令 2026-08-12: 推送必须带编号以区分订单;
	// 2026-08-13: 单子须标明是大模型策略还是固化策略生成的)。
	replaceLine := ""
	if sig.Replace != nil && sig.Replace.Contract != "" {
		replaceLine = alertRow("改单", fmt.Sprintf("撤 <code>%s</code>(%s) 换此候选", html.EscapeString(sig.Replace.Contract), sig.Replace.OrderID))
	}
	lines := []string{
		fmt.Sprintf("<b>📌 %s · %s · 信号 #%d · %s</b>", html.EscapeString(sig.Symbol), directionLabel(c.Direction), sig.ID, strategyBadge(sig.Strategy)),
		alertOuterRule,
		"🎯 <b>订单</b>",
		alertRow("候选", fmt.Sprintf("<b><code>%s</code></b>", html.EscapeString(c.Code))),
	}
	if replaceLine != "" {
		lines = append(lines, replaceLine)
	}
	if isStockDirection(c.Direction) {
		lines = append(lines,
			alertRow("数量", fmt.Sprintf("<b><code>%s</code></b> 股", commaInt(int64(c.Quantity)))),
			alertRow("限价", fmt.Sprintf("<b><code>%s</code></b>", limit)),
			alertInnerRule,
			"📊 <b>标的当前</b>",
			alertRow("标的", fmt.Sprintf("<b>%s</b>", html.EscapeString(underlyingLabel(underlying, sig.Symbol)))),
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
			alertRow("标的", fmt.Sprintf("<b>%s</b>", html.EscapeString(underlyingLabel(underlying, sig.Symbol)))),
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

// strategyBadge labels the signal origin on push cards: "llm" (大模型策略)
// vs "wheel" (固化规则策略), so the operator can tell at a glance where an
// order came from (老板指令 2026-08-13: 单子未标明是大模型策略还是固化策略
// 生成的)。Unknown/empty strategy defaults to the fixed wheel label.
func strategyBadge(strategy string) string {
	if strings.TrimSpace(strategy) == "llm" {
		return "🤖 LLM 策略"
	}
	return "⚙️ 固化策略"
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
