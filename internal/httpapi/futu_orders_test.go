package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/futu/fakegw"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
	"google.golang.org/protobuf/proto"
)

const protoOrders = 2201 // TRD_GETORDERLIST

// fakeFutuOrderer is a scriptable FutuOrderer for endpoint-level tests.
type fakeFutuOrderer struct {
	err     error
	snap    OrdersSnapshot
	gotEnv  futu.Env
	gotID   uint64
	gotPend bool
}

func (f *fakeFutuOrderer) Orders(_ context.Context, env futu.Env, accID uint64, pendingOnly bool) (OrdersSnapshot, error) {
	f.gotEnv, f.gotID, f.gotPend = env, accID, pendingOnly
	if f.err != nil {
		return OrdersSnapshot{}, f.err
	}
	if f.snap.Env != "" {
		return f.snap, nil
	}
	return OrdersSnapshot{
		Env:   "simulate",
		AccID: 1907141,
		Orders: []OrderJSON{
			{OrderID: 88, Symbol: "HK.00700", Name: "TENCENT", Side: "Sell", Status: "OrderStatus_Submitted",
				Qty: 100, Price: 480.0, CreateTime: "2026-08-02 10:00:00", UpdateTime: "2026-08-02 10:00:01"},
		},
	}, nil
}

func TestFutuOrdersPassthrough(t *testing.T) {
	f := &fakeFutuOrderer{}
	rec := get(t, FutuOrdersHandler(f), "/v1/futu/orders")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if !f.gotPend {
		t.Fatalf("pending = %v; want default pending-only (挂单)", f.gotPend)
	}
	if f.gotEnv != futu.EnvSim || f.gotID != 0 {
		t.Fatalf("env/accID = %v/%d; want default sim account", f.gotEnv, f.gotID)
	}
	var got OrdersSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if got.Env != "simulate" || got.AccID != 1907141 || len(got.Orders) != 1 {
		t.Fatalf("snapshot = %+v; want simulate/1907141 with 1 order", got)
	}
	o := got.Orders[0]
	if o.OrderID != 88 || o.Symbol != "HK.00700" || o.Side != "Sell" || o.Status != "OrderStatus_Submitted" ||
		o.Qty != 100 || o.Price != 480.0 || o.CreateTime != "2026-08-02 10:00:00" {
		t.Fatalf("order = %+v; want the whitelisted row", o)
	}
}

func TestFutuOrdersParams(t *testing.T) {
	f := &fakeFutuOrderer{}
	rec := get(t, FutuOrdersHandler(f), "/v1/futu/orders?env=real&acc_id=281756478875559548&pending=0")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	if f.gotEnv != futu.EnvReal || f.gotID != 281756478875559548 || f.gotPend {
		t.Fatalf("env/accID/pending = %v/%d/%v; want real/full list (read-only allowed)", f.gotEnv, f.gotID, f.gotPend)
	}
}

func TestFutuOrdersBadParams(t *testing.T) {
	h := FutuOrdersHandler(&fakeFutuOrderer{})
	for _, path := range []string{
		"/v1/futu/orders?env=production",
		"/v1/futu/orders?acc_id=abc",
		"/v1/futu/orders?pending=maybe",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d; want 400 (body %s)", path, rec.Code, rec.Body)
		}
	}
}

func TestFutuOrdersGatewayUnreachable(t *testing.T) {
	h := FutuOrdersHandler(&fakeFutuOrderer{err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}})
	rec := get(t, h, "/v1/futu/orders")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body %s)", rec.Code, rec.Body)
	}
}

// TestFutuOrdersProtoGateway drives the real orderer against the fake
// OpenD-protocol gateway (proto order-list round-trip, no live gateway).
func TestFutuOrdersProtoGateway(t *testing.T) {
	fastFutuLimits(t)
	addr := fakegw.Server(t, func(protoID int32, body []byte) []byte {
		switch protoID {
		case protoInit:
			return fakegw.InitBody(42)
		case protoOrders:
			return fakegw.OrdersBody(0, 1907141, []*trdcommon.Order{
				{TrdSide: proto.Int32(2), OrderType: proto.Int32(0), OrderStatus: proto.Int32(6),
					OrderID: proto.Uint64(88), OrderIDEx: proto.String("x88"), Code: proto.String("00700"),
					Name: proto.String("TENCENT"), Qty: proto.Float64(100), Price: proto.Float64(480.0),
					CreateTime: proto.String("2026-08-02 10:00:00"), UpdateTime: proto.String("2026-08-02 10:00:01"),
					SecMarket: proto.Int32(1)},
			})
		}
		return fakegw.AccountsBody([]*trdcommon.TrdAcc{fakegw.Acc(0, 1907141, 1)})
	})

	h := FutuOrdersHandler(&futuOrderer{pc: newProtoClientAt(addr)})
	rec := get(t, h, "/v1/futu/orders")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body %s)", rec.Code, rec.Body)
	}
	var got OrdersSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, rec.Body)
	}
	if len(got.Orders) != 1 {
		t.Fatalf("orders = %d rows; want 1 (body %s)", len(got.Orders), rec.Body)
	}
	o := got.Orders[0]
	if o.Symbol != "HK.00700" || o.Side != "Sell" || o.OrderID != 88 || o.Qty != 100 || o.Price != 480.0 {
		t.Fatalf("order = %+v; want proto passthrough (side Sell, HK.00700)", o)
	}
}
