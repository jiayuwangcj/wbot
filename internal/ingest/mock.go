package ingest

import (
	"context"
	"database/sql"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

type mockSource struct{}

func (mockSource) Bars(_ context.Context, _ domain.Symbol, _ string) ([]Bar, error) {
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	return []Bar{
		{base, 100, 101, 99.5, 100.5, 1000},
		{base.Add(24 * time.Hour), 100.5, 102, 100, 101.25, 1100},
		{base.Add(48 * time.Hour), 101.25, 103, 101, 102, 900},
	}, nil
}

// RunMockIngestion inserts one ingestion run, a few sample OHLCV bars, then marks
// the run finished. Intended for pipeline wiring tests; not a real market feed.
func RunMockIngestion(ctx context.Context, db *sql.DB, source string, symbol domain.Symbol, timeframe string) error {
	return RunIngestion(ctx, db, source, symbol, timeframe, mockSource{})
}
