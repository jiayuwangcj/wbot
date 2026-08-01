package backtest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// SymbolBars is one symbol's bars (ts ascending) for a multi-symbol run.
type SymbolBars struct {
	Symbol string
	Bars   []ingest.Bar
}

// SymbolResult is one multi-symbol sub-account's standalone run result.
type SymbolResult struct {
	Symbol string
	Result *Result
}

// MultiResult summarizes a multi-symbol run: per-symbol sub-accounts plus the
// combined portfolio (equity curve is the pointwise sum over aligned bars).
type MultiResult struct {
	PerSymbol   []SymbolResult
	Equity      float64
	TotalReturn float64
	MaxDrawdown float64
	Bars        int
	EquityCurve []EquityPoint
}

// StrategyFactory returns a fresh strategy per multi-symbol sub-account;
// stateful strategies (buy-hold, option templates) are not reusable between runs.
type StrategyFactory func() (Strategy, error)

// RunMulti runs one independent sub-account per symbol over the intersection
// of their bars: only ts present in every symbol's series form the shared
// timeline (bars outside the common window do not participate), the initial
// cash is split equally (cash/N), and each account replays its aligned bars
// via RunOptions with its own strategy instance. The combined equity curve is
// the pointwise sum over the aligned timeline. details: doc/BACKTEST.md
func RunMulti(ctx context.Context, series []SymbolBars, initialCash float64, feePerTrade float64, factory StrategyFactory) (*MultiResult, error) {
	if len(series) == 0 {
		return nil, errors.New("backtest: multi: empty symbol series")
	}
	if initialCash <= 0 {
		return nil, errors.New("backtest: multi: initial cash must be > 0")
	}
	if feePerTrade < 0 {
		return nil, errors.New("backtest: multi: negative fee")
	}
	if factory == nil {
		return nil, errors.New("backtest: multi: nil strategy factory")
	}
	seen := make(map[string]bool, len(series))
	for _, sb := range series {
		if sb.Symbol == "" {
			return nil, errors.New("backtest: multi: empty symbol")
		}
		if seen[sb.Symbol] {
			return nil, fmt.Errorf("backtest: multi: duplicate symbol %s", sb.Symbol)
		}
		seen[sb.Symbol] = true
		if len(sb.Bars) == 0 {
			return nil, fmt.Errorf("backtest: multi: symbol %s: empty bars", sb.Symbol)
		}
		if err := ingest.ValidateBars(sb.Bars); err != nil {
			return nil, fmt.Errorf("backtest: multi: symbol %s: %w", sb.Symbol, err)
		}
	}
	aligned, err := alignBars(series)
	if err != nil {
		return nil, err
	}
	cash := initialCash / float64(len(series))
	subs := make([]SymbolResult, 0, len(aligned))
	for _, sb := range aligned {
		s, err := factory()
		if err != nil {
			return nil, fmt.Errorf("backtest: multi: symbol %s: strategy: %w", sb.Symbol, err)
		}
		res, err := RunOptions(ctx, sb.Bars, cash, feePerTrade, s, nil)
		if err != nil {
			return nil, fmt.Errorf("backtest: multi: symbol %s: %w", sb.Symbol, err)
		}
		subs = append(subs, SymbolResult{Symbol: sb.Symbol, Result: res})
	}

	// All sub-curves share the aligned ts list, so index i across accounts is
	// the same bar; the combined curve is their pointwise sum.
	var (
		peak, maxDD float64
		curve       []EquityPoint
	)
	first := subs[0].Result
	curve = make([]EquityPoint, 0, len(first.EquityCurve))
	for i := range first.EquityCurve {
		total := 0.0
		for _, sub := range subs {
			total += sub.Result.EquityCurve[i].Equity
		}
		eq := EquityPoint{Ts: first.EquityCurve[i].Ts, Equity: total}
		curve = append(curve, eq)
		if total > peak {
			peak = total
		}
		if peak > 0 && (peak-total)/peak > maxDD {
			maxDD = (peak - total) / peak
		}
	}
	final := curve[len(curve)-1].Equity
	return &MultiResult{
		PerSymbol:   subs,
		Equity:      final,
		TotalReturn: (final - initialCash) / initialCash,
		MaxDrawdown: maxDD,
		Bars:        len(curve),
		EquityCurve: curve,
	}, nil
}

// alignBars keeps only bars whose ts is present in every series (intersection);
// ts are compared by instant via UTC-normalized keys, so equivalent timestamps
// in different locations align. An empty intersection is an error.
func alignBars(series []SymbolBars) ([]SymbolBars, error) {
	common := make(map[time.Time]bool)
	for _, b := range series[0].Bars {
		common[b.Ts.UTC()] = true
	}
	for _, sb := range series[1:] {
		next := make(map[time.Time]bool)
		for _, b := range sb.Bars {
			if common[b.Ts.UTC()] {
				next[b.Ts.UTC()] = true
			}
		}
		common = next
	}
	if len(common) == 0 {
		return nil, errors.New("backtest: multi: no common bars across symbols (intersection empty)")
	}
	out := make([]SymbolBars, 0, len(series))
	for _, sb := range series {
		bars := make([]ingest.Bar, 0, len(common))
		for _, b := range sb.Bars {
			if common[b.Ts.UTC()] {
				bars = append(bars, b)
			}
		}
		out = append(out, SymbolBars{Symbol: sb.Symbol, Bars: bars})
	}
	return out, nil
}
