package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/domain"
)

// RunMockIngestion inserts one ingestion run, a few sample OHLCV bars, then marks
// the run finished. Intended for pipeline wiring tests; not a real market feed.
func RunMockIngestion(ctx context.Context, db *sql.DB, source string, symbol domain.Symbol, timeframe string) error {
	if db == nil {
		return errors.New("ingest: nil db")
	}
	if !symbol.Valid() {
		return errors.New("ingest: invalid symbol")
	}
	if timeframe == "" {
		return errors.New("ingest: empty timeframe")
	}
	if source == "" {
		return errors.New("ingest: empty source")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var runID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO ingestion_runs (source, status)
VALUES ($1, 'running')
RETURNING id`, source).Scan(&runID)
	if err != nil {
		return fmt.Errorf("ingest: insert run: %w", err)
	}

	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	bars := []struct {
		ts     time.Time
		open   float64
		high   float64
		low    float64
		close  float64
		volume int64
	}{
		{base, 100, 101, 99.5, 100.5, 1000},
		{base.Add(24 * time.Hour), 100.5, 102, 100, 101.25, 1100},
		{base.Add(48 * time.Hour), 101.25, 103, 101, 102, 900},
	}

	const insertBar = `
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	for _, b := range bars {
		_, err = tx.ExecContext(ctx, insertBar,
			string(symbol), timeframe, b.ts, b.open, b.high, b.low, b.close, b.volume)
		if err != nil {
			return fmt.Errorf("ingest: insert bar: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
UPDATE ingestion_runs SET finished_at = now(), status = $2 WHERE id = $1`,
		runID, "succeeded")
	if err != nil {
		return fmt.Errorf("ingest: finish run: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
