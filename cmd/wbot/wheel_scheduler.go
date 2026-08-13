package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/llmreview"
	"github.com/jiayu/wbot/internal/watchlist"
	"github.com/jiayu/wbot/internal/wheelrun"
	"github.com/jiayu/wbot/internal/wheelstore"
	trdcommon "github.com/qtopie/gofutuapi/gen/trade/common"
)

// startWheelRunner runs the wheel live loop for serve until ctx is cancelled
// (pattern: startDataCheckScheduler). Per-symbol and per-pass failures are
// logged inside the runner; only assembly-level errors surface here.
func startWheelRunner(ctx context.Context, database *sql.DB, env futu.Env, interval time.Duration) {
	client := futu.NewClient(resolveFutuGateway(""))
	reviewer, model := llmReviewerFromEnv()
	if reviewer == nil {
		fmt.Fprintln(os.Stderr, "wheel: WARN LLM reviewer disabled; set LLM_BASE_URL, LLM_API_KEY and LLM_MODEL; ALERT signals cannot be pushed")
	}
	store := wheelstore.New(database)
	runner := wheelrun.NewRunner(wheelrun.Dependencies{
		Quoter:           futuQuoter{client: client},
		Positions:        futuPositions{addr: futuProtoAddr(), env: env},
		Funds:            futuPositions{addr: futuProtoAddr(), env: env}.Funds,
		Chain:            client,
		Store:            store,
		SnapshotRecorder: store,
		Watchlist:        watchlistStore{db: database},
		LLMReviewer:      reviewer,
		LLMModel:         model,
	})
	if err := runner.Run(ctx, interval); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "wheel: runner: %v\n", err)
	}
}

func llmReviewerFromEnv() (llmreview.Reviewer, string) {
	baseURL := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	apiKey := os.Getenv("LLM_API_KEY")
	model := strings.TrimSpace(os.Getenv("LLM_MODEL"))
	if baseURL == "" || strings.TrimSpace(apiKey) == "" || model == "" {
		return nil, ""
	}
	reviewer, err := llmreview.New(baseURL, apiKey, model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wheel: LLM reviewer disabled: %v\n", err)
		return nil, ""
	}
	return reviewer, model
}

// futuQuoter adapts the REST gateway to wheelrun.Quoter: the underlying's
// current price is parsed from the raw /api/quote s2c body, option quotes
// delegate to the slice-B batch adapter.
type futuQuoter struct {
	client *futu.Client
}

func (q futuQuoter) Quote(ctx context.Context, symbol string) (float64, error) {
	s2c, err := q.client.Quote(ctx, symbol)
	if err != nil {
		return 0, err
	}
	return priceFromQuotePage(s2c, symbol)
}

// QuoteRaw satisfies underlyingQuoter for the card's display-name lookup
// (老板指令 2026-08-13: 正股价格区多一份底层资产名字和编号)。
func (q futuQuoter) QuoteRaw(ctx context.Context, symbol string) (json.RawMessage, error) {
	return q.client.Quote(ctx, symbol)
}

func priceFromQuotePage(s2c json.RawMessage, symbol string) (float64, error) {
	var pg struct {
		BasicQotList []struct {
			CurPrice float64 `json:"cur_price"`
		} `json:"basic_qot_list"`
	}
	if err := json.Unmarshal(s2c, &pg); err != nil {
		return 0, fmt.Errorf("quote %s: bad s2c: %w", symbol, err)
	}
	if len(pg.BasicQotList) == 0 {
		return 0, fmt.Errorf("quote %s: empty response", symbol)
	}
	return pg.BasicQotList[0].CurPrice, nil
}

func (q futuQuoter) OptionQuotes(ctx context.Context, symbols []string) (map[string]futu.OptionQuoteEx, error) {
	return q.client.OptionQuotes(ctx, symbols)
}

// futuPositions adapts the proto TradeClient to wheelrun.TradePositions: it
// merges every account of env (stock account + option account + futures …)
// and maps every position row (acc is ignored; the runner always passes nil).
// The per-account merge matters since 2026-08-12's multi-account order
// routing: AccountForSymbol places option orders on the SimAccType=Option
// account, so reading only the first env account (the stock account) hides
// sold puts and the inventory gap never closes (2026-08-13: signal 500 filled
// HK.TCH260821P450000, yet subsequent passes still showed gap 300).
type futuPositions struct {
	addr string
	env  futu.Env
}

// envAccounts returns every gateway account of p.env.
func (p futuPositions) envAccounts(ctx context.Context, tc *futu.TradeClient) ([]*trdcommon.TrdAcc, error) {
	accounts, err := tc.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var out []*trdcommon.TrdAcc
	for _, a := range accounts {
		if a.GetTrdEnv() == int32(p.env) {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no %s account (trd_env=%d)", futu.EnvName(p.env), p.env)
	}
	return out, nil
}

func (p futuPositions) Positions(ctx context.Context, _ any) ([]wheelrun.Position, error) {
	tc, err := futu.AcquireTrade(ctx, p.addr)
	if err != nil {
		return nil, fmt.Errorf("wheel positions: %w", err)
	}
	defer tc.Close()
	accs, err := p.envAccounts(ctx, tc)
	if err != nil {
		return nil, err
	}
	out := make([]wheelrun.Position, 0, 16)
	for _, acc := range accs {
		positions, err := tc.Positions(ctx, acc)
		if err != nil {
			return nil, fmt.Errorf("wheel positions acc=%d: %w", acc.GetAccID(), err)
		}
		for _, pos := range positions {
			out = append(out, wheelrun.Position{
				Symbol: qualifySymbol(pos.GetSecMarket(), pos.GetCode()),
				Code:   pos.GetCode(),
				// GetQty is already signed by the gateway (short = negative);
				// wheelrun.Position wants a positive qty with Side carrying
				// the sign (2026-08-13: the sold 450P came back qty=-1 side=1
				// and PositionsInput rejects negative qtys).
				Qty:  math.Abs(pos.GetQty()),
				Side: int(pos.GetPositionSide()),
			})
		}
	}
	return out, nil
}

// Funds returns the summed available cash across every env account for the
// wheel put-assignment check (read-only; same per-account merge as
// Positions).
func (p futuPositions) Funds(ctx context.Context) (float64, error) {
	tc, err := futu.AcquireTrade(ctx, p.addr)
	if err != nil {
		return 0, fmt.Errorf("wheel funds: %w", err)
	}
	defer tc.Close()
	accs, err := p.envAccounts(ctx, tc)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, acc := range accs {
		funds, err := tc.Funds(ctx, acc)
		if err != nil {
			return 0, fmt.Errorf("wheel funds acc=%d: %w", acc.GetAccID(), err)
		}
		total += funds.GetCash()
	}
	return total, nil
}

// qualifySymbol reconstructs a market-qualified symbol from the TrdSecMarket
// enum (1=HK, 2=US, 3=CN) and code (CN exchange from the code prefix).
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

// watchlistStore adapts the watchlist package to the runner's narrow,
// DB-free interface.
type watchlistStore struct {
	db *sql.DB
}

func (w watchlistStore) List(ctx context.Context) ([]watchlist.Item, error) {
	return watchlist.List(ctx, w.db)
}

func (w watchlistStore) SetExecutionStatus(ctx context.Context, symbol, status, reason string) error {
	return watchlist.SetExecutionStatus(ctx, w.db, symbol, status, reason)
}

// futuProtoAddr returns the proto gateway address: $FUTU_PROTO_ADDR or the
// OpenD loopback default (the REST gateway env is resolved by
// resolveFutuGateway).
func futuProtoAddr() string {
	if v := strings.TrimSpace(os.Getenv("FUTU_PROTO_ADDR")); v != "" {
		return v
	}
	return futu.DefaultProtoAddr
}

// parseWheelEnv maps the --wheel-env flag to the futu trading environment.
func parseWheelEnv(s string) (futu.Env, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "sim", "simulate":
		return futu.EnvSim, nil
	case "real":
		return futu.EnvReal, nil
	}
	return 0, fmt.Errorf("serve: unknown -wheel-env %q (want sim or real)", s)
}

func validateWheelInterval(interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("invalid -wheel-interval %q (must be > 0)", interval.String())
	}
	return nil
}
