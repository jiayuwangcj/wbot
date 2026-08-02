package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AccountSnapshotRow is one captured funds snapshot (account_snapshots,
// written by `wbot ingest account`). Chronological per query — the caller
// draws the equity curve from these points.
type AccountSnapshotRow struct {
	CapturedAt  time.Time
	TotalAssets float64
	Cash        float64
	MarketVal   float64
}

// QueryAccountSnapshots returns the most recent `limit` snapshots for an env,
// oldest first (drawSparkline reads chronologically). env is the canonical
// EnvName output ("simulate"|"real") as stored by `wbot ingest account`.
// limit ≤ 0 falls back to 120.
func QueryAccountSnapshots(ctx context.Context, dbq *sql.DB, env string, limit int) ([]AccountSnapshotRow, error) {
	if limit <= 0 {
		limit = 120
	}
	rows, err := dbq.QueryContext(ctx, `
SELECT captured_at, total_assets, cash, market_val
FROM account_snapshots
WHERE env = $1
ORDER BY captured_at DESC
LIMIT $2`, env, limit)
	if err != nil {
		return nil, fmt.Errorf("query account_snapshots: %w", err)
	}
	defer rows.Close()
	desc := make([]AccountSnapshotRow, 0, limit)
	for rows.Next() {
		var r AccountSnapshotRow
		if err := rows.Scan(&r.CapturedAt, &r.TotalAssets, &r.Cash, &r.MarketVal); err != nil {
			return nil, fmt.Errorf("scan account_snapshot: %w", err)
		}
		desc = append(desc, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query account_snapshots: %w", err)
	}
	// Reverse into chronological order (oldest first) for the curve.
	out := make([]AccountSnapshotRow, len(desc))
	for i := range desc {
		out[len(desc)-1-i] = desc[i]
	}
	return out, nil
}
