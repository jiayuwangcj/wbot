package httpapi

// Futu paper-account proxy: GET /v1/futu/account serves funds + positions from
// the futu gateway (proto TCP 11111) for the browser, which cannot reach the
// loopback gateway directly. Read-only and paper-first: default env is
// simulate; real env queries are read-only safe (doc/FUTU.md 交易安全策略). The
// response is a field whitelist — no credentials or config values (PRIVACY).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jiayu/wbot/internal/futu"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

const futuAccountPath = "/v1/futu/account"

// FundsJSON is the whitelisted funds summary (购买力/资产总额/现金/市值/可用资金).
type FundsJSON struct {
	Power         float64 `json:"power"`
	TotalAssets   float64 `json:"total_assets"`
	Cash          float64 `json:"cash"`
	MarketVal     float64 `json:"market_val"`
	AvailableCash float64 `json:"available_cash"`
}

// PositionJSON is one whitelisted position row (no account/order metadata).
type PositionJSON struct {
	Symbol    string  `json:"symbol"`
	Qty       float64 `json:"qty"`
	AvgCost   float64 `json:"avg_cost"`
	Price     float64 `json:"price"`
	MarketVal float64 `json:"market_val"`
	PL        float64 `json:"pl"`
}

// AccountSnapshot is the GET /v1/futu/account success body.
type AccountSnapshot struct {
	Env       string         `json:"env"`
	AccID     uint64         `json:"acc_id"`
	Funds     FundsJSON      `json:"funds"`
	Positions []PositionJSON `json:"positions"`
}

// FutuAccounter is the funds/positions surface the endpoint needs; backed by
// internal/futu.TradeClient (proto TCP 11111, doc/FUTU.md §10).
type FutuAccounter interface {
	Account(ctx context.Context, env futu.Env, accID uint64) (AccountSnapshot, error)
}

// futuAccounter backs the account endpoint with the shared protoClient
// (connection + reconnect management lives there, see futu_client.go).
type futuAccounter struct {
	pc *protoClient
}

func (a *futuAccounter) Account(ctx context.Context, env futu.Env, accID uint64) (AccountSnapshot, error) {
	var snap AccountSnapshot
	err := a.pc.do(ctx, func(tc *futu.TradeClient) error {
		acc, err := tc.Account(ctx, env, accID)
		if err != nil {
			return err
		}
		funds, err := tc.Funds(ctx, acc)
		if err != nil {
			return err
		}
		positions, err := tc.Positions(ctx, acc)
		if err != nil {
			return err
		}
		snap = AccountSnapshot{
			Env:       futu.EnvName(env),
			AccID:     acc.GetAccID(),
			Positions: make([]PositionJSON, 0, len(positions)),
		}
		snap.Funds = FundsJSON{
			Power:         funds.GetPower(),
			TotalAssets:   funds.GetTotalAssets(),
			Cash:          funds.GetCash(),
			MarketVal:     funds.GetMarketVal(),
			AvailableCash: funds.GetAvailableFunds(), // 可用资金 (proto available_funds)
		}
		for _, p := range positions {
			snap.Positions = append(snap.Positions, PositionJSON{
				Symbol:    symbolFor(p),
				Qty:       p.GetQty(),
				AvgCost:   p.GetCostPrice(),
				Price:     p.GetPrice(),
				MarketVal: p.GetVal(),
				PL:        p.GetPlVal(),
			})
		}
		return nil
	})
	return snap, err
}

// qualifySymbol reconstructs a market-qualified symbol from the TrdSecMarket
// enum (1=HK, 2=US, 3=CN) and code; the CN exchange is inferred from the code
// (6xxxxx = Shanghai, else Shenzhen). Shared by positions and orders.
func qualifySymbol(market int32, code string) string {
	switch market {
	case 1:
		return "HK." + code
	case 2:
		return "US." + code
	case 3:
		if strings.HasPrefix(code, "6") {
			return "SH." + code
		}
		return "SZ." + code
	}
	return code
}

// symbolFor reconstructs a market-qualified symbol for a position row.
func symbolFor(p *trdcommon.Position) string {
	return qualifySymbol(p.GetSecMarket(), p.GetCode())
}

// NewFutuAccounter returns a FutuAccounter talking to the gateway at FutuAccountAddr().
func NewFutuAccounter() FutuAccounter {
	return &futuAccounter{pc: newProtoClient()}
}

// FutuAccountHandler serves GET /v1/futu/account: the browser cannot reach the
// gateway directly (loopback/CORS), so serve proxies funds + positions and
// returns the whitelisted snapshot.
func FutuAccountHandler(accounter FutuAccounter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(futuAccountPath, func(w http.ResponseWriter, r *http.Request) {
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
		snap, err := accounter.Account(r.Context(), env, accID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: futu account: %v\n", err)
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

// accountError maps an accounter error to the API status: gateway unreachable
// (dial/timeout) → 503; upstream/business errors → 502 with message passthrough.
func accountError(err error) (int, string) {
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusServiceUnavailable, "Futu gateway unreachable"
	}
	return http.StatusBadGateway, err.Error()
}

func accountAction(status int) string {
	if status == http.StatusServiceUnavailable {
		return "start the Futu gateway container (docker compose -f configs/docker-compose.futu.yml up -d) and retry"
	}
	return "check the account parameters and gateway state, then retry"
}
