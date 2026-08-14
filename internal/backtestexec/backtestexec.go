// Package backtestexec shares the DB-backed backtest run path between the CLI
// (`wbot backtest -dsn`) and the API (POST /v1/backtests, draft 2026-08-02
// S4): one validation contract, one runner, one persisted params shape
// (doc/BACKTEST.md, doc/API.md). Same input, same output.
package backtestexec

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
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

// ErrNoOptionData is retained for callers mapping older executor failures.
// Run now returns a successful DATA_BLOCKED/HOLD result when bars exist but
// snapshot rows do not, so a report can expose zero coverage without inventing
// Greeks or market sides.
var ErrNoOptionData = errors.New("backtest: no option quote snapshots in range")

// Options is one DB-backed run's inputs; zero values are not defaults — the
// caller must set Timeframe/Adjust/Cash/Fee/Limit explicitly (the CLI from
// flags, the API from its documented defaults).
type Options struct {
	Symbol   string
	Strategy string
	Params   map[string]any
	// ConfigVersion is non-nil only when Params came from a versioned
	// production binding (for example CLI -from-watchlist).
	ConfigVersion *int
	Timeframe     string
	Adjust        string
	From          time.Time
	To            time.Time
	Limit         int
	Cash          float64
	Fee           float64
	// FeeModel selects the type-specific fee schedule. nil preserves the
	// historical fixed Fee behavior, which is required for API and saved-run
	// compatibility.
	FeeModel *backtest.FeeModel
	// Seed seeds the unfilled-attempt draws (0 = backtest default 42).
	Seed int64
	// QuoteFrom overrides the option snapshot query start when non-zero. ES
	// training windows use it to widen history when the search space tunes
	// min_iv_rank, which needs a 1-year IV window (backtest.IVRankWindow).
	QuoteFrom time.Time
}

// SaveParams returns the run inputs persisted by `wbot backtest -save` and
// POST /v1/backtests. Wheel's complete structured configuration is retained
// under strategy_params so a saved run can be reproduced and audited.
func SaveParams(o Options) map[string]any {
	out := map[string]any{"cash": o.Cash, "fee": o.Fee, "timeframe": o.Timeframe, "adjust": o.Adjust}
	if o.FeeModel != nil {
		out["fee_option_per_contract"] = o.FeeModel.OptionPerContract
		out["fee_stock_per_lot"] = o.FeeModel.StockPerLot
		out["lot_size"] = o.FeeModel.LotSize
	}
	if len(o.Params) > 0 {
		params := o.Params
		if o.Strategy == "wheel" {
			if canonical, err := strategy.CanonicalParams(o.Params); err == nil {
				params = canonical
			}
		}
		out["strategy_params"] = params
	}
	if o.ConfigVersion != nil {
		out["config_version"] = *o.ConfigVersion
	}
	return out
}

// Outcome is one executed run: the Result plus the bar range it consumed
// (persisted by callers as start_ts/end_ts, mirroring `wbot backtest -save`).
type Outcome struct {
	Result            *backtest.Result
	StartTs           time.Time
	EndTs             time.Time
	BaselineReturnPct float64
	SourceHash        string
}

// Prepared contains the immutable market input for one backtest window.
// Strategies and run seeds are deliberately not part of the prepared value:
// ES evaluates many parameter/seed combinations over the same window, while
// the bars and quote snapshots are identical for all of them.
type Prepared struct {
	bars       []ingest.Bar
	options    *backtest.OptionsData
	sourceHash string
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

// Prepare loads bars (and option quotes when the strategy needs them) from the
// database once for one window. The returned value can be reused for many
// parameter/seed evaluations without repeating the database queries or
// OptionsData conversion.
func Prepare(ctx context.Context, db *sql.DB, o Options) (*Prepared, error) {
	if err := validateInputs(o, db); err != nil {
		return nil, err
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
		if !o.QuoteFrom.IsZero() {
			quoteFrom = o.QuoteFrom
		}
		rows, err := wheelstore.New(db).QueryUnderlyingQuoteSnapshots(ctx, o.Symbol, quoteFrom, o.To, o.Limit)
		if err != nil {
			return nil, err
		}
		opts, err = optionsDataForRun(rows, o.Seed)
		if err != nil {
			return nil, err
		}
	}
	inputHash, err := sourceHash(bars, opts)
	if err != nil {
		return nil, err
	}
	return &Prepared{bars: bars, options: opts, sourceHash: inputHash}, nil
}

// RunPrepared executes one parameter/seed evaluation over data loaded by
// Prepare. It intentionally creates a fresh strategy and backtest state for
// every call; only immutable input data is shared.
func (p *Prepared) RunPrepared(ctx context.Context, o Options) (*Outcome, error) {
	if p == nil || len(p.bars) == 0 {
		return nil, errors.New("backtest: exec: nil prepared data")
	}
	if strings.TrimSpace(o.Symbol) == "" || strings.TrimSpace(o.Strategy) == "" {
		return nil, errors.New("backtest: exec: symbol and strategy are required")
	}
	s, _, err := Build(o.Strategy, o.Params)
	if err != nil {
		return nil, err
	}
	opts := p.options
	if opts != nil {
		// RunSeed is per evaluation. Copy the small wrapper instead of mutating
		// the shared prepared value, so a future parallel evaluator cannot make
		// one candidate's unfilled draws affect another candidate.
		copy := *opts
		copy.RunSeed = o.Seed
		opts = &copy
	}
	res, err := RunBars(ctx, p.bars, o, s, opts)
	if err != nil {
		return nil, err
	}
	return &Outcome{
		Result:            res,
		StartTs:           p.bars[0].Ts,
		EndTs:             p.bars[len(p.bars)-1].Ts,
		BaselineReturnPct: p.bars[len(p.bars)-1].Close/p.bars[0].Close - 1,
		SourceHash:        p.sourceHash,
	}, nil
}

// RunBars executes already-loaded bars with the same fee selection used by
// the DB-backed runner. File-backed CLI runs use this helper too, keeping the
// fee path deterministic across CLI, batch, ES and API callers.
func RunBars(ctx context.Context, bars []ingest.Bar, o Options, s backtest.Strategy, opts *backtest.OptionsData) (*backtest.Result, error) {
	if o.FeeModel != nil {
		return backtest.RunOptionsWithFeeModel(ctx, bars, o.Cash, *o.FeeModel, s, opts)
	}
	return backtest.RunOptions(ctx, bars, o.Cash, o.Fee, s, opts)
}

// Run preserves the public DB-backed execution contract for API and CLI
// callers that only need one evaluation.
func Run(ctx context.Context, db *sql.DB, o Options) (*Outcome, error) {
	p, err := Prepare(ctx, db, o)
	if err != nil {
		return nil, err
	}
	return p.RunPrepared(ctx, o)
}

func validateInputs(o Options, db *sql.DB) error {
	if strings.TrimSpace(o.Symbol) == "" || strings.TrimSpace(o.Strategy) == "" {
		return errors.New("backtest: exec: symbol and strategy are required")
	}
	if db == nil {
		return errors.New("backtest: exec: nil db")
	}
	return nil
}

func optionsDataForRun(rows []wheelstore.QuoteSnapshotRecord, seed int64) (*backtest.OptionsData, error) {
	if len(rows) == 0 {
		return &backtest.OptionsData{RunSeed: seed}, nil
	}
	opts, err := backtest.OptionsDataFromQuoteSnapshots(rows)
	if err != nil {
		return nil, err
	}
	opts.RunSeed = seed
	return opts, nil
}

// sourceHash fingerprints the semantic market inputs consumed by Run. It
// deliberately excludes database row IDs and ingestion timestamps.
func sourceHash(bars []ingest.Bar, opts *backtest.OptionsData) (string, error) {
	snapshot := struct {
		Bars         []ingest.Bar                  `json:"bars"`
		QuoteBatches []backtest.QuoteSnapshotBatch `json:"quote_batches,omitempty"`
	}{Bars: bars}
	if opts != nil {
		snapshot.QuoteBatches = opts.QuoteBatches
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("backtest: hash source snapshot: %w", err)
	}
	digest := sha256.Sum256(b)
	return fmt.Sprintf("sha256-%x", digest), nil
}

func quoteRangeStart(from time.Time, s backtest.Strategy) time.Time {
	if wheelStrategy, ok := s.(*strategy.WheelStrategy); ok && !from.IsZero() {
		lookback := wheelStrategy.Config.QuoteMaxAge()
		// min_iv_rank needs a 1-year trailing IV window (ivrank.go); without it
		// every batch carries an unknown rank and the gate masks all candidates.
		if wheelStrategy.Config.MinIVRank > 0 {
			lookback = max(lookback, backtest.IVRankWindow)
		}
		return from.Add(-lookback)
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
	var runErr error
	var res *backtest.MultiResult
	factory := func() (backtest.Strategy, error) {
		s, _, err := Build(o.Strategy, o.Params)
		return s, err
	}
	if o.FeeModel != nil {
		res, runErr = backtest.RunMultiWithFeeModel(ctx, series, o.Cash, *o.FeeModel, factory)
	} else {
		res, runErr = backtest.RunMulti(ctx, series, o.Cash, o.Fee, factory)
	}
	if runErr != nil {
		return nil, runErr
	}
	return &MultiOutcome{Result: res, StartTs: res.EquityCurve[0].Ts, EndTs: res.EquityCurve[len(res.EquityCurve)-1].Ts}, nil
}
