package datacheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/watchlist"
)

// Snapshot reads the watchlist and all market-data coverage needed by Check.
func Snapshot(ctx context.Context, db *sql.DB) ([]string, []ingest.BarCoverage, []OptionCoverage, error) {
	if db == nil {
		return nil, nil, nil, errors.New("datacheck: nil db")
	}
	items, err := watchlist.List(ctx, db)
	if err != nil {
		return nil, nil, nil, err
	}
	symbols := make([]string, 0, len(items))
	for _, item := range items {
		symbols = append(symbols, item.Symbol)
	}
	bars, err := ingest.QueryBarCoverage(ctx, db)
	if err != nil {
		return nil, nil, nil, err
	}
	options, err := queryOptionCoverage(ctx, db)
	if err != nil {
		return nil, nil, nil, err
	}
	return symbols, bars, options, nil
}

func queryOptionCoverage(ctx context.Context, db *sql.DB) ([]OptionCoverage, error) {
	// Keep MaxExpiry from the same (latest) quote snapshot as MaxTs. A plain
	// MAX(ts), MAX(expiry) aggregation can combine two different snapshots and
	// incorrectly make a latest quote from an expired chain look current.
	rows, err := db.QueryContext(ctx, `
WITH latest AS (
	SELECT underlying, MAX(ts) AS max_ts
	FROM option_quotes
	GROUP BY underlying
)
SELECT q.underlying, latest.max_ts, MAX(q.expiry)
FROM option_quotes q
JOIN latest ON latest.underlying = q.underlying AND latest.max_ts = q.ts
GROUP BY q.underlying, latest.max_ts
ORDER BY q.underlying`)
	if err != nil {
		return nil, fmt.Errorf("datacheck: option coverage: query: %w", err)
	}
	defer rows.Close()
	coverage := []OptionCoverage{}
	for rows.Next() {
		var item OptionCoverage
		if err := rows.Scan(&item.Underlying, &item.MaxTs, &item.MaxExpiry); err != nil {
			return nil, fmt.Errorf("datacheck: option coverage: scan: %w", err)
		}
		coverage = append(coverage, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("datacheck: option coverage: rows: %w", err)
	}
	return coverage, nil
}
