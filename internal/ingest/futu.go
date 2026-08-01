package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
)

// FutuSource pulls OHLCV bars from the futu-opend-rs gateway REST
// /api/history-kline (no subscription; paged). The timeframe argument follows
// the ingest convention (1m..1mo; futu K_* names are also accepted); Adjust
// (none/fwd/back, empty = none) maps to the gateway rehab_type (DATA_STANDARD).
type FutuSource struct {
	Client *futu.Client
	Adjust string
}

// Bars fetches bars in [from, to], skipping blank bars and out-of-range rows.
func (s FutuSource) Bars(ctx context.Context, symbol domain.Symbol, timeframe string, from, to time.Time) ([]Bar, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if s.Client == nil {
		return nil, errors.New("ingest: futu source: nil client")
	}
	klType, _, err := futu.ParseTimeframe(timeframe)
	if err != nil {
		return nil, fmt.Errorf("ingest: futu source: %w", err)
	}
	rehabType, _, err := futu.ParseAdjust(s.Adjust)
	if err != nil {
		return nil, fmt.Errorf("ingest: futu source: %w", err)
	}
	kbars, err := s.Client.HistoryKline(ctx, string(symbol), klType, rehabType, from, to)
	if err != nil {
		return nil, fmt.Errorf("ingest: futu source: %w", err)
	}
	bars := make([]Bar, 0, len(kbars))
	for _, k := range kbars {
		if k.IsBlank {
			continue
		}
		bars = append(bars, Bar{Ts: k.Ts, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	return filterRange(bars, from, to), nil
}
