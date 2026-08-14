package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/futu/fakegw"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
	"google.golang.org/protobuf/proto"
)

const (
	protoInit  = 1001 // TRD_INITCONNECT
	protoFunds = 2101 // TRD_GETFUNDS
	protoPos   = 2102 // TRD_GETPOSITIONLIST
)

// fakeFutuAccounter is a scriptable FutuAccounter for endpoint-level tests.
type fakeFutuAccounter struct {
	err    error
	snap   AccountSnapshot
	gotEnv futu.Env
	gotID  uint64
}

func (f *fakeFutuAccounter) Account(_ context.Context, env futu.Env, accID uint64) (AccountSnapshot, error) {
	f.gotEnv, f.gotID = env, accID
	return f.snapshot(env, accID)
}

// AccountForSymbol mirrors Account; symbol→account resolution is exercised at
// the internal/futu layer with the live gateway.
func (f *fakeFutuAccounter) AccountForSymbol(_ context.Context, env futu.Env, _ string) (AccountSnapshot, error) {
	f.gotEnv = env
	return f.snapshot(env, 13477968)
}

func (f *fakeFutuAccounter) snapshot(env futu.Env, accID uint64) (AccountSnapshot, error) {
	if f.err != nil {
		return AccountSnapshot{}, f.err
	}
	if f.snap.Env != "" {
		return f.snap, nil
	}
	if accID == 0 {
		accID = 1907141 // endpoint default account
	}
	return AccountSnapshot{
		Env:   "simulate",
		AccID: accID,
		Funds: FundsJSON{Power: 1198286.822, TotalAssets: 1198286.822, Cash: 318666.822, MarketVal: 879620, AvailableCash: 318666.822},
		Positions: []PositionJSON{
			{Symbol: "HK.00700", Qty: 100, AvgCost: 470.0, Price: 475.2, MarketVal: 47520, PL: 520},
		},
	}, nil
}

func TestFutuAccountPassthrough(t *testing.T) {
	f := &fakeFutuAccounter{}
	rec := get(t, FutuAccountHandler(f), "/v1/futu/account")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q; want application/json", ct)
	}
	if f.gotEnv != futu.EnvSim || f.gotID != 0 {
		t.Fatalf("env/accID = %v/%d; want default sim account", f.gotEnv, f.gotID)
	}
	var got AccountSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got.Env != "simulate" || got.AccID != 1907141 {
		t.Fatalf("env/acc_id = %q/%d; want simulate/1907141", got.Env, got.AccID)
	}
	if got.Funds.Power != 1198286.822 || got.Funds.TotalAssets != 1198286.822 || got.Funds.Cash != 318666.822 ||
		got.Funds.MarketVal != 879620 || got.Funds.AvailableCash != 318666.822 {
		t.Fatalf("funds = %+v; want the whitelisted summary", got.Funds)
	}
	if len(got.Positions) != 1 {
		t.Fatalf("positions = %d rows; want 1", len(got.Positions))
	}
	p := got.Positions[0]
	if p.Symbol != "HK.00700" || p.Qty != 100 || p.AvgCost != 470.0 || p.Price != 475.2 || p.MarketVal != 47520 || p.PL != 520 {
		t.Fatalf("position = %+v; want the whitelisted row", p)
	}
}

// TestFutuAccountAddrEnv: the proto address comes from $FUTU_PROTO_ADDR and is
// independent of $FUTU_GATEWAY_URL (the REST transport's var) — the dual-
// semantic bug where the REST URL was fed to the proto dialer.
func TestFutuAccountAddrEnv(t *testing.T) {
	if v := os.Getenv("FUTU_PROTO_ADDR"); v != "" {
		t.Cleanup(func() { os.Setenv("FUTU_PROTO_ADDR", v) })
	} else {
		t.Cleanup(func() { os.Unsetenv("FUTU_PROTO_ADDR") })
	}
	t.Setenv("FUTU_GATEWAY_URL", "http://192.168.215.2:22222") // REST addr must not leak into proto
	if got := FutuAccountAddr(); got != futu.DefaultProtoAddr {
		t.Fatalf("addr without FUTU_PROTO_ADDR = %q; want default %q", got, futu.DefaultProtoAddr)
	}
	t.Setenv("FUTU_PROTO_ADDR", "192.168.215.2:11111")
	if got := FutuAccountAddr(); got != "192.168.215.2:11111" {
		t.Fatalf("addr with FUTU_PROTO_ADDR = %q; want the var value", got)
	}
}

func TestFutuAccountEnvAndAccID(t *testing.T) {
	f := &fakeFutuAccounter{}
	rec := get(t, FutuAccountHandler(f), "/v1/futu/account?env=real&acc_id=281756478875559548")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if f.gotEnv != futu.EnvReal || f.gotID != 281756478875559548 {
		t.Fatalf("env/accID = %v/%d; want real/281756478875559548 (read-only allowed)", f.gotEnv, f.gotID)
	}
}

func TestFutuAccountGatewayUnreachable(t *testing.T) {
	h := FutuAccountHandler(&fakeFutuAccounter{err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}})
	rec := get(t, h, "/v1/futu/account")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", rec.Code, rec.Body)
	}
	var errBody errorJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("body %q not JSON: %v", rec.Body, err)
	}
	if errBody.Code != "dependency_failed" {
		t.Fatalf("code = %q; want dependency_failed", errBody.Code)
	}
	if !strings.Contains(errBody.Action, "gateway container") {
		t.Fatalf("action = %q; want gateway-container hint", errBody.Action)
	}
	if errBody.Error != errBody.Message {
		t.Fatalf("error alias %q != message %q", errBody.Error, errBody.Message)
	}
}

func TestFutuAccountUpstreamErrorPassthrough(t *testing.T) {
	h := FutuAccountHandler(&fakeFutuAccounter{err: errors.New("no simulate account (trd_env=0) among 3 accounts")})
	rec := get(t, h, "/v1/futu/account")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d; want 502 (body %s)", rec.Code, rec.Body)
	}
	var errBody errorJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("body %q not JSON: %v", rec.Body, err)
	}
	if errBody.Message != "no simulate account (trd_env=0) among 3 accounts" {
		t.Fatalf("message = %q; want upstream message passthrough", errBody.Message)
	}
	if errBody.Code != "dependency_failed" || errBody.Action == "" {
		t.Fatalf("error body = %+v; want dependency_failed with action", errBody)
	}
}

func TestFutuAccountBadParams(t *testing.T) {
	h := FutuAccountHandler(&fakeFutuAccounter{})
	for _, path := range []string{
		"/v1/futu/account?env=production",
		"/v1/futu/account?env=simx",
		"/v1/futu/account?acc_id=abc",
		"/v1/futu/account?acc_id=-1",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d; want 400 (body %s)", path, rec.Code, rec.Body)
		}
		var errBody map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody["error"] == "" {
			t.Fatalf("%s: body %q; want JSON error", path, rec.Body)
		}
	}
}

func TestFutuAccountMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/futu/account", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	FutuAccountHandler(&fakeFutuAccounter{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; want 405 (body %s)", rec.Code, rec.Body)
	}
}

func TestFutuAccountUnknownPath(t *testing.T) {
	rec := get(t, FutuAccountHandler(&fakeFutuAccounter{}), "/v1/futu/account/extra")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (body %s)", rec.Code, rec.Body)
	}
}

// TestFutuAccountProtoGateway drives the real accounter against the fake
// OpenD-protocol gateway (proto funds/positions round-trip, no live gateway).
func TestFutuAccountProtoGateway(t *testing.T) {
	fastFutuLimits(t)
	addr := fakegw.Server(t, func(protoID int32, body []byte) []byte {
		switch protoID {
		case protoInit:
			return fakegw.InitBody(42)
		case protoFunds:
			return fakegw.FundsBody(0, 1907141, &trdcommon.Funds{
				Power:             proto.Float64(1198286.822),
				TotalAssets:       proto.Float64(1198286.822),
				Cash:              proto.Float64(318666.822),
				MarketVal:         proto.Float64(879620),
				FrozenCash:        proto.Float64(0),
				DebtCash:          proto.Float64(0),
				AvlWithdrawalCash: proto.Float64(0),
				AvailableFunds:    proto.Float64(318666.822),
			})
		case protoPos:
			return fakegw.PositionsBody(0, 1907141, []*trdcommon.Position{
				{PositionID: proto.Uint64(1), PositionSide: proto.Int32(1), Code: proto.String("00700"), Name: proto.String("TENCENT"),
					Qty: proto.Float64(100), CanSellQty: proto.Float64(100), Price: proto.Float64(475.2),
					CostPrice: proto.Float64(470.0), Val: proto.Float64(47520), PlVal: proto.Float64(520),
					SecMarket: proto.Int32(1)},
			})
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	})

	h := FutuAccountHandler(&futuAccounter{pc: newProtoClientAt(addr)})
	rec := get(t, h, "/v1/futu/account")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got AccountSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got.Env != "simulate" || got.AccID != 1907141 {
		t.Fatalf("env/acc_id = %q/%d; want simulate/1907141", got.Env, got.AccID)
	}
	if got.Funds.TotalAssets != 1198286.822 || got.Funds.AvailableCash != 318666.822 || got.Funds.Power != 1198286.822 {
		t.Fatalf("funds = %+v; want proto passthrough", got.Funds)
	}
	if len(got.Positions) != 1 || got.Positions[0].Symbol != "HK.00700" ||
		got.Positions[0].Price != 475.2 || got.Positions[0].PL != 520 {
		t.Fatalf("positions = %+v; want HK.00700 row", got.Positions)
	}
}

// TestFutuAccountReconnectAfterDrop: the gateway closes the reused proto
// connection mid-stream (observed live with opend-rs 2026-08-02); the
// accounter must reconnect and the next call must succeed instead of surfacing
// "connection closed" forever until process restart.
func TestFutuAccountReconnectAfterDrop(t *testing.T) {
	fastFutuLimits(t)
	var reqs, inits atomic.Int32
	addr := fakegw.Server(t, func(protoID int32, body []byte) []byte {
		n := reqs.Add(1)
		switch protoID {
		case protoInit:
			inits.Add(1)
			return fakegw.InitBody(42)
		case protoFunds:
			return fakegw.FundsBody(0, 1907141, &trdcommon.Funds{
				Power: proto.Float64(1000), TotalAssets: proto.Float64(1000),
				Cash: proto.Float64(1000), MarketVal: proto.Float64(0),
				FrozenCash: proto.Float64(0), DebtCash: proto.Float64(0),
				AvlWithdrawalCash: proto.Float64(0), AvailableFunds: proto.Float64(1000),
			})
		case protoPos:
			return fakegw.PositionsBody(0, 1907141, nil)
		}
		// The 5th request is call 2's accounts query on the reused conn:
		// respond nil so the fake gateway closes it, forcing a reconnect.
		if n == 5 {
			return nil
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	})

	a := &futuAccounter{pc: newProtoClientAt(addr)}
	for i := 0; i < 2; i++ {
		snap, err := a.Account(context.Background(), futu.EnvSim, 1907141)
		if err != nil {
			t.Fatalf("call %d after mid-stream drop: %v", i+1, err)
		}
		if snap.Funds.TotalAssets != 1000 {
			t.Fatalf("call %d: funds = %+v; want total_assets 1000", i+1, snap.Funds)
		}
	}
	if inits.Load() < 2 {
		t.Fatalf("init handshakes = %d; want >= 2 (a reconnect must have happened)", inits.Load())
	}
}

// TestIsConnError covers the failure signatures of a dropped proto connection;
// business errors must never trigger a reconnect.
func TestIsConnError(t *testing.T) {
	connErrors := []error{
		errors.New("accounts: get acc list failed: connection closed"),
		errors.New("accounts: get acc list failed: timeout waiting for reply SN 5"),
		errors.New("connect 192.168.215.2:11111: connection refused"),
		net.ErrClosed,
		io.EOF,
	}
	for _, err := range connErrors {
		if !isConnError(err) {
			t.Fatalf("isConnError(%q) = false; want true", err)
		}
	}
	for _, err := range []error{
		errors.New("no simulate account (trd_env=0) among 3 accounts"),
		errors.New("account 1907141 not found in simulate env (trd_env mismatch?)"),
	} {
		if isConnError(err) {
			t.Fatalf("isConnError(%q) = true; want false", err)
		}
	}
}

// TestFutuAccountLiveGateway hits the real gateway when FUTU_LIVE_TEST=1
// (local verification with a running container; CI never sets it).
func TestFutuAccountLiveGateway(t *testing.T) {
	if os.Getenv("FUTU_LIVE_TEST") == "" {
		t.Skip("FUTU_LIVE_TEST not set (real Futu gateway verification)")
	}
	h := FutuAccountHandler(NewFutuAccounter())
	rec := get(t, h, "/v1/futu/account")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got AccountSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q not JSON: %v", rec.Body, err)
	}
	if got.Funds.TotalAssets == 0 && got.Funds.Cash == 0 {
		t.Fatalf("live funds empty: %+v", got.Funds)
	}
}

// TestFutuAccountAvailableCashFallback: 模拟盘网关实测 available_funds 恒为 0
// (2026-08-12 盘中),而 cash 是真实可用现金;AvailableCash 必须回退 cash-frozen,
// 否则 LLM 审核/下单上下文会把资金充裕账户误判为「现金不足」fail-closed REJECT。
func TestFutuAccountAvailableCashFallback(t *testing.T) {
	fastFutuLimits(t)
	addr := fakegw.Server(t, func(protoID int32, body []byte) []byte {
		switch protoID {
		case protoInit:
			return fakegw.InitBody(42)
		case protoFunds:
			return fakegw.FundsBody(0, 1907141, &trdcommon.Funds{
				Power:             proto.Float64(1188976.366),
				TotalAssets:       proto.Float64(1188976.366),
				Cash:              proto.Float64(272866.366),
				MarketVal:         proto.Float64(916110),
				FrozenCash:        proto.Float64(0),
				DebtCash:          proto.Float64(0),
				AvlWithdrawalCash: proto.Float64(0),
				AvailableFunds:    proto.Float64(0), // 模拟盘网关不填
			})
		case protoPos:
			return fakegw.PositionsBody(0, 1907141, nil)
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	})
	h := FutuAccountHandler(&futuAccounter{pc: newProtoClientAt(addr)})
	rec := get(t, h, "/v1/futu/account")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got AccountSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if want := 272866.366; got.Funds.AvailableCash != want {
		t.Fatalf("AvailableCash = %v; want fallback %v (cash - frozen)", got.Funds.AvailableCash, want)
	}
}
