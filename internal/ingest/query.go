package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BarCoverage is one symbol×timeframe×adjust combination present in the bars table.
type BarCoverage struct {
	Symbol    string
	Timeframe string
	Adjust    string
	Count     int64
	MinTs     time.Time
	MaxTs     time.Time
}

// QueryBarCoverage tallies bars per symbol×timeframe×adjust with min/max ts, ordered by symbol, timeframe.
func QueryBarCoverage(ctx context.Context, db *sql.DB) ([]BarCoverage, error) {
	if db == nil {
		return nil, errors.New("ingest: bars coverage: nil db")
	}
	rows, err := db.QueryContext(ctx, `
SELECT symbol, timeframe, adjust, COUNT(*), MIN(ts), MAX(ts) FROM bars
GROUP BY symbol, timeframe, adjust ORDER BY symbol, timeframe, adjust`)
	if err != nil {
		return nil, fmt.Errorf("ingest: bars coverage: query: %w", err)
	}
	defer rows.Close()

	var out []BarCoverage
	for rows.Next() {
		var c BarCoverage
		if err := rows.Scan(&c.Symbol, &c.Timeframe, &c.Adjust, &c.Count, &c.MinTs, &c.MaxTs); err != nil {
			return nil, fmt.Errorf("ingest: bars coverage: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: bars coverage: rows: %w", err)
	}
	return out, nil
}

// QueryBars returns a symbol/timeframe/adjust's bars in [from, to] (zero from/to unbounded),
// ts ascending; with desc, newest-first (tail-window reads, e.g. the Web UI data page).
// When providers overlap at one timestamp, one row is selected deterministically
// (futu, then tencent, then lexical source) so multi-source storage never creates
// duplicate timestamps in a backtest. Source/Adjusted retain the selected row's
// provider provenance; Tencent's canonical adjust=fwd is reported as qfq.
func QueryBars(ctx context.Context, db *sql.DB, symbol string, timeframe string, adjust string, from, to time.Time, limit int, desc bool) ([]Bar, error) {
	return queryBars(ctx, db, symbol, timeframe, adjust, from, to, limit, desc, false)
}

// QueryBarsExclusiveEnd is QueryBars with an exclusive upper bound: bars in
// [from, to) instead of [from, to]. Chunked readers advance the cursor to the
// previous chunk's end and must not re-read a bar exactly at that boundary, so
// every non-final chunk reads [cur, next) and only the final chunk reads
// [cur, to] closed (RunChunked determinism, doc/BACKTEST.md).
func QueryBarsExclusiveEnd(ctx context.Context, db *sql.DB, symbol string, timeframe string, adjust string, from, to time.Time, limit int, desc bool) ([]Bar, error) {
	return queryBars(ctx, db, symbol, timeframe, adjust, from, to, limit, desc, true)
}

func queryBars(ctx context.Context, db *sql.DB, symbol string, timeframe string, adjust string, from, to time.Time, limit int, desc bool, exclusiveEnd bool) ([]Bar, error) {
	if db == nil {
		return nil, errors.New("ingest: query bars: nil db")
	}
	if symbol == "" {
		return nil, errors.New("ingest: query bars: empty symbol")
	}
	if timeframe == "" {
		return nil, errors.New("ingest: query bars: empty timeframe")
	}
	if adjust == "" {
		return nil, errors.New("ingest: query bars: empty adjust")
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, errors.New("ingest: query bars: from after to")
	}
	if limit <= 0 {
		return nil, errors.New("ingest: query bars: invalid limit")
	}

	// Placeholders are numbered by the running arg count so conds and args stay in lockstep.
	conds := []string{"symbol = $1", "timeframe = $2", "adjust = $3"}
	args := []any{symbol, timeframe, adjust}
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, fmt.Sprintf("ts >= $%d", len(args)))
	}
	if !to.IsZero() {
		op := "<="
		if exclusiveEnd {
			op = "<"
		}
		args = append(args, to)
		conds = append(conds, fmt.Sprintf("ts %s $%d", op, len(args)))
	}
	args = append(args, limit)
	order := "ASC"
	if desc {
		order = "DESC"
	}
	query := fmt.Sprintf(`
SELECT ts, open, high, low, close, volume, source, adjust
FROM (
  SELECT DISTINCT ON (ts) ts, open, high, low, close, volume, source, adjust
  FROM bars
  WHERE %s
  ORDER BY ts,
    CASE source WHEN 'futu' THEN 0 WHEN 'tencent' THEN 1 ELSE 2 END,
    source
) selected
ORDER BY ts %s
LIMIT $%d`, strings.Join(conds, " AND "), order, len(args))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ingest: query bars: query: %w", err)
	}
	defer rows.Close()

	var bars []Bar
	for rows.Next() {
		var b Bar
		var adjustName string
		if err := rows.Scan(&b.Ts, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Source, &adjustName); err != nil {
			return nil, fmt.Errorf("ingest: query bars: scan: %w", err)
		}
		b.Adjusted = providerAdjustment(b.Source, adjustName)
		bars = append(bars, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: query bars: rows: %w", err)
	}
	return bars, nil
}

func providerAdjustment(source, adjust string) string {
	if strings.EqualFold(strings.TrimSpace(source), "tencent") {
		switch strings.ToLower(strings.TrimSpace(adjust)) {
		case "fwd":
			return "qfq"
		case "back":
			return "hfq"
		}
	}
	return strings.ToLower(strings.TrimSpace(adjust))
}
