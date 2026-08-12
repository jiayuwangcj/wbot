package futu

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/futu/fakegw"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
	"github.com/qtopie/gofutuapi/gen/trade/trdgetfunds"
	"github.com/qtopie/gofutuapi/gen/trade/trdplaceorder"
	"google.golang.org/protobuf/proto"
)

const (
	protoInit  = 1001
	protoAccs  = 2001 // TRD_GETACCLIST
	protoFunds = 2101 // TRD_GETFUNDS
	protoPos   = 2102 // TRD_GETPOSITIONLIST
	protoOrder = 2202 // TRD_PLACEORDER
)

// openTrade connects to a fake gateway address with cleanup ordering that
// cancels the context before closing (prevents gofutuapi auto-reconnect loops).
func openTrade(t *testing.T, addr string) *TradeClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tc, err := OpenTrade(ctx, addr)
	if err != nil {
		t.Fatalf("OpenTrade: %v", err)
	}
	t.Cleanup(func() { tc.Close() })
	t.Cleanup(cancel)
	return tc
}

// handler wraps h with the mandatory INIT_CONNECT answer.
func handler(h fakegw.Handler) fakegw.Handler {
	return func(protoID int32, body []byte) []byte {
		if protoID == protoInit {
			return fakegw.InitBody(42)
		}
		return h(protoID, body)
	}
}

func TestOpenTradeRefused(t *testing.T) {
	// Closed listener port: connect must fail with a readable error.
	ln, err := netListen()
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	started := time.Now()
	tc, err := OpenTrade(context.Background(), addr)
	if err == nil {
		tc.Close()
		t.Fatal("OpenTrade: want error, got nil")
	}
	if !strings.Contains(err.Error(), "connect") {
		t.Fatalf("OpenTrade: want readable connect error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("OpenTrade refused took %v; want fast error", elapsed)
	}
}

func TestOpenTradeHandshakeTimeout(t *testing.T) {
	ln, err := netListen()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	started := time.Now()
	tc, err := OpenTrade(context.Background(), ln.Addr().String())
	elapsed := time.Since(started)
	if err == nil {
		tc.Close()
		t.Fatal("OpenTrade: want init handshake timeout, got nil")
	}
	if elapsed < 10*time.Second || elapsed > 15*time.Second {
		t.Fatalf("OpenTrade timeout took %v; want 10-15s", elapsed)
	}
	select {
	case conn := <-accepted:
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 1024)
		for {
			if _, readErr := conn.Read(buf); readErr != nil {
				if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
					t.Fatal("timed-out init connection remains open")
				}
				break
			}
		}
	case <-time.After(time.Second):
		t.Fatal("mock gateway did not accept init connection")
	}
}

func TestTradeConnectionReusedByAddress(t *testing.T) {
	addr, accepts := fakegw.ServerWithAcceptCount(t, func(protoID int32, body []byte) []byte {
		if protoID == protoInit {
			return fakegw.InitBody(42)
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	})

	for i := 0; i < 2; i++ {
		tc, err := AcquireTrade(context.Background(), addr)
		if err != nil {
			t.Fatalf("AcquireTrade call %d: %v", i+1, err)
		}
		if _, err := tc.Account(context.Background(), EnvSim, 0); err != nil {
			t.Fatalf("Account call %d: %v", i+1, err)
		}
		if err := tc.Close(); err != nil {
			t.Fatalf("Close call %d: %v", i+1, err)
		}
	}
	if got := accepts.Load(); got != 1 {
		t.Fatalf("accepted connections = %d; want 1", got)
	}
}

func TestTradeReconnectsOnceAfterDisconnect(t *testing.T) {
	var accounts atomic.Int32
	addr, accepts := fakegw.ServerWithAcceptCount(t, func(protoID int32, body []byte) []byte {
		switch protoID {
		case protoInit:
			return fakegw.InitBody(42)
		case protoAccs:
			if accounts.Add(1) == 1 {
				return nil
			}
			return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
		default:
			return nil
		}
	})
	tc := openTrade(t, addr)
	acc, err := tc.Account(context.Background(), EnvSim, 0)
	if err != nil {
		t.Fatalf("Account after disconnect: %v", err)
	}
	if acc.GetAccID() != 1907141 {
		t.Fatalf("account id = %d; want 1907141", acc.GetAccID())
	}
	if got := accepts.Load(); got != 2 {
		t.Fatalf("accepted connections = %d; want 2 (initial + one reconnect)", got)
	}
}

func TestTradeRetryAttemptsAtMostTwo(t *testing.T) {
	addr, accepts := fakegw.ServerWithAcceptCount(t, func(protoID int32, body []byte) []byte {
		if protoID == protoInit {
			return fakegw.InitBody(42)
		}
		return nil
	})
	tc := openTrade(t, addr)
	if _, err := tc.Account(context.Background(), EnvSim, 0); err == nil {
		t.Fatal("Account: want disconnect error, got nil")
	}
	if got := accepts.Load(); got != 2 {
		t.Fatalf("accepted connections = %d; want exactly 2 total attempts", got)
	}
}

func TestAccountResolution(t *testing.T) {
	addr := fakegw.Server(t, handler(func(protoID int32, body []byte) []byte {
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{
			fakegw.Acc(1, 281756478875559548, 1, 2),
			fakegw.Acc(0, 1907141, 1),
			fakegw.Acc(0, 13477968, 1),
		})
	}))
	tc := openTrade(t, addr)

	acc, err := tc.Account(context.Background(), EnvSim, 0)
	if err != nil {
		t.Fatalf("Account sim default: %v", err)
	}
	if acc.GetAccID() != 1907141 {
		t.Fatalf("sim default acc = %d; want 1907141", acc.GetAccID())
	}
	acc, err = tc.Account(context.Background(), EnvReal, 0)
	if err != nil {
		t.Fatalf("Account real default: %v", err)
	}
	if acc.GetAccID() != 281756478875559548 {
		t.Fatalf("real default acc = %d; want 281756478875559548", acc.GetAccID())
	}
	acc, err = tc.Account(context.Background(), EnvSim, 13477968)
	if err != nil {
		t.Fatalf("Account sim by id: %v", err)
	}
	if acc.GetAccID() != 13477968 {
		t.Fatalf("sim by id acc = %d; want 13477968", acc.GetAccID())
	}
	// Real account id asked under sim env: readable trd_env mismatch error.
	_, err = tc.Account(context.Background(), EnvSim, 281756478875559548)
	if err == nil || !strings.Contains(err.Error(), "not found in simulate") {
		t.Fatalf("env mismatch: want readable error, got %v", err)
	}
}

func TestAccountEnvEmpty(t *testing.T) {
	addr := fakegw.Server(t, handler(func(protoID int32, body []byte) []byte {
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(1, 281756478875559548, 1)})
	}))
	tc := openTrade(t, addr)
	_, err := tc.Account(context.Background(), EnvSim, 0)
	if err == nil || !strings.Contains(err.Error(), "no simulate account") {
		t.Fatalf("want no-sim-account error, got %v", err)
	}
}

func TestFunds(t *testing.T) {
	var gotReq *trdgetfunds.Request
	addr := fakegw.Server(t, handler(func(protoID int32, body []byte) []byte {
		if protoID == protoFunds {
			gotReq = &trdgetfunds.Request{}
			proto.Unmarshal(body, gotReq)
			return fakegw.FundsBody(0, 1907141, &trdcommon.Funds{
				Power:             proto.Float64(0),
				TotalAssets:       proto.Float64(1198286.822),
				Cash:              proto.Float64(318666.822),
				MarketVal:         proto.Float64(879620),
				FrozenCash:        proto.Float64(0),
				DebtCash:          proto.Float64(0),
				AvlWithdrawalCash: proto.Float64(0),
			})
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	}))
	tc := openTrade(t, addr)

	acc, err := tc.Account(context.Background(), EnvSim, 0)
	if err != nil {
		t.Fatal(err)
	}
	funds, err := tc.Funds(context.Background(), acc)
	if err != nil {
		t.Fatalf("Funds: %v", err)
	}
	if funds.GetTotalAssets() != 1198286.822 || funds.GetCash() != 318666.822 {
		t.Fatalf("funds mismatch: total=%v cash=%v", funds.GetTotalAssets(), funds.GetCash())
	}
	// The request header must carry the resolved account and trd_env.
	if gotReq == nil {
		t.Fatal("funds request not captured")
	}
	h := gotReq.GetC2S().GetHeader()
	if h.GetAccID() != 1907141 || h.GetTrdEnv() != 0 {
		t.Fatalf("funds header acc=%d env=%d; want 1907141/0", h.GetAccID(), h.GetTrdEnv())
	}
}

func TestPositions(t *testing.T) {
	addr := fakegw.Server(t, handler(func(protoID int32, body []byte) []byte {
		if protoID == protoPos {
			return fakegw.PositionsBody(0, 1907141, []*trdcommon.Position{
				pos("00700", "TENCENT", 100, 47520),
				pos("01810", "XIAOMI-W", 5000, 143900),
			})
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	}))
	tc := openTrade(t, addr)

	acc, err := tc.Account(context.Background(), EnvSim, 0)
	if err != nil {
		t.Fatal(err)
	}
	positions, err := tc.Positions(context.Background(), acc)
	if err != nil {
		t.Fatalf("Positions: %v", err)
	}
	if len(positions) != 2 || positions[0].GetCode() != "00700" {
		t.Fatalf("positions mismatch: %+v", positions)
	}
}

// pos builds a fully-populated Position (all proto2-required fields set).
func pos(code, name string, qty, val float64) *trdcommon.Position {
	return &trdcommon.Position{
		PositionID:   proto.Uint64(1),
		PositionSide: proto.Int32(0),
		Code:         proto.String(code),
		Name:         proto.String(name),
		Qty:          proto.Float64(qty),
		CanSellQty:   proto.Float64(qty),
		Price:        proto.Float64(0),
		Val:          proto.Float64(val),
		PlVal:        proto.Float64(0),
	}
}

func TestPlaceOrderMarketMapping(t *testing.T) {
	// QotMarket and TrdMarket enums differ (US=11 vs 2, SH/SZ=21/22 vs CN=3);
	// the request header must carry the TrdMarket value per symbol market.
	got := map[string]int32{}
	addr := fakegw.Server(t, handler(func(protoID int32, body []byte) []byte {
		if protoID == protoOrder {
			req := &trdplaceorder.Request{}
			proto.Unmarshal(body, req)
			got[req.GetC2S().GetCode()] = req.GetC2S().GetHeader().GetTrdMarket()
			return fakegw.PlaceOrderBody(0, 1907141, "EX123", 777)
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1, 2, 3)})
	}))
	tc := openTrade(t, addr)
	acc, err := tc.Account(context.Background(), EnvSim, 0)
	if err != nil {
		t.Fatal(err)
	}
	for symbol, wantMkt := range map[string]int32{
		"HK.00700":  1, // TrdMarket_HK
		"US.AAPL":   2, // TrdMarket_US
		"SH.600000": 3, // TrdMarket_CN
		"SZ.000001": 3, // TrdMarket_CN
	} {
		if _, _, err := tc.PlaceOrder(context.Background(), acc, OrderRequest{
			Symbol: symbol, Side: "buy", Qty: 100, Price: 470,
		}); err != nil {
			t.Fatalf("PlaceOrder %s: %v", symbol, err)
		}
		if gotMkt := got[strings.SplitN(symbol, ".", 2)[1]]; gotMkt != wantMkt {
			t.Fatalf("%s: trd_market = %d; want %d", symbol, gotMkt, wantMkt)
		}
	}
}

func TestPlaceOrderLimitAndMarket(t *testing.T) {
	var gotReqs []*trdplaceorder.Request
	addr := fakegw.Server(t, handler(func(protoID int32, body []byte) []byte {
		if protoID == protoOrder {
			req := &trdplaceorder.Request{}
			proto.Unmarshal(body, req)
			gotReqs = append(gotReqs, req)
			return fakegw.PlaceOrderBody(0, 1907141, "EX123", 777)
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	}))
	tc := openTrade(t, addr)

	acc, err := tc.Account(context.Background(), EnvSim, 0)
	if err != nil {
		t.Fatal(err)
	}
	ex, id, err := tc.PlaceOrder(context.Background(), acc, OrderRequest{
		Symbol: "HK.00700", Side: "buy", Qty: 100, Price: 470,
	})
	if err != nil {
		t.Fatalf("PlaceOrder limit: %v", err)
	}
	if ex != "EX123" || id != 777 {
		t.Fatalf("order ids: %q %d", ex, id)
	}
	ex, _, err = tc.PlaceOrder(context.Background(), acc, OrderRequest{
		Symbol: "HK.00700", Side: "sell", Qty: 100, // price 0 => market
	})
	if err != nil {
		t.Fatalf("PlaceOrder market: %v", err)
	}
	if ex != "EX123" {
		t.Fatalf("second order id: %q", ex)
	}
	if len(gotReqs) != 2 {
		t.Fatalf("got %d order requests; want 2", len(gotReqs))
	}
	limit := gotReqs[0].GetC2S()
	if limit.GetCode() != "00700" || limit.GetQty() != 100 || limit.GetPrice() != 470 {
		t.Fatalf("limit req mismatch: %+v", limit)
	}
	if limit.GetTrdSide() != 1 || limit.GetOrderType() != 1 || limit.GetHeader().GetAccID() != 1907141 {
		t.Fatalf("limit side/type/acc mismatch: side=%d type=%d", limit.GetTrdSide(), limit.GetOrderType())
	}
	market := gotReqs[1].GetC2S()
	if market.GetTrdSide() != 2 || market.GetOrderType() != 2 {
		t.Fatalf("market side/type mismatch: side=%d type=%d", market.GetTrdSide(), market.GetOrderType())
	}
}

func TestPlaceOrderValidation(t *testing.T) {
	tc := openTrade(t, fakegw.Server(t, handler(func(int32, []byte) []byte { return nil })))
	acc := fakegw.Acc(0, 1907141, 1)
	for name, req := range map[string]OrderRequest{
		"bad symbol": {Symbol: "00700", Side: "buy", Qty: 100},
		"bad side":   {Symbol: "HK.00700", Side: "hold", Qty: 100},
		"bad qty":    {Symbol: "HK.00700", Side: "buy", Qty: 0},
		"bad price":  {Symbol: "HK.00700", Side: "buy", Qty: 100, Price: -5},
	} {
		if _, _, err := tc.PlaceOrder(context.Background(), acc, req); err == nil {
			t.Fatalf("%s: want validation error", name)
		}
	}
}

func TestPlaceOrderBusinessError(t *testing.T) {
	addr := fakegw.Server(t, handler(func(protoID int32, body []byte) []byte {
		if protoID == protoOrder {
			b, _ := proto.Marshal(&trdplaceorder.Response{RetType: proto.Int32(-1), RetMsg: proto.String("交易解锁失败")})
			return b
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	}))
	tc := openTrade(t, addr)
	acc, err := tc.Account(context.Background(), EnvSim, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = tc.PlaceOrder(context.Background(), acc, OrderRequest{
		Symbol: "HK.00700", Side: "buy", Qty: 100, Price: 470,
	})
	if err == nil || !strings.Contains(err.Error(), "交易解锁失败") {
		t.Fatalf("want gateway business error, got %v", err)
	}
}

// netListen opens a loopback listener (used to synthesize a refused port).
func netListen() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}
