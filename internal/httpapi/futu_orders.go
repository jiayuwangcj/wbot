package httpapi

// Futu paper-order proxy: GET /v1/futu/orders serves the account's order list
// from the futu gateway (proto TCP 11111) for the browser, which cannot reach
// the loopback gateway directly. Read-only and paper-first: default env is
// simulate; real env queries are read-only safe (doc/FUTU.md 交易安全策略). The
// response is a field whitelist — no credentials or config values (PRIVACY).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/qtopie/gofutuapi"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

const futuOrdersPath = "/v1/futu/orders"

// OrderJSON is one whitelisted order row (no account/order metadata).
type OrderJSON struct {
	OrderID      uint64  `json:"order_id"`
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Side         string  `json:"side"`   // buy / sell (TrdSide)
	Status       string  `json:"status"` // OrderStatus enum name
	Qty          float64 `json:"qty"`
	Price        float64 `json:"price"`
	FillQty      float64 `json:"fill_qty"`
	FillAvgPrice float64 `json:"fill_avg_price"`
	CreateTime   string  `json:"create_time"`
	UpdateTime   string  `json:"update_time"`
	LastErr      string  `json:"last_err,omitempty"`
}

// OrdersSnapshot is the GET /v1/futu/orders success body.
type OrdersSnapshot struct {
	Env    string      `json:"env"`
	AccID  uint64      `json:"acc_id"`
	Orders []OrderJSON `json:"orders"`
}

// FutuOrderer is the order surface the endpoint needs; backed by
// internal/futu.TradeClient (proto TCP 11111, doc/FUTU.md §10).
type FutuOrderer interface {
	Orders(ctx context.Context, env futu.Env, accID uint64, pendingOnly bool) (OrdersSnapshot, error)
}

// futuOrderer backs the orders endpoint with the shared protoClient
// (connection + reconnect management lives there, see futu_client.go).
type futuOrderer struct {
	pc *protoClient
}

func (o *futuOrderer) Orders(ctx context.Context, env futu.Env, accID uint64, pendingOnly bool) (OrdersSnapshot, error) {
	var snap OrdersSnapshot
	err := o.pc.do(ctx, func(tc *futu.TradeClient) error {
		acc, err := tc.Account(ctx, env, accID)
		if err != nil {
			return err
		}
		orders, err := tc.Orders(ctx, acc, pendingOnly)
		if err != nil {
			return err
		}
		snap = OrdersSnapshot{
			Env:    futu.EnvName(env),
			AccID:  acc.GetAccID(),
			Orders: make([]OrderJSON, 0, len(orders)),
		}
		for _, ord := range orders {
			snap.Orders = append(snap.Orders, orderJSON(ord))
		}
		return nil
	})
	return snap, err
}

// orderJSON maps a proto Order to the whitelist. Side is rendered from the
// TrdSide enum (1=buy, 2=sell); status from the OrderStatus enum name
// (goFutuApi's OrderStatusLabel).
func orderJSON(o *trdcommon.Order) OrderJSON {
	side := ""
	if name, ok := trdcommon.TrdSide_name[o.GetTrdSide()]; ok {
		side = strings.TrimPrefix(name, "TrdSide_")
	}
	return OrderJSON{
		OrderID:      o.GetOrderID(),
		Symbol:       qualifySymbol(o.GetSecMarket(), o.GetCode()),
		Name:         o.GetName(),
		Side:         side,
		Status:       gofutuapi.OrderStatusLabel(o.GetOrderStatus()),
		Qty:          o.GetQty(),
		Price:        o.GetPrice(),
		FillQty:      o.GetFillQty(),
		FillAvgPrice: o.GetFillAvgPrice(),
		CreateTime:   o.GetCreateTime(),
		UpdateTime:   o.GetUpdateTime(),
		LastErr:      o.GetLastErrMsg(),
	}
}

// NewFutuOrderer returns a FutuOrderer talking to the gateway at FutuAccountAddr().
func NewFutuOrderer() FutuOrderer {
	return &futuOrderer{pc: newProtoClient()}
}

// FutuOrdersHandler serves GET /v1/futu/orders: query params env (sim|real,
// default sim), acc_id (uint64, default first account of env) and pending
// (0|1, default 1 = 挂单). Errors map like the account endpoint: unreachable
// gateway → 503, upstream/business → 502.
func FutuOrdersHandler(orderer FutuOrderer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(futuOrdersPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		env := futu.EnvSim // 默认模拟盘（安全策略 doc/FUTU.md）
		if s := strings.TrimSpace(r.URL.Query().Get("env")); s != "" {
			switch strings.ToLower(s) {
			case "sim", "simulate", "paper":
			case "real":
				env = futu.EnvReal
			default:
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid env %q (want sim or real)", s))
				return
			}
		}
		var accID uint64
		if s := strings.TrimSpace(r.URL.Query().Get("acc_id")); s != "" {
			id, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid acc_id %q (want uint64)", s))
				return
			}
			accID = id
		}
		pendingOnly := true // 默认挂单（Dashboard 视角：当前订单状态）
		if s := strings.TrimSpace(r.URL.Query().Get("pending")); s != "" {
			v, err := strconv.ParseBool(s)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid pending %q (want 0 or 1)", s))
				return
			}
			pendingOnly = v
		}
		snap, err := orderer.Orders(r.Context(), env, accID, pendingOnly)
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: futu orders: %v\n", err)
			status, msg := accountError(err)
			writeErrorBody(w, status, errorJSON{
				Code:    codeForStatus(status),
				Message: msg,
				Action:  accountAction(status),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snap)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}
