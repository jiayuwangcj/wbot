package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

// Bar is one OHLCV row aligned with the bars table.
type Bar struct {
	Ts       time.Time
	Open     float64
	High     float64
	Low      float64
	Close    float64
	Volume   int64
	Source   string `json:"source,omitempty"`
	Adjusted string `json:"adjusted,omitempty"`
}

// Source yields OHLCV bars in the closed interval [from, to]; zero from/to are unbounded.
type Source interface {
	Bars(ctx context.Context, symbol domain.Symbol, timeframe string, from, to time.Time) ([]Bar, error)
}

// filterRange keeps bars in the closed interval [from, to]; zero from/to are unbounded.
func filterRange(bars []Bar, from, to time.Time) []Bar {
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if !from.IsZero() && b.Ts.Before(from) {
			continue
		}
		if !to.IsZero() && b.Ts.After(to) {
			continue
		}
		out = append(out, b)
	}
	return out
}

// ValidateBar checks OHLCV sanity of one bar (ts non-zero, high/low bounds);
// the first failing condition is returned.
func ValidateBar(b Bar) error {
	switch {
	case b.Ts.IsZero():
		return errors.New("ingest: validate bar: zero timestamp")
	case b.High < b.Low:
		return fmt.Errorf("ingest: validate bar: high %v < low %v", b.High, b.Low)
	case b.High < b.Open:
		return fmt.Errorf("ingest: validate bar: high %v < open %v", b.High, b.Open)
	case b.High < b.Close:
		return fmt.Errorf("ingest: validate bar: high %v < close %v", b.High, b.Close)
	case b.Low > b.Open:
		return fmt.Errorf("ingest: validate bar: low %v > open %v", b.Low, b.Open)
	case b.Low > b.Close:
		return fmt.Errorf("ingest: validate bar: low %v > close %v", b.Low, b.Close)
	}
	return nil
}

// ValidateBars checks OHLCV sanity and strictly increasing ts; errors name the first offending bar.
func ValidateBars(bars []Bar) error {
	if len(bars) == 0 {
		return errors.New("ingest: validate bars: empty bar slice")
	}
	for i, b := range bars {
		if err := ValidateBar(b); err != nil {
			return fmt.Errorf("ingest: validate bars: bar %d: %w", i, err)
		}
		if i > 0 && !b.Ts.After(bars[i-1].Ts) {
			return fmt.Errorf("ingest: validate bars: bar %d: ts %v not after previous %v", i, b.Ts, bars[i-1].Ts)
		}
	}
	return nil
}
