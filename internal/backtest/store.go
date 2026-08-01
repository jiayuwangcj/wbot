package backtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Result persistence in backtest_results (migration 003): params/metrics as
// JSONB; SaveResult inserts one run, LoadResults lists newest first.

// ResultRecord is one persisted backtest run.
type ResultRecord struct {
	ID        int64
	Strategy  string
	Symbol    string
	Params    map[string]any
	Metrics   map[string]any
	StartTs   time.Time
	EndTs     time.Time
	CreatedAt time.Time
}

// SaveResult persists one run: params (e.g. cash/fee/adjust) plus the metrics
// derived from Result (equity/total_return/max_drawdown/bars).
func SaveResult(ctx context.Context, db *sql.DB, strategy, symbol string, params map[string]any, r *Result, startTs, endTs time.Time) (int64, error) {
	if db == nil {
		return 0, errors.New("backtest: save result: nil db")
	}
	if strategy == "" {
		return 0, errors.New("backtest: save result: empty strategy")
	}
	if symbol == "" {
		return 0, errors.New("backtest: save result: empty symbol")
	}
	if r == nil {
		return 0, errors.New("backtest: save result: nil result")
	}
	if startTs.IsZero() || endTs.IsZero() || startTs.After(endTs) {
		return 0, errors.New("backtest: save result: need zero-free start_ts <= end_ts")
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return 0, fmt.Errorf("backtest: save result: params: %w", err)
	}
	metricsJSON, err := json.Marshal(map[string]any{
		"equity":       r.Equity,
		"total_return": r.TotalReturn,
		"max_drawdown": r.MaxDrawdown,
		"bars":         r.Bars,
	})
	if err != nil {
		return 0, fmt.Errorf("backtest: save result: metrics: %w", err)
	}
	var id int64
	err = db.QueryRowContext(ctx, `
INSERT INTO backtest_results (strategy, symbol, params, metrics, start_ts, end_ts)
VALUES ($1, $2, $3::jsonb, $4::jsonb, $5, $6)
RETURNING id`, strategy, symbol, string(paramsJSON), string(metricsJSON), startTs, endTs).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("backtest: save result: insert: %w", err)
	}
	return id, nil
}

// LoadResults lists saved runs for symbol (strategy filters when non-empty),
// newest first; limit <= 0 defaults to 50.
func LoadResults(ctx context.Context, db *sql.DB, symbol, strategy string, limit int) ([]ResultRecord, error) {
	if db == nil {
		return nil, errors.New("backtest: load results: nil db")
	}
	if symbol == "" {
		return nil, errors.New("backtest: load results: empty symbol")
	}
	if limit <= 0 {
		limit = 50
	}
	conds := []string{"symbol = $1"}
	args := []any{symbol}
	if strategy != "" {
		args = append(args, strategy)
		conds = append(conds, fmt.Sprintf("strategy = $%d", len(args)))
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
SELECT id, strategy, symbol, params, metrics, start_ts, end_ts, created_at
FROM backtest_results WHERE %s ORDER BY id DESC LIMIT $%d`,
		strings.Join(conds, " AND "), len(args))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("backtest: load results: query: %w", err)
	}
	defer rows.Close()

	var out []ResultRecord
	for rows.Next() {
		var rec ResultRecord
		var paramsJSON, metricsJSON []byte
		if err := rows.Scan(&rec.ID, &rec.Strategy, &rec.Symbol, &paramsJSON, &metricsJSON,
			&rec.StartTs, &rec.EndTs, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("backtest: load results: scan: %w", err)
		}
		if err := json.Unmarshal(paramsJSON, &rec.Params); err != nil {
			return nil, fmt.Errorf("backtest: load results: params: %w", err)
		}
		if err := json.Unmarshal(metricsJSON, &rec.Metrics); err != nil {
			return nil, fmt.Errorf("backtest: load results: metrics: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("backtest: load results: rows: %w", err)
	}
	return out, nil
}
