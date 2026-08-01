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
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/qtopie/gofutuapi"
	qotcommon "github.com/qtopie/gofutuapi/gen/qot/common"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

// DefaultProtoAddr is the OpenD protobuf API endpoint of the gateway.
const DefaultProtoAddr = "127.0.0.1:11111"

// Env is a Futu trading environment (proto TrdEnv: 0=simulate, 1=real).
type Env int

const (
	EnvSim  Env = 0 // paper trading (默认, 安全红线)
	EnvReal Env = 1 // live trading (只读查询; 写操作需老板确认)
)

// TradeClient is a protobuf connection to the futu gateway (TCP 11111).
type TradeClient struct {
	api *gofutuapi.FutuApiConn
	cli *gofutuapi.FutuClient
}

// OpenTrade connects to addr and completes the OpenD init handshake.
func OpenTrade(ctx context.Context, addr string) (*TradeClient, error) {
	// gofutuapi logs lifecycle chatter (init/reconnect) to stdlib log; wbot
	// does not use stdlib log elsewhere, silence it for clean CLI output.
	log.SetOutput(io.Discard)
	api, err := gofutuapi.Open(ctx, gofutuapi.FutuApiOption{Address: addr})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	return &TradeClient{api: api, cli: gofutuapi.NewClient(api)}, nil
}

// Close closes the underlying protobuf connection.
func (tc *TradeClient) Close() error {
	return tc.api.Close()
}

// Account resolves the account for env: with accID != 0 that exact account is
// required (readable error when missing or trd_env-mismatched); with accID ==
// 0 the first account of env from the gateway list is returned.
func (tc *TradeClient) Account(ctx context.Context, env Env, accID uint64) (*trdcommon.TrdAcc, error) {
	if err := QuoteLimit.Wait(ctx); err != nil {
		return nil, err
	}
	accounts, err := tc.cli.GetTradeAccounts()
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
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
	funds, err := tc.cli.GetFundsForAccount(acc, true)
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
	positions, err := tc.cli.GetPositionsForAccount(acc, true)
	if err != nil {
		return nil, fmt.Errorf("positions acc %d: %w", acc.GetAccID(), err)
	}
	return positions, nil
}

// OrderRequest describes an order for PlaceOrder (validated there).
type OrderRequest struct {
	Symbol string  // market-qualified code, e.g. HK.00700
	Side   string  // "buy" or "sell"
	Qty    float64 // shares, > 0
	Price  float64 // limit price; 0 => market order
}

// PlaceOrder submits req on acc; the caller (CLI) enforces the live-write
// safety guard (实盘写操作需 --live-confirm, 见 doc/FUTU.md 交易安全策略).
func (tc *TradeClient) PlaceOrder(ctx context.Context, acc *trdcommon.TrdAcc, req OrderRequest) (orderIDEx string, orderID uint64, err error) {
	market, code, err := ParseSymbol(req.Symbol)
	if err != nil {
		return "", 0, err
	}
	if req.Qty <= 0 {
		return "", 0, fmt.Errorf("bad qty %v (want > 0)", req.Qty)
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
	if price <= 0 {
		orderType = trdcommon.OrderType_OrderType_Market
	}
	if err := QuoteLimit.Wait(ctx); err != nil {
		return "", 0, err
	}
	orderIDEx, orderID, err = tc.cli.PlaceOrder(acc, side, orderType, code, req.Qty, price,
		qotcommon.QotMarket(market), trdcommon.TrdMarket(market), trdcommon.TimeInForce_TimeInForce_DAY)
	if err != nil {
		return "", 0, fmt.Errorf("order %s qty %v: %w", req.Symbol, req.Qty, err)
	}
	return orderIDEx, orderID, nil
}
