package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jiayu/wbot/internal/futu/fakegw"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
	"google.golang.org/protobuf/proto"
)

// captureRun captures stdout, stderr and the exit code of run().
func captureRun(t *testing.T, argv []string) (string, string, int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	or, ow, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = ow, ew
	code := run(argv)
	ow.Close()
	ew.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	stdout, err := io.ReadAll(or)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(er)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdout), string(stderr), code
}

func TestFutuDispatch(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"no sub", []string{"wbot", "futu"}, 2},
		{"help", []string{"wbot", "futu", "-h"}, 0},
		{"bad sub", []string{"wbot", "futu", "nope"}, 2},
		{"status help", []string{"wbot", "futu", "status", "-h"}, 0},
		{"quote help", []string{"wbot", "futu", "quote", "-h"}, 0},
		{"quote no symbol", []string{"wbot", "futu", "quote"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

func TestFutuStatusSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			io.WriteString(w, "ok")
		case "/api/global-state":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"qot_logined":true,"trd_logined":true,"server_ver":1002,"time":1785554255}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "status", "-addr", srv.URL})
	if code != 0 {
		t.Fatalf("run() = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{`"health": "ok"`, `"server_ver": 1002`, `"qot_logined": true`, `"trd_logined": true`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %s: %s", want, stdout)
		}
	}
}

func TestFutuStatusGatewayDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "status", "-addr", addr})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "futu: status:") {
		t.Fatalf("stderr = %q; want futu: status: prefix", stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q; want empty on failure", stdout)
	}
}

func TestFutuStatusHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":"backend disconnected"}`)
	}))
	defer srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "futu", "status", "-addr", srv.URL})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "503") || !strings.Contains(stderr, "backend disconnected") {
		t.Fatalf("stderr = %q; want 503 backend disconnected", stderr)
	}
}

func TestFutuQuoteSuccess(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscribe":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{}}`)
		case "/api/quote":
			io.WriteString(w, `{"ret_type":0,"ret_msg":null,"err_code":null,"s2c":{"basic_qot_list":[{"cur_price":475.2,"high_price":479.8,"low_price":462.0,"open_price":470.0,"volume":31100240,"security":{"market":1,"code":"00700"},"name":"TENCENT","update_time":"2026-07-31 16:07:51"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "HK.00700", "-addr", srv.URL})
	if code != 0 {
		t.Fatalf("run() = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{`"name": "TENCENT"`, `"cur_price": 475.2`, `"security"`, `"code": "00700"`, `"volume": 31100240`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("quote output missing %s: %s", want, stdout)
		}
	}
}

func TestFutuQuoteUnreachable(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "HK.00700", "-addr", addr})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "futu: quote:") {
		t.Fatalf("stderr = %q; want futu: quote: prefix", stderr)
	}
}

func TestFutuQuoteUnauthorized(t *testing.T) {
	fastFutuLimits(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"missing bearer token"}`)
	}))
	defer srv.Close()

	_, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "HK.00700", "-addr", srv.URL})
	if code != 1 {
		t.Fatalf("run() = %d; want 1", code)
	}
	if !strings.Contains(stderr, "401") || !strings.Contains(stderr, "missing bearer token") {
		t.Fatalf("stderr = %q; want 401 missing bearer token", stderr)
	}
}

func TestFutuQuoteBadSymbol(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "futu", "quote", "-symbol", "00700"})
	if code != 2 {
		t.Fatalf("run() = %d; want 2", code)
	}
	if !strings.Contains(stderr, "MARKET.CODE") {
		t.Fatalf("stderr = %q; want MARKET.CODE hint", stderr)
	}
}

func TestFutuUsageMentionsREST(t *testing.T) {
	_, stderr, code := captureRun(t, []string{"wbot", "futu", "-h"})
	if code != 0 {
		t.Fatalf("run() = %d; want 0", code)
	}
	for _, want := range []string{"status", "quote"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("futu usage missing %s: %s", want, stderr)
		}
	}
}

// fakeTradeGw starts a fake protobuf gateway serving init + accounts + canned
// funds/positions/order responses, returning its loopback address.
func fakeTradeGw(t *testing.T, accounts []*trdcommon.TrdAcc) string {
	t.Helper()
	return fakegw.Server(t, func(protoID int32, body []byte) []byte {
		switch protoID {
		case 1001: // INIT_CONNECT
			return fakegw.InitBody(42)
		case 2001: // TRD_GETACCLIST
			return fakegw.AccountsBody(accounts)
		case 2101: // TRD_GETFUNDS
			return fakegw.FundsBody(0, 1907141, &trdcommon.Funds{
				Power: proto.Float64(0), TotalAssets: proto.Float64(1198286.822),
				Cash: proto.Float64(318666.822), MarketVal: proto.Float64(879620),
				FrozenCash: proto.Float64(0), DebtCash: proto.Float64(0), AvlWithdrawalCash: proto.Float64(0),
			})
		case 2102: // TRD_GETPOSITIONLIST
			return fakegw.PositionsBody(0, 1907141, []*trdcommon.Position{{
				PositionID: proto.Uint64(1), PositionSide: proto.Int32(0),
				Code: proto.String("00700"), Name: proto.String("TENCENT"),
				Qty: proto.Float64(100), CanSellQty: proto.Float64(100),
				Price: proto.Float64(475.2), Val: proto.Float64(47520),
				PlVal: proto.Float64(0),
			}})
		case 2202: // TRD_PLACEORDER
			return fakegw.PlaceOrderBody(0, 1907141, "EX123", 777)
		}
		return nil
	})
}

func simAccounts() []*trdcommon.TrdAcc {
	return []*trdcommon.TrdAcc{
		fakegw.Acc(1, 281756478875559548, 1),
		fakegw.Acc(0, 1907141, 1),
	}
}

func TestFutuTradeDispatch(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want int
	}{
		{"funds help", []string{"wbot", "futu", "funds", "-h"}, 0},
		{"position help", []string{"wbot", "futu", "position", "-h"}, 0},
		{"order help", []string{"wbot", "futu", "order", "-h"}, 0},
		{"funds bad env", []string{"wbot", "futu", "funds", "-env", "bogus"}, 2},
		{"order no symbol", []string{"wbot", "futu", "order", "-side", "buy", "-qty", "100"}, 2},
		{"order bad side", []string{"wbot", "futu", "order", "-symbol", "HK.00700", "-side", "hold", "-qty", "100"}, 2},
		{"order bad qty", []string{"wbot", "futu", "order", "-symbol", "HK.00700", "-side", "buy", "-qty", "0"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.argv); got != tt.want {
				t.Fatalf("run(%v) = %d; want %d", tt.argv, got, tt.want)
			}
		})
	}
}

// TestFutuOrderSafetyGuard: real-env writes are refused without -live-confirm
// and without an explicit account; both guards never touch the gateway.
func TestFutuOrderSafetyGuard(t *testing.T) {
	base := []string{"wbot", "futu", "order", "-symbol", "HK.00700", "-side", "buy", "-qty", "100"}

	_, stderr, code := captureRun(t, append(base, "-env", "real"))
	if code != 2 {
		t.Fatalf("real without -live-confirm: code = %d; want 2", code)
	}
	if !strings.Contains(stderr, "live-confirm") || !strings.Contains(stderr, "拒绝") {
		t.Fatalf("real without -live-confirm: want refusal mentioning -live-confirm, got: %s", stderr)
	}

	_, stderr, code = captureRun(t, append(base, "-env", "real", "-live-confirm"))
	if code != 2 {
		t.Fatalf("real with -live-confirm but no -acc-id: code = %d; want 2", code)
	}
	if !strings.Contains(stderr, "-acc-id") {
		t.Fatalf("want refusal mentioning -acc-id, got: %s", stderr)
	}

	stdout, stderr, code := captureRun(t, append(base, "-env", "real", "-live-confirm", "-acc-id", "281756478875559548", "-dry-run"))
	if code != 0 {
		t.Fatalf("real with full confirmation + dry-run: code = %d; want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "LIVE CONFIRMED") {
		t.Fatalf("want live-confirm warning on stderr, got: %s", stderr)
	}
	if !strings.Contains(stdout, `"dry_run": true`) {
		t.Fatalf("want dry-run plan on stdout, got: %s", stdout)
	}
}

func TestFutuOrderDryRunSim(t *testing.T) {
	stdout, stderr, code := captureRun(t, []string{
		"wbot", "futu", "order", "-symbol", "HK.00700", "-side", "buy",
		"-qty", "100", "-price", "470", "-dry-run",
	})
	if code != 0 {
		t.Fatalf("sim dry-run: code = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{`"env": "simulate"`, `"symbol": "HK.00700"`, `"qty": 100`, `"price": 470`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("sim dry-run output missing %s: %s", want, stdout)
		}
	}
}

func TestFutuFundsSim(t *testing.T) {
	addr := fakeTradeGw(t, simAccounts())
	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "funds", "-addr", addr})
	if code != 0 {
		t.Fatalf("funds sim: code = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{`"acc_id": 1907141`, `"env": "simulate"`, `"total_assets": 1198286.822`, `"cash": 318666.822`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("funds output missing %s: %s", want, stdout)
		}
	}
}

func TestFutuFundsRealReadOnly(t *testing.T) {
	addr := fakeTradeGw(t, simAccounts())
	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "funds", "-env", "real", "-addr", addr})
	if code != 0 {
		t.Fatalf("funds real (read-only): code = %d; want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `"acc_id": 281756478875559548`) {
		t.Fatalf("funds real: want real acc_id, got: %s", stdout)
	}
}

func TestFutuFundsAccIDMismatch(t *testing.T) {
	addr := fakeTradeGw(t, simAccounts())
	_, stderr, code := captureRun(t, []string{
		"wbot", "futu", "funds", "-env", "sim", "-acc-id", "281756478875559548", "-addr", addr,
	})
	if code != 1 {
		t.Fatalf("funds env mismatch: code = %d; want 1", code)
	}
	if !strings.Contains(stderr, "not found in simulate") {
		t.Fatalf("want trd_env mismatch error, got: %s", stderr)
	}
}

func TestFutuFundsGatewayDown(t *testing.T) {
	// A closed listener port makes the protobuf connect fail fast.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, stderr, code := captureRun(t, []string{"wbot", "futu", "funds", "-addr", addr})
	if code != 1 {
		t.Fatalf("funds gateway down: code = %d; want 1", code)
	}
	if !strings.Contains(stderr, "futu: funds:") {
		t.Fatalf("want futu-prefixed error, got: %s", stderr)
	}
}

func TestFutuPositionSim(t *testing.T) {
	addr := fakeTradeGw(t, simAccounts())
	stdout, stderr, code := captureRun(t, []string{"wbot", "futu", "position", "-addr", addr})
	if code != 0 {
		t.Fatalf("position sim: code = %d; want 0 (stderr: %s)", code, stderr)
	}
	for _, want := range []string{`"code": "00700"`, `"name": "TENCENT"`, `"qty": 100`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("position output missing %s: %s", want, stdout)
		}
	}
}
