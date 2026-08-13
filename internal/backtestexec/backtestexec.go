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
	"github.com/jiayu/wbot/internal/wheelstore"
)

// ErrNoBars reports a run whose symbol has no bars in the requested range.
var ErrNoBars = errors.New("backtest: no bars in range")

// ErrNoOptionData reports a Wheel run whose symbol has bars but no trusted
// option quote snapshot rows. Legacy option_quotes rows are intentionally not
// a fallback because they cannot supply executable Greeks or market sides.
var ErrNoOptionData = errors.New("backtest: no option quote snapshots in range")

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

// SaveParams returns the run inputs persisted by `wbot backtest -save` and
// POST /v1/backtests. Wheel's complete structured configuration is retained
// under strategy_params so a saved run can be reproduced and audited.
func SaveParams(o Options) map[string]any {
	out := map[string]any{"cash": o.Cash, "fee": o.Fee, "timeframe": o.Timeframe, "adjust": o.Adjust}
	if len(o.Params) > 0 {
		params := o.Params
		if o.Strategy == "wheel" {
			if canonical, err := strategy.CanonicalParams(o.Params); err == nil {
				params = canonical
			}
		}
		out["strategy_params"] = params
	}
	return out
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
	bars, err := ingest.QueryBars(ctx, db, o.Symbol, o.Timeframe, o.Adjust, o.From, o.To, o.Limit, false)
	if err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoBars, o.Symbol)
	}
	var opts *backtest.OptionsData
	if templ != nil && templ.NeedsOptions {
		// Filter by underlying in SQL before LIMIT. This is important when the
		// database contains several underlyings: truncating a global stream
		// first can otherwise manufacture an ErrNoOptionData.
		quoteFrom := quoteRangeStart(o.From, s)
		rows, err := wheelstore.New(db).QueryUnderlyingQuoteSnapshots(ctx, o.Symbol, quoteFrom, o.To, o.Limit)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNoOptionData, o.Symbol)
		}
		opts, err = backtest.OptionsDataFromQuoteSnapshots(rows)
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

func quoteRangeStart(from time.Time, s backtest.Strategy) time.Time {
	if wheelStrategy, ok := s.(*strategy.WheelStrategy); ok && !from.IsZero() {
		return from.Add(-wheelStrategy.Config.QuoteMaxAge())
	}
	return from
}

// MultiOutcome is one multi-symbol run: the combined result plus the aligned
// bar range it consumed (start/end of the common timeline).
type MultiOutcome struct {
	Result  *backtest.MultiResult
	StartTs time.Time
	EndTs   time.Time
}

// RunMulti runs one independent sub-account per symbol over the intersection
// of their bars (equal cash split) — the CLI's `wbot backtest -symbols` path
// (doc/BACKTEST.md). Option strategies are rejected: per-symbol OptionsData is
// not part of the minimal multi-symbol semantic. Each sub-account gets a fresh
// strategy instance (stateful strategies are not reusable between runs).
func RunMulti(ctx context.Context, db *sql.DB, o Options, symbols []string) (*MultiOutcome, error) {
	if len(symbols) == 0 {
		return nil, errors.New("backtest: exec: multi: empty symbols")
	}
	if strings.TrimSpace(o.Strategy) == "" {
		return nil, errors.New("backtest: exec: multi: strategy is required")
	}
	// Strategy/params validation first, so bad input errors without touching
	// the database (mirrors Run's Build contract).
	if _, templ, err := Build(o.Strategy, o.Params); err != nil {
		return nil, err
	} else if templ != nil && templ.NeedsOptions {
		return nil, fmt.Errorf("backtest: exec: multi: strategy %s needs option_quotes; multi-symbol runs support hold/buy-hold", o.Strategy)
	}
	seen := make(map[string]bool, len(symbols))
	for _, sym := range symbols {
		if strings.TrimSpace(sym) == "" {
			return nil, errors.New("backtest: exec: multi: empty symbol")
		}
		if seen[sym] {
			return nil, fmt.Errorf("backtest: exec: multi: duplicate symbol %s", sym)
		}
		seen[sym] = true
	}
	if db == nil {
		return nil, errors.New("backtest: exec: multi: nil db")
	}
	series := make([]backtest.SymbolBars, 0, len(symbols))
	for _, sym := range symbols {
		bars, err := ingest.QueryBars(ctx, db, sym, o.Timeframe, o.Adjust, o.From, o.To, o.Limit, false)
		if err != nil {
			return nil, err
		}
		if len(bars) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNoBars, sym)
		}
		series = append(series, backtest.SymbolBars{Symbol: sym, Bars: bars})
	}
	res, err := backtest.RunMulti(ctx, series, o.Cash, o.Fee, func() (backtest.Strategy, error) {
		s, _, err := Build(o.Strategy, o.Params)
		return s, err
	})
	if err != nil {
		return nil, err
	}
	return &MultiOutcome{Result: res, StartTs: res.EquityCurve[0].Ts, EndTs: res.EquityCurve[len(res.EquityCurve)-1].Ts}, nil
}
