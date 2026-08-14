package ingest

import (
	"context"
	"database/sql"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

type mockSource struct{}

// mockSource is a fixed demo feed; from/to are ignored.
func (mockSource) Bars(_ context.Context, _ domain.Symbol, _ string, _, _ time.Time) ([]Bar, error) {
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	return []Bar{
		{Ts: base, Open: 100, High: 101, Low: 99.5, Close: 100.5, Volume: 1000},
		{Ts: base.Add(24 * time.Hour), Open: 100.5, High: 102, Low: 100, Close: 101.25, Volume: 1100},
		{Ts: base.Add(48 * time.Hour), Open: 101.25, High: 103, Low: 101, Close: 102, Volume: 900},
	}, nil
}

// RunMockIngestion runs RunIngestion with the fixed mockSource (source=mock,
// adjust passed through — default none; dev-up.sh seeds fwd demo bars so the
// 回测页 has runnable data); intended for wiring tests and demo seeding.
func RunMockIngestion(ctx context.Context, db *sql.DB, source string, symbol domain.Symbol, timeframe, adjust string) error {
	if adjust == "" {
		adjust = "none"
	}
	return RunIngestion(ctx, db, source, symbol, timeframe, adjust, "mock", time.Time{}, time.Time{}, mockSource{})
}
