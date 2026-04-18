package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jiayu/wbot/internal/domain"
)

// RunIngestion inserts one ingestion run, writes bars from src in the same
// transaction, then marks the run succeeded.
func RunIngestion(ctx context.Context, db *sql.DB, runSource string, symbol domain.Symbol, timeframe string, src Source) error {
	if db == nil {
		return errors.New("ingest: nil db")
	}
	if src == nil {
		return errors.New("ingest: nil source")
	}
	if !symbol.Valid() {
		return errors.New("ingest: invalid symbol")
	}
	if timeframe == "" {
		return errors.New("ingest: empty timeframe")
	}
	if runSource == "" {
		return errors.New("ingest: empty source")
	}

	bars, err := src.Bars(ctx, symbol, timeframe)
	if err != nil {
		return fmt.Errorf("ingest: source: %w", err)
	}
	if len(bars) == 0 {
		return errors.New("ingest: no bars from source")
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
RETURNING id`, runSource).Scan(&runID)
	if err != nil {
		return fmt.Errorf("ingest: insert run: %w", err)
	}

	const insertBar = `
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	for _, b := range bars {
		_, err = tx.ExecContext(ctx, insertBar,
			string(symbol), timeframe, b.Ts, b.Open, b.High, b.Low, b.Close, b.Volume)
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
