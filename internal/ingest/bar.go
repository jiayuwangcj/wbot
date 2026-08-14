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

// ValidateBars checks OHLCV sanity and strictly increasing ts; errors name the first offending bar.
func ValidateBars(bars []Bar) error {
	if len(bars) == 0 {
		return errors.New("ingest: validate bars: empty bar slice")
	}
	for i, b := range bars {
		if b.Ts.IsZero() {
			return fmt.Errorf("ingest: validate bars: bar %d: zero timestamp", i)
		}
		switch {
		case b.High < b.Low:
			return fmt.Errorf("ingest: validate bars: bar %d: high %v < low %v", i, b.High, b.Low)
		case b.High < b.Open:
			return fmt.Errorf("ingest: validate bars: bar %d: high %v < open %v", i, b.High, b.Open)
		case b.High < b.Close:
			return fmt.Errorf("ingest: validate bars: bar %d: high %v < close %v", i, b.High, b.Close)
		case b.Low > b.Open:
			return fmt.Errorf("ingest: validate bars: bar %d: low %v > open %v", i, b.Low, b.Open)
		case b.Low > b.Close:
			return fmt.Errorf("ingest: validate bars: bar %d: low %v > close %v", i, b.Low, b.Close)
		}
		if i > 0 && !b.Ts.After(bars[i-1].Ts) {
			return fmt.Errorf("ingest: validate bars: bar %d: ts %v not after previous %v", i, b.Ts, bars[i-1].Ts)
		}
	}
	return nil
}
