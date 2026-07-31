package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RunStatus is one row of ingestion_runs, newest first.
type RunStatus struct {
	ID         int64
	Source     string
	Status     string
	StartedAt  time.Time
	FinishedAt *time.Time // nil while the run is still running
}

// RecentRuns returns the most recent ingestion runs, newest first.
func RecentRuns(ctx context.Context, db *sql.DB, limit int) ([]RunStatus, error) {
	if limit <= 0 {
		return nil, errors.New("ingest: status: invalid limit")
	}
	if db == nil {
		return nil, errors.New("ingest: status: nil db")
	}

	rows, err := db.QueryContext(ctx, `
SELECT id, source, status, started_at, finished_at FROM ingestion_runs ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("ingest: status: query: %w", err)
	}
	defer rows.Close()

	var runs []RunStatus
	for rows.Next() {
		var r RunStatus
		var finished sql.NullTime
		if err := rows.Scan(&r.ID, &r.Source, &r.Status, &r.StartedAt, &finished); err != nil {
			return nil, fmt.Errorf("ingest: status: scan: %w", err)
		}
		if finished.Valid {
			r.FinishedAt = &finished.Time
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: status: rows: %w", err)
	}
	return runs, nil
}
