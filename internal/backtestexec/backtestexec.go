// Package backtestexec shares the DB-backed backtest run path between the CLI
// (`wbot backtest -dsn`) and the API (POST /v1/backtests, draft 2026-08-02
// S4): one validation contract, one runner, one persisted params shape
// (doc/BACKTEST.md, doc/API.md). Same input, same output.
package backtestexec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/strategy"
)

// ErrNoBars reports a run whose symbol has no bars in the requested range.
var ErrNoBars = errors.New("backtest: no bars in range")

// ErrNoOptionData reports a run whose symbol has bars but no option_quotes rows.
var ErrNoOptionData = errors.New("backtest: no option quote rows in range")

// Options is one DB-backed run's inputs; zero values are not defaults — the
// caller must set Timeframe/Adjust/Cash/Fee/Limit explicitly (the CLI from
// flags, the API from its documented defaults).
type Options struct {
	Symbol    string
	Strategy  string
	Params    map[string]any
	Timeframe string
	Adjust    string
	From      time.Time
	To        time.Time
	Limit     int
	Cash      float64
	Fee       float64
}

// SaveParams returns the run params map persisted by `wbot backtest -save` and
// POST /v1/backtests (cash/fee/timeframe/adjust; same shape on both paths).
func SaveParams(o Options) map[string]any {
	return map[string]any{"cash": o.Cash, "fee": o.Fee, "timeframe": o.Timeframe, "adjust": o.Adjust}
}

// Outcome is one executed run: the Result plus the bar range it consumed
// (persisted by callers as start_ts/end_ts, mirroring `wbot backtest -save`).
type Outcome struct {
	Result  *backtest.Result
	StartTs time.Time
	EndTs   time.Time
}

// Build validates a strategy name + params against the CLI/API contract and
// returns the ready strategy; templ is nil for hold/buy-hold. It is the shared
// validation surface: unknown strategy, params on hold/buy-hold, unknown
// params, wrong types and out-of-range values all error.
func Build(name string, params map[string]any) (backtest.Strategy, *strategy.Template, error) {
	switch name {
	case "hold":
		if len(params) > 0 {
			return nil, nil, fmt.Errorf("strategy hold takes no params")
		}
		return backtest.HoldStrategy{}, nil, nil
	case "buy-hold":
		if len(params) > 0 {
			return nil, nil, fmt.Errorf("strategy buy-hold takes no params")
		}
		return &backtest.BuyHoldStrategy{}, nil, nil
	}
	s, err := strategy.Factory(name, params)
	if err != nil {
		return nil, nil, err
	}
	templ, _ := strategy.Lookup(name)
	return s, templ, nil
}

// Run loads bars (and option quotes when the strategy needs them) from the
// database and runs the strategy — the same path as `wbot backtest -dsn`
// (doc/BACKTEST.md). ErrNoBars/ErrNoOptionData report missing input data;
// ctx cancellation aborts the run (no result is returned on abort).
func Run(ctx context.Context, db *sql.DB, o Options) (*Outcome, error) {
	if strings.TrimSpace(o.Symbol) == "" || strings.TrimSpace(o.Strategy) == "" {
		return nil, errors.New("backtest: exec: symbol and strategy are required")
	}
	if db == nil {
		return nil, errors.New("backtest: exec: nil db")
	}
	s, templ, err := Build(o.Strategy, o.Params)
	if err != nil {
		return nil, err
	}
	bars, err := ingest.QueryBars(ctx, db, o.Symbol, o.Timeframe, o.Adjust, o.From, o.To, o.Limit)
	if err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoBars, o.Symbol)
	}
	var opts *backtest.OptionsData
	if templ != nil && templ.NeedsOptions {
		rows, err := ingest.QueryOptionQuotes(ctx, db, o.Symbol, o.Adjust, o.From, o.To, o.Limit)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNoOptionData, o.Symbol)
		}
		opts, err = backtest.OptionsDataFromQuotes(rows)
		if err != nil {
			return nil, err
		}
	}
	res, err := backtest.RunOptions(ctx, bars, o.Cash, o.Fee, s, opts)
	if err != nil {
		return nil, err
	}
	return &Outcome{Result: res, StartTs: bars[0].Ts, EndTs: bars[len(bars)-1].Ts}, nil
}
