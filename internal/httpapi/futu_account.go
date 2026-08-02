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
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

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

// futuAccounter shares one TradeClient across requests (single process, low
// query frequency; the gateway auto-reconnects). The mutex serializes queries:
// gofutuapi's client is not concurrency-safe.
type futuAccounter struct {
	mu   sync.Mutex
	addr string
	tc   *futu.TradeClient
}

func (a *futuAccounter) Account(ctx context.Context, env futu.Env, accID uint64) (AccountSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap, err := a.account(ctx, env, accID)
	if err != nil && a.tc != nil && isConnError(err) {
		// The gateway drops proto connections between requests (observed live
		// 2026-08-02 with opend-rs: "accounts: get acc list failed: connection
		// closed" on every call after the first until restart). Drop the stale
		// client, reconnect, and retry once — these queries are read-only.
		_ = a.tc.Close()
		a.tc = nil
		snap, err = a.account(ctx, env, accID)
	}
	return snap, err
}

// account runs the snapshot query on a.locked client (mutex held by Account).
func (a *futuAccounter) account(ctx context.Context, env futu.Env, accID uint64) (AccountSnapshot, error) {
	if a.tc == nil {
		tc, err := futu.OpenTrade(ctx, a.addr)
		if err != nil {
			return AccountSnapshot{}, err
		}
		a.tc = tc
	}
	acc, err := a.tc.Account(ctx, env, accID)
	if err != nil {
		return AccountSnapshot{}, err
	}
	funds, err := a.tc.Funds(ctx, acc)
	if err != nil {
		return AccountSnapshot{}, err
	}
	positions, err := a.tc.Positions(ctx, acc)
	if err != nil {
		return AccountSnapshot{}, err
	}
	snap := AccountSnapshot{
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
	return snap, nil
}

// symbolFor reconstructs a market-qualified symbol from the TrdMarket enum
// (1=HK, 2=US, 3=CN) and code; the CN exchange is inferred from the code
// (6xxxxx = Shanghai, else Shenzhen).
func symbolFor(p *trdcommon.Position) string {
	switch p.GetSecMarket() {
	case 1:
		return "HK." + p.GetCode()
	case 2:
		return "US." + p.GetCode()
	case 3:
		if strings.HasPrefix(p.GetCode(), "6") {
			return "SH." + p.GetCode()
		}
		return "SZ." + p.GetCode()
	}
	return p.GetCode()
}

// FutuAccountAddr returns the gateway proto address: $FUTU_PROTO_ADDR or the
// OpenD default loopback 11111 (doc/FUTU.md). The proto client dials TCP 11111;
// the REST quote/options proxies read $FUTU_GATEWAY_URL (REST 22222) instead —
// the two transports are configured independently, so a REST gateway URL must
// not be fed to the proto dialer.
func FutuAccountAddr() string {
	if v := strings.TrimSpace(os.Getenv("FUTU_PROTO_ADDR")); v != "" {
		return v
	}
	return futu.DefaultProtoAddr
}

// NewFutuAccounter returns a FutuAccounter talking to the gateway at FutuAccountAddr().
func NewFutuAccounter() FutuAccounter {
	return &futuAccounter{addr: FutuAccountAddr()}
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

// isConnError reports transport-level failures (dead socket, EOF, refused,
// reply timeout after the gateway dropped the conn); business errors (bad
// account, trd_env mismatch) never trigger a reconnect. gofutuapi surfaces a
// mid-stream drop as "connection closed" or, when its internal reconnect is
// stuck, "timeout waiting for reply SN N" (observed with opend-rs 2026-08-02).
func isConnError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection closed") || strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "timeout waiting for reply")
}

func accountAction(status int) string {
	if status == http.StatusServiceUnavailable {
		return "start the Futu gateway container (docker compose -f configs/docker-compose.futu.yml up -d) and retry"
	}
	return "check the account parameters and gateway state, then retry"
}
