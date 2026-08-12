package futu

// Protobuf trade client (OpenD protocol, TCP 11111). Path decision 2026-08-01
// (boss directive): trade commands MUST use the proto interface, not REST —
// the gateway blocks REST mutating endpoints without keys.json (legacy mode)
// and the safety policy keeps that block in place. Transport + generated proto
// come from qtopie/gofutuapi (MIT, github.com/qtopie/gofutuapi), the reference
// Go client; go.mod moved to go 1.24.4 to match its toolchain requirement.
// Rate limiting: every trade request shares the global QuoteLimit pool.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/qtopie/gofutuapi"
	qotcommon "github.com/qtopie/gofutuapi/gen/qot/common"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

// DefaultProtoAddr is the OpenD protobuf API endpoint of the gateway.
const DefaultProtoAddr = "127.0.0.1:11111"

const tradeConnectTimeout = 10 * time.Second

// Env is a Futu trading environment (proto TrdEnv: 0=simulate, 1=real).
type Env int

const (
	EnvSim  Env = 0 // paper trading (默认, 安全红线)
	EnvReal Env = 1 // live trading (只读查询; 写操作需老板确认)
)

// TradeClient is a lease on the process-wide protobuf connection for an
// address. Close releases the lease; the underlying long-lived connection is
// retained for reuse.
type TradeClient struct {
	shared    *sharedTradeConn
	closeOnce sync.Once
}

type sharedTradeConn struct {
	mu   sync.Mutex
	addr string
	api  *gofutuapi.FutuApiConn
	cli  *gofutuapi.FutuClient
	refs int
}

var tradeConnections = struct {
	sync.Mutex
	byAddr map[string]*sharedTradeConn
}{byAddr: make(map[string]*sharedTradeConn)}

// AcquireTrade returns a lease on the process-wide connection for addr. The
// first lease performs the init handshake; later leases reuse that connection.
// Requests are serialized by sharedTradeConn because the SDK is not safe for
// concurrent request/reply use.
func AcquireTrade(ctx context.Context, addr string) (*TradeClient, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = DefaultProtoAddr
	}
	// gofutuapi logs lifecycle chatter (init/reconnect) to stdlib log; wbot
	// does not use stdlib log elsewhere, silence it for clean CLI output.
	log.SetOutput(io.Discard)

	tradeConnections.Lock()
	shared := tradeConnections.byAddr[addr]
	if shared == nil {
		shared = &sharedTradeConn{addr: addr}
		tradeConnections.byAddr[addr] = shared
	}
	shared.refs++
	tradeConnections.Unlock()

	shared.mu.Lock()
	err := shared.connect(ctx)
	shared.mu.Unlock()
	if err != nil {
		tradeConnections.Lock()
		shared.refs--
		tradeConnections.Unlock()
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	return &TradeClient{shared: shared}, nil
}

// OpenTrade is kept as a source-compatible alias. It has the same pooled
// semantics as AcquireTrade and never creates a per-call connection.
func OpenTrade(ctx context.Context, addr string) (*TradeClient, error) {
	return AcquireTrade(ctx, addr)
}

func (s *sharedTradeConn) connect(ctx context.Context) error {
	if s.api != nil {
		return nil
	}
	api, err := gofutuapi.Open(ctx, gofutuapi.FutuApiOption{
		Address: s.addr,
		Timeout: tradeConnectTimeout,
	})
	if err != nil {
		return err
	}
	s.api = api
	s.cli = gofutuapi.NewClient(api)
	return nil
}

func (s *sharedTradeConn) invalidate() {
	if s.api != nil {
		_ = s.api.Close()
	}
	s.api = nil
	s.cli = nil
}

// Close releases this lease. The process-wide connection deliberately remains
// open for later users of the same addr.
func (tc *TradeClient) Close() error {
	if tc == nil || tc.shared == nil {
		return nil
	}
	tc.closeOnce.Do(func() {
		tradeConnections.Lock()
		tc.shared.refs--
		tradeConnections.Unlock()
	})
	return nil
}

// withClient serializes one SDK call and retries it at most once after a
// transport failure. Thus a logical operation creates at most two connections;
// the stale connection is always closed before the replacement is dialed.
func (tc *TradeClient) withClient(ctx context.Context, fn func(*gofutuapi.FutuClient) error) error {
	if tc == nil || tc.shared == nil {
		return fmt.Errorf("trade client is nil")
	}
	s := tc.shared
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err = s.connect(ctx); err != nil {
			return fmt.Errorf("connect %s: %w", s.addr, err)
		}
		err = fn(s.cli)
		if err == nil || !isTradeConnError(err) {
			return err
		}
		s.invalidate()
	}
	return err
}

func isTradeConnError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection closed") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "timeout waiting for reply")
}

// Account resolves the account for env: with accID != 0 that exact account is
// required (readable error when missing or trd_env-mismatched); with accID ==
// 0 the first account of env from the gateway list is returned.
func (tc *TradeClient) Account(ctx context.Context, env Env, accID uint64) (*trdcommon.TrdAcc, error) {
	accounts, err := tc.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var matched []*trdcommon.TrdAcc
	for _, acc := range accounts {
		if acc.GetTrdEnv() == int32(env) {
			matched = append(matched, acc)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no %s account (trd_env=%d) among %d accounts", EnvName(env), env, len(accounts))
	}
	if accID == 0 {
		return matched[0], nil
	}
	for _, acc := range matched {
		if acc.GetAccID() == accID {
			return acc, nil
		}
	}
	return nil, fmt.Errorf("account %d not found in %s env (trd_env mismatch?)", accID, EnvName(env))
}

// ListAccounts returns every gateway account (all envs) so callers can
// discover the option sim account (SimAccType=Option) vs the stock sim account
// (SimAccType=Stock, 不支持期权; 老板指令 2026-08-12: 切换到期权模拟账户)。
func (tc *TradeClient) ListAccounts(ctx context.Context) ([]*trdcommon.TrdAcc, error) {
	if err := QuoteLimit.Wait(ctx); err != nil {
		return nil, err
	}
	var accounts []*trdcommon.TrdAcc
	err := tc.withClient(ctx, func(cli *gofutuapi.FutuClient) error {
		var err error
		accounts, err = cli.GetTradeAccounts()
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	return accounts, nil
}

// SimAccTypeName renders a SimAccType enum for user-facing output.
func SimAccTypeName(t int32) string {
	switch trdcommon.SimAccType(t) {
	case trdcommon.SimAccType_SimAccType_Stock:
		return "stock (不支持期权)"
	case trdcommon.SimAccType_SimAccType_Option:
		return "option (仅期权)"
	case trdcommon.SimAccType_SimAccType_Futures:
		return "futures"
	}
	return "unknown"
}

// IsOptionCode reports whether code looks like an option contract (富途格式:
// 字母前缀 + YYMMDD + C/P + 行权价, e.g. TCH260821P460000; 正股是纯数字
// 00700/00883)。用于下单账户解析:期权必须落在期权模拟账户(SimAccType=Option,
// 实测 2026-08-12: 股票模拟账户拒绝期权「Can't trade this type of securities」)。
var optionCodeRe = regexp.MustCompile(`^[A-Z]{1,6}\d{6}[CP]\d{1,10}$`)

func IsOptionCode(code string) bool { return optionCodeRe.MatchString(code) }

// AccountForSymbol resolves the account for env and symbol: with accID != 0
// that exact account (original behavior); with accID == 0 the account is
// auto-selected by market + security type — 期权走 SimAccType=Option 账户,
// 正股走 SimAccType=Stock 账户, 市场取 TrdMarketAuthList 匹配 (老板指令
// 2026-08-12: 支持港股期权/美股等多模拟账户切换; 多账户时不再默认取第一个)。
// No match is an error listing the available sim accounts (fail-closed: 宁
// 可不交易, 不冒名交易到错误的账户上)。
func (tc *TradeClient) AccountForSymbol(ctx context.Context, env Env, symbol string, accID uint64) (*trdcommon.TrdAcc, error) {
	if accID != 0 {
		return tc.Account(ctx, env, accID)
	}
	market, code, err := ParseSymbol(symbol)
	if err != nil {
		return nil, err
	}
	accounts, err := tc.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	option := IsOptionCode(code)
	var matched []*trdcommon.TrdAcc
	for _, acc := range accounts {
		if acc.GetTrdEnv() != int32(env) {
			continue
		}
		if option != (acc.GetSimAccType() == int32(trdcommon.SimAccType_SimAccType_Option)) {
			continue
		}
		for _, m := range acc.GetTrdMarketAuthList() {
			if m == int32(market) {
				matched = append(matched, acc)
				break
			}
		}
	}
	if len(matched) == 0 {
		var names []string
		for _, acc := range accounts {
			if acc.GetTrdEnv() == int32(env) {
				names = append(names, fmt.Sprintf("acc=%d type=%s markets=%v", acc.GetAccID(), SimAccTypeName(acc.GetSimAccType()), acc.GetTrdMarketAuthList()))
			}
		}
		return nil, fmt.Errorf("no %s account for %s (option=%v, market=%d); available: %s",
			EnvName(env), symbol, option, market, strings.Join(names, "; "))
	}
	return matched[0], nil
}

// EnvName renders an Env for user-facing messages and JSON output.
func EnvName(env Env) string {
	if env == EnvReal {
		return "real"
	}
	return "simulate"
}

// Funds queries account funds (read-only; safe for real accounts).
func (tc *TradeClient) Funds(ctx context.Context, acc *trdcommon.TrdAcc) (*trdcommon.Funds, error) {
	if err := QuoteLimit.Wait(ctx); err != nil {
		return nil, err
	}
	var funds *trdcommon.Funds
	err := tc.withClient(ctx, func(cli *gofutuapi.FutuClient) error {
		var err error
		funds, err = cli.GetFundsForAccount(acc, true)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("funds acc %d: %w", acc.GetAccID(), err)
	}
	if funds == nil {
		return nil, fmt.Errorf("funds acc %d: empty response", acc.GetAccID())
	}
	return funds, nil
}

// Positions queries account positions (read-only; safe for real accounts).
func (tc *TradeClient) Positions(ctx context.Context, acc *trdcommon.TrdAcc) ([]*trdcommon.Position, error) {
	if err := QuoteLimit.Wait(ctx); err != nil {
		return nil, err
	}
	var positions []*trdcommon.Position
	err := tc.withClient(ctx, func(cli *gofutuapi.FutuClient) error {
		var err error
		positions, err = cli.GetPositionsForAccount(acc, true)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("positions acc %d: %w", acc.GetAccID(), err)
	}
	return positions, nil
}

// Orders queries the account's order list (read-only; safe for real accounts).
// With pendingOnly the list is filtered to open/pending states (挂单, the
// default view); otherwise all orders including filled/cancelled.
func (tc *TradeClient) Orders(ctx context.Context, acc *trdcommon.TrdAcc, pendingOnly bool) ([]*trdcommon.Order, error) {
	if err := QuoteLimit.Wait(ctx); err != nil {
		return nil, err
	}
	var statuses []int32
	if pendingOnly {
		statuses = gofutuapi.PendingOrderStatuses()
	}
	var orders []*trdcommon.Order
	err := tc.withClient(ctx, func(cli *gofutuapi.FutuClient) error {
		var err error
		orders, err = cli.GetOrderListForAccount(acc, statuses, true)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("orders acc %d: %w", acc.GetAccID(), err)
	}
	return orders, nil
}

// OrderRequest describes an order for PlaceOrder (validated there).
type OrderRequest struct {
	Symbol string  // market-qualified code, e.g. HK.00700
	Side   string  // "buy" or "sell"
	Qty    float64 // shares, > 0
	Price  float64 // limit price, MUST be > 0 (市价单禁止, 老板指令 2026-08-12)
}

// PlaceOrder submits req on acc; the caller (CLI) enforces the live-write
// safety guard (实盘写操作需 --live-confirm, 见 doc/FUTU.md 交易安全策略).
// 限价单纪律(老板指令 2026-08-12「所有策略禁止市价单,我只用限价单」):
// price <= 0 fail-closed,永远不发起 Market order — 网关对 price=0 也拒绝
// (实测 2026-08-12:「price=0 非法,必须为正数」),此校验覆盖所有调用方
// (wheel 信号链路、LLM 信号链路、CLI),调用方必须显式提供限价。
// 边界(老板澄清 2026-08-12):多腿复杂订单(IB 风格 spread)的净价可为负
// (净收入,credit),但那是独立的订单模型(逐腿价格),不适用本单腿校验 —
// 本包仅富途单腿订单,多腿负价订单需引入新的 OrderRequest 变体再放开。
func (tc *TradeClient) PlaceOrder(ctx context.Context, acc *trdcommon.TrdAcc, req OrderRequest) (orderIDEx string, orderID uint64, err error) {
	market, code, err := ParseSymbol(req.Symbol)
	if err != nil {
		return "", 0, err
	}
	if req.Qty <= 0 {
		return "", 0, fmt.Errorf("bad qty %v (want > 0)", req.Qty)
	}
	if req.Price <= 0 {
		return "", 0, fmt.Errorf("bad price %v (want > 0; 市价单禁止,必须显式限价)", req.Price)
	}
	var side trdcommon.TrdSide
	switch strings.ToLower(req.Side) {
	case "buy":
		side = trdcommon.TrdSide_TrdSide_Buy
	case "sell":
		side = trdcommon.TrdSide_TrdSide_Sell
	default:
		return "", 0, fmt.Errorf("bad side %q (want buy or sell)", req.Side)
	}
	orderType := trdcommon.OrderType_OrderType_Normal
	price := req.Price
	trdMkt, err := trdMarket(market)
	if err != nil {
		return "", 0, err
	}
	if err := QuoteLimit.Wait(ctx); err != nil {
		return "", 0, err
	}
	err = tc.withClient(ctx, func(cli *gofutuapi.FutuClient) error {
		orderIDEx, orderID, err = cli.PlaceOrder(acc, side, orderType, code, req.Qty, price,
			qotcommon.QotMarket(market), trdMkt, trdcommon.TimeInForce_TimeInForce_DAY)
		return err
	})
	if err != nil {
		return "", 0, fmt.Errorf("order %s qty %v: %w", req.Symbol, req.Qty, err)
	}
	return orderIDEx, orderID, nil
}

// trdMarket maps the QotMarket enum (from ParseSymbol) to the TrdMarket enum;
// the value domains differ (QotMarket: HK=1 US=11 SH=21 SZ=22; TrdMarket:
// HK=1 US=2 CN=3), so a direct cast would misroute US/CN orders.
func trdMarket(market int) (trdcommon.TrdMarket, error) {
	switch market {
	case 1:
		return trdcommon.TrdMarket_TrdMarket_HK, nil
	case 11:
		return trdcommon.TrdMarket_TrdMarket_US, nil
	case 21, 22:
		return trdcommon.TrdMarket_TrdMarket_CN, nil
	}
	return 0, fmt.Errorf("unsupported market %d for trading", market)
}
