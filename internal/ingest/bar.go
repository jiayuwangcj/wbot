package ingest

import (
	"context"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

// Bar is one OHLCV row aligned with the bars table.
type Bar struct {
	Ts     time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume int64
}

// Source yields OHLCV bars for the given symbol and timeframe.
type Source interface {
	Bars(ctx context.Context, symbol domain.Symbol, timeframe string) ([]Bar, error)
}
