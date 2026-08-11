package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/config"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/telegram"
	"github.com/jiayu/wbot/internal/wheelstore"
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

// wheelTelegramStore is the store surface the Telegram confirm loop needs
// (wheelstore.Store satisfies it; unit tests inject fakes).
type wheelTelegramStore interface {
	GetSignal(ctx context.Context, id int64) (*wheelstore.SignalRecord, error)
	LatestLLMReview(ctx context.Context, signalID int64) (*wheelstore.ActionRecord, error)
	HasAction(ctx context.Context, signalID int64, action string) (bool, error)
	AppendAction(ctx context.Context, r wheelstore.ActionRecord) (int64, error)
	QuerySignalsSince(ctx context.Context, action string, afterID int64, limit int) ([]wheelstore.SignalRecord, error)
	MaxSignalID(ctx context.Context) (int64, error)
	Dismiss(ctx context.Context, symbol string, date time.Time) error
	IsDismissed(ctx context.Context, symbol string, date time.Time) (bool, error)
}

// wheelOrderPlacer submits a sim-env market option order (futu proto adapter;
// fakes in tests).
type wheelOrderPlacer interface {
	PlaceOrder(ctx context.Context, symbol, side string, qty float64) (orderIDEx string, orderID uint64, err error)
}

// futuOrderPlacer adapts the proto TradeClient to wheelOrderPlacer: it opens
// the gateway per order, resolves the env account and submits a market order
// (price 0). Real-env placement is refused with errLiveEnvNotAllowed (the
// wheel confirm path is sim-only by design).
type futuOrderPlacer struct {
	addr string
	env  futu.Env
}

func (p futuOrderPlacer) PlaceOrder(ctx context.Context, symbol, side string, qty float64) (string, uint64, error) {
	if p.env != futu.EnvSim {
		return "", 0, errLiveEnvNotAllowed
	}
	tc, err := futu.OpenTrade(ctx, p.addr)
	if err != nil {
		return "", 0, fmt.Errorf("open trade: %w", err)
	}
	defer tc.Close()
	acc, err := tc.Account(ctx, p.env, 0)
	if err != nil {
		return "", 0, err
	}
	return tc.PlaceOrder(ctx, acc, futu.OrderRequest{Symbol: symbol, Side: side, Qty: qty, Price: 0})
}

// telegramScheduler pushes wheel ALERT signals to Telegram and disposes the
// yes/no/dismiss buttons. It owns no goroutines itself: startTelegramScheduler
// runs one push loop and one long-poll loop, both on the serve context.
type telegramScheduler struct {
	tg      *telegram.Client
	store   wheelTelegramStore
	orders  wheelOrderPlacer
	chatIDs map[int64]bool
	now     func() time.Time
	logf    func(format string, a ...any)
}

func newTelegramScheduler(tg *telegram.Client, store wheelTelegramStore, orders wheelOrderPlacer, chatIDs map[int64]bool) *telegramScheduler {
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
// never replays history; the cursor advances regardless of push outcome so a
// rejected/unapproved signal is not retried forever. A failed MaxSignalID
// retries before the ticker starts: polling with a zero cursor would replay
// every historical ALERT once the DB recovers.
func (s *telegramScheduler) runPush(ctx context.Context, interval time.Duration) error {
	var cursor int64
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
		for _, sig := range signals {
			cursor = sig.ID
			s.pushSignal(ctx, sig)
		}
	}
}

// pushSignal sends one ALERT to every whitelisted chat when it survives the
// dismissal and LLM-approval gates; skips are logged with the reason.
func (s *telegramScheduler) pushSignal(ctx context.Context, sig wheelstore.SignalRecord) {
	if dismissed, err := s.store.IsDismissed(ctx, sig.Symbol, utcDate(s.now())); err != nil {
		s.logf("push: %s signal=%d: dismissed check: %v", sig.Symbol, sig.ID, err)
		return
	} else if dismissed {
		s.logf("push: %s signal=%d: dismissed for today, skip", sig.Symbol, sig.ID)
		return
	}
	review, err := s.store.LatestLLMReview(ctx, sig.ID)
	if err != nil || verdictOf(review) != "APPROVE" {
		s.logf("push: %s signal=%d: not pushed (LLM review not APPROVE: %v)", sig.Symbol, sig.ID, err)
		return
	}
	text, err := alertMessage(&sig)
	if err != nil {
		s.logf("push: %s signal=%d: %v", sig.Symbol, sig.ID, err)
		return
	}
	buttons := []telegram.Button{
		{Text: "是,下单", Data: fmt.Sprintf("wheel:%d:yes", sig.ID)},
		{Text: "否,等待机会", Data: fmt.Sprintf("wheel:%d:no", sig.ID)},
		{Text: "今日不再提醒", Data: fmt.Sprintf("wheel:%d:dismiss", sig.ID)},
	}
	for chatID := range s.chatIDs {
		if err := s.tg.SendMessage(ctx, strconv.FormatInt(chatID, 10), text, buttons); err != nil {
			s.logf("push: %s signal=%d chat=%d: %v", sig.Symbol, sig.ID, chatID, err)
		}
	}
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
	case "dismiss":
		s.recordDismiss(ctx, cq, signalID)
	default:
		_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, "未知操作")
	}
}

// confirmOrder is the yes path: re-verify the signal is fresh and its latest
// LLM review is APPROVE, then place a sim-env market order and record
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
	// Dedup: a second chat or a double-press in the same freshness window
	// must not place a second order on the same signal.
	if confirmed, err := s.store.HasAction(ctx, signalID, "CONFIRM"); err != nil {
		s.reject(ctx, cq, signalID, "confirm check failed", "下单状态检查失败")
		return
	} else if confirmed {
		s.reject(ctx, cq, signalID, "already confirmed", "该信号已下单,请勿重复确认")
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
	orderIDEx, orderID, err := s.orders.PlaceOrder(ctx, cand.Code, cand.Side, float64(cand.Quantity))
	if err != nil {
		reason := "place order failed"
		toast := "下单失败"
		if errors.Is(err, errLiveEnvNotAllowed) {
			reason = "live env not allowed"
			toast = "实盘下单不允许(仅模拟盘)"
		}
		s.reject(ctx, cq, signalID, reason, toast)
		return
	}
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{
		SignalID: signalID, Action: "CONFIRM", Actor: telegramActor(cq),
		Details: map[string]any{"order_id": orderID, "order_id_ex": orderIDEx, "symbol": cand.Code, "side": cand.Side, "qty": cand.Quantity},
	}); err != nil {
		s.logf("callback %s: confirm record: %v", cq.ID, err)
	}
	_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, fmt.Sprintf("已下单 订单号 %d", orderID))
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
}

// reject records REJECTED with the reason and answers the callback.
func (s *telegramScheduler) reject(ctx context.Context, cq *telegram.CallbackQuery, signalID int64, reason, toast string) {
	if _, err := s.store.AppendAction(ctx, wheelstore.ActionRecord{SignalID: signalID, Action: "REJECTED", Actor: telegramActor(cq), Note: reason}); err != nil {
		s.logf("callback %s: rejected record: %v", cq.ID, err)
	}
	_ = s.tg.AnswerCallbackQuery(ctx, cq.ID, toast)
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

// candidateOrder is the first candidate's order facts (signals store
// candidates as opaque JSON maps, so this decodes them).
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
	Delta        float64
	ImpliedVol   float64
	OpenInterest int64
}

// firstCandidate decodes the signal's first candidate into order facts. The
// wheel sells the option in both directions (PUT sell / CALL sell), so side
// is always sell; quantity defaults to 1 when missing.
func firstCandidate(sig *wheelstore.SignalRecord) (*candidateOrder, error) {
	if sig == nil || len(sig.Candidates) == 0 {
		return nil, errors.New("signal has no candidates")
	}
	raw, err := json.Marshal(sig.Candidates[0])
	if err != nil {
		return nil, fmt.Errorf("candidate encode: %w", err)
	}
	var c struct {
		Direction string `json:"direction"`
		Quantity  int    `json:"quantity"`
		Quote     struct {
			Symbol       string  `json:"symbol"`
			OptionType   string  `json:"option_type"`
			Strike       float64 `json:"strike"`
			Expiry       string  `json:"expiry"`
			Bid          float64 `json:"bid"`
			Ask          float64 `json:"ask"`
			Delta        float64 `json:"delta"`
			ImpliedVol   float64 `json:"implied_vol"`
			OpenInterest int64   `json:"open_interest"`
		} `json:"quote"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("candidate decode: %w", err)
	}
	if strings.TrimSpace(c.Quote.Symbol) == "" {
		return nil, errors.New("candidate has no option symbol")
	}
	direction := strings.ToUpper(strings.TrimSpace(c.Direction))
	if direction != "PUT" && direction != "CALL" {
		return nil, fmt.Errorf("candidate direction %q unsupported", c.Direction)
	}
	qty := c.Quantity
	if qty < 1 {
		qty = 1
	}
	return &candidateOrder{
		Code: c.Quote.Symbol, Side: "sell", Quantity: qty, Direction: direction,
		Quote: candidateQuote{Symbol: c.Quote.Symbol, OptionType: c.Quote.OptionType, Strike: c.Quote.Strike, Expiry: c.Quote.Expiry, Bid: c.Quote.Bid, Ask: c.Quote.Ask, Delta: c.Quote.Delta, ImpliedVol: c.Quote.ImpliedVol, OpenInterest: c.Quote.OpenInterest},
	}, nil
}

// alertMessage renders the simple order instruction (方向/数量/候选期权 行权·
// 到期·bid-ask·Δ·IV·OI/现价/库存缺口/信号 id/LLM 审核 APPROVE).
func alertMessage(sig *wheelstore.SignalRecord) (string, error) {
	c, err := firstCandidate(sig)
	if err != nil {
		return "", err
	}
	expiry := c.Quote.Expiry
	if t, err := time.Parse(time.RFC3339, expiry); err == nil {
		expiry = t.Format("2006-01-02")
	}
	lines := []string{
		fmt.Sprintf("[WHEEL 提醒] %s", sig.Symbol),
		fmt.Sprintf("方向: %s", directionLabel(c.Direction)),
		fmt.Sprintf("数量: %d 张", c.Quantity),
		fmt.Sprintf("候选: %s", c.Code),
		fmt.Sprintf("  行权 %.2f | 到期 %s", c.Quote.Strike, expiry),
		fmt.Sprintf("  bid %.2f | ask %.2f | Δ %.3f | IV %.2f | OI %d", c.Quote.Bid, c.Quote.Ask, c.Quote.Delta, c.Quote.ImpliedVol, c.Quote.OpenInterest),
		fmt.Sprintf("现价: %s", priceText(sig.Inventory.CurrentPrice)),
		fmt.Sprintf("库存缺口: %s", priceText(sig.Inventory.InventoryGap)),
		fmt.Sprintf("信号 #%d | LLM 审核: APPROVE", sig.ID),
	}
	return strings.Join(lines, "\n"), nil
}

// directionLabel renders the wheel direction for the alert text (both
// directions sell the option).
func directionLabel(direction string) string {
	switch direction {
	case "PUT":
		return "卖出认沽 (SELL PUT)"
	case "CALL":
		return "卖出认购 (SELL CALL)"
	}
	return direction
}

func priceText(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *p)
}
