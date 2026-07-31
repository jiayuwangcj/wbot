package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// Result summarizes one backtest run.
type Result struct {
	Equity      float64
	TotalReturn float64
	MaxDrawdown float64
	Bars        int
}

// buyTol tolerates one-ulp float error in BuyHold's all-in size (cash/close).
const buyTol = 1e-9

// Run replays bars ascending, calling the strategy once per bar and settling trades at the close.
func Run(ctx context.Context, bars []ingest.Bar, initialCash float64, feePerTrade float64, s Strategy) (*Result, error) {
	if len(bars) == 0 {
		return nil, errors.New("backtest: empty bars")
	}
	if initialCash <= 0 {
		return nil, errors.New("backtest: initial cash must be > 0")
	}
	if feePerTrade < 0 {
		return nil, errors.New("backtest: negative fee")
	}
	if s == nil {
		return nil, errors.New("backtest: nil strategy")
	}
	if err := ingest.ValidateBars(bars); err != nil {
		return nil, err
	}

	st := &State{Cash: initialCash}
	var peak, maxDD float64
	for i, b := range bars {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		st.Price = b.Close
		act, size, err := s.OnBar(ctx, b, st)
		if err != nil {
			return nil, fmt.Errorf("backtest: bar %d: strategy: %w", i, err)
		}
		switch act {
		case ActionHold:
		case ActionBuy:
			if size < 0 || size*b.Close > st.Cash+buyTol {
				return nil, fmt.Errorf("backtest: bar %d: buy %v shares at close %v exceeds cash %v", i, size, b.Close, st.Cash)
			}
			st.Cash -= size*b.Close + feePerTrade
			st.Position += size
		case ActionSell:
			if size < 0 || size > st.Position+buyTol {
				return nil, fmt.Errorf("backtest: bar %d: sell %v shares exceeds position %v", i, size, st.Position)
			}
			st.Cash += size*b.Close - feePerTrade
			st.Position -= size
		default:
			return nil, fmt.Errorf("backtest: bar %d: unknown action %d", i, act)
		}
		eq := st.Equity(b.Close)
		if eq > peak {
			peak = eq
		}
		if peak > 0 && (peak-eq)/peak > maxDD {
			maxDD = (peak - eq) / peak
		}
	}

	final := st.Equity(bars[len(bars)-1].Close)
	return &Result{
		Equity:      final,
		TotalReturn: (final - initialCash) / initialCash,
		MaxDrawdown: maxDD,
		Bars:        len(bars),
	}, nil
}

// barRecord is the JSON wire format of one bar, matching `ingest bars -json`.
type barRecord struct {
	Ts     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

// ParseBars parses a JSON array of bars in `ingest bars -json` format; Run checks sanity/order.
func ParseBars(data []byte) ([]ingest.Bar, error) {
	var recs []barRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("backtest: parse bars: json: %w", err)
	}
	out := make([]ingest.Bar, 0, len(recs))
	for i, r := range recs {
		ts, err := time.Parse(time.RFC3339Nano, r.Ts)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, r.Ts)
			if err != nil {
				return nil, fmt.Errorf("backtest: parse bars: record %d ts: %w", i, err)
			}
		}
		out = append(out, ingest.Bar{
			Ts: ts.UTC(), Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		})
	}
	return out, nil
}
