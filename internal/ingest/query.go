package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BarCoverage is one symbol×timeframe combination present in the bars table.
type BarCoverage struct {
	Symbol    string
	Timeframe string
	Count     int64
	MinTs     time.Time
	MaxTs     time.Time
}

// QueryBarCoverage tallies bars per symbol×timeframe with min/max ts, ordered by symbol, timeframe.
func QueryBarCoverage(ctx context.Context, db *sql.DB) ([]BarCoverage, error) {
	if db == nil {
		return nil, errors.New("ingest: bars coverage: nil db")
	}
	rows, err := db.QueryContext(ctx, `
SELECT symbol, timeframe, COUNT(*), MIN(ts), MAX(ts) FROM bars
GROUP BY symbol, timeframe ORDER BY symbol, timeframe`)
	if err != nil {
		return nil, fmt.Errorf("ingest: bars coverage: query: %w", err)
	}
	defer rows.Close()

	var out []BarCoverage
	for rows.Next() {
		var c BarCoverage
		if err := rows.Scan(&c.Symbol, &c.Timeframe, &c.Count, &c.MinTs, &c.MaxTs); err != nil {
			return nil, fmt.Errorf("ingest: bars coverage: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: bars coverage: rows: %w", err)
	}
	return out, nil
}

// QueryBars returns a symbol/timeframe's bars in [from, to] (zero from/to unbounded), ts ascending.
func QueryBars(ctx context.Context, db *sql.DB, symbol string, timeframe string, from, to time.Time, limit int) ([]Bar, error) {
	if db == nil {
		return nil, errors.New("ingest: query bars: nil db")
	}
	if symbol == "" {
		return nil, errors.New("ingest: query bars: empty symbol")
	}
	if timeframe == "" {
		return nil, errors.New("ingest: query bars: empty timeframe")
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, errors.New("ingest: query bars: from after to")
	}
	if limit <= 0 {
		return nil, errors.New("ingest: query bars: invalid limit")
	}

	// Placeholders are numbered by the running arg count so conds and args stay in lockstep.
	conds := []string{"symbol = $1", "timeframe = $2"}
	args := []any{symbol, timeframe}
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, fmt.Sprintf("ts >= $%d", len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, fmt.Sprintf("ts <= $%d", len(args)))
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
SELECT ts, open, high, low, close, volume FROM bars WHERE %s ORDER BY ts ASC LIMIT $%d`,
		strings.Join(conds, " AND "), len(args))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ingest: query bars: query: %w", err)
	}
	defer rows.Close()

	var bars []Bar
	for rows.Next() {
		var b Bar
		if err := rows.Scan(&b.Ts, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume); err != nil {
			return nil, fmt.Errorf("ingest: query bars: scan: %w", err)
		}
		bars = append(bars, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: query bars: rows: %w", err)
	}
	return bars, nil
}
