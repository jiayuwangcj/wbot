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

// Result persistence in backtest_results (migration 003, detail columns 004):
// params/metrics as JSONB, equity_curve/trades nullable; SaveResult inserts one
// run, LoadResults lists newest first, LoadResult reads one run's full trace.

// ErrResultNotFound reports LoadResult missing its row.
var ErrResultNotFound = errors.New("backtest: result not found")

// ResultRecord is one persisted backtest run; EquityCurve/Trades are nil for
// pre-004 rows (or metrics-only saves).
type ResultRecord struct {
	ID          int64
	Strategy    string
	Symbol      string
	Params      map[string]any
	Metrics     map[string]any
	StartTs     time.Time
	EndTs       time.Time
	CreatedAt   time.Time
	EquityCurve []EquityPoint
	Trades      []Trade
}

// SaveResult persists one run: params (e.g. cash/fee/adjust) plus the metrics
// derived from Result (equity/total_return/max_drawdown/bars) and, when the
// Result carries them, the equity_curve/trades trace (migration 004). Stock
// trades get their Symbol filled with the underlying symbol; option trades
// keep the contract code. A trace-less Result saves metrics only.
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
	var curveArg, tradesArg any
	if len(r.EquityCurve) > 0 {
		curveJSON, err := json.Marshal(r.EquityCurve)
		if err != nil {
			return 0, fmt.Errorf("backtest: save result: equity curve: %w", err)
		}
		curveArg = string(curveJSON)
	}
	if len(r.Trades) > 0 {
		trades := make([]Trade, len(r.Trades))
		copy(trades, r.Trades)
		for i := range trades {
			if trades[i].Symbol == "" {
				trades[i].Symbol = symbol
			}
		}
		tradesJSON, err := json.Marshal(trades)
		if err != nil {
			return 0, fmt.Errorf("backtest: save result: trades: %w", err)
		}
		tradesArg = string(tradesJSON)
	}
	var id int64
	err = db.QueryRowContext(ctx, `
INSERT INTO backtest_results (strategy, symbol, params, metrics, start_ts, end_ts, equity_curve, trades)
VALUES ($1, $2, $3::jsonb, $4::jsonb, $5, $6, $7::jsonb, $8::jsonb)
RETURNING id`, strategy, symbol, string(paramsJSON), string(metricsJSON), startTs, endTs, curveArg, tradesArg).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("backtest: save result: insert: %w", err)
	}
	return id, nil
}

// sortExprs maps whitelisted sort keys to ORDER BY expressions. Only keys
// in this map ever reach SQL — the ORDER BY fragment is built from the
// literal expression, never from caller input. Numeric metric keys cast the
// JSONB text value; NULLS LAST keeps runs with a missing metric at the
// bottom in both directions (matches the UI's -Infinity sink semantics).
var sortExprs = map[string]string{
	"id":           "id",
	"strategy":     "strategy",
	"symbol":       "symbol",
	"created_at":   "created_at",
	"equity":       "(metrics->>'equity')::numeric",
	"total_return": "(metrics->>'total_return')::numeric",
	"max_drawdown": "(metrics->>'max_drawdown')::numeric",
	"bars":         "(metrics->>'bars')::int",
}

// ValidSortKey reports whether key is a whitelisted ListResults sort key.
func ValidSortKey(key string) bool {
	_, ok := sortExprs[key]
	return ok
}

// SortKeyNames returns the whitelisted sort keys (stable order) for API
// error messages and docs.
func SortKeyNames() []string {
	return []string{"id", "strategy", "symbol", "equity", "total_return", "max_drawdown", "bars", "created_at"}
}

// escapeLike 转义 LIKE 通配符(% _),使 q 按字面包含匹配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ListResults lists saved runs; symbol/strategy exact filter when non-empty,
// q performs a contains-match (ILIKE, escaped) over symbol OR strategy,
// offset/limit page the result set (offset < 0 → 0, limit <= 0 → 50).
// sortKey "" keeps the historical newest-first order (id DESC); a
// whitelisted sortKey orders by that column instead (desc=false → ASC,
// desc=true → DESC), metrics NULLS LAST either way. The list shape is the
// summary only (no equity_curve/trades).
func ListResults(ctx context.Context, db *sql.DB, symbol, strategy, q string, offset, limit int, sortKey string, desc bool) ([]ResultRecord, error) {
	if db == nil {
		return nil, errors.New("backtest: list results: nil db")
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	conds := []string{}
	args := []any{}
	if symbol != "" {
		args = append(args, symbol)
		conds = append(conds, fmt.Sprintf("symbol = $%d", len(args)))
	}
	if strategy != "" {
		args = append(args, strategy)
		conds = append(conds, fmt.Sprintf("strategy = $%d", len(args)))
	}
	if q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf("(symbol ILIKE $%d ESCAPE '\\' OR strategy ILIKE $%d ESCAPE '\\')", n, n))
	}
	args = append(args, limit)
	query := `
SELECT id, strategy, symbol, params, metrics, start_ts, end_ts, created_at
FROM backtest_results`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	// LIMIT 用当前参数号;offset > 0 才追加参数与 OFFSET 子句
	// (offset 0 保持历史 SQL 形态)。
	if expr, ok := sortExprs[sortKey]; ok {
		dir := "ASC"
		if desc {
			dir = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s NULLS LAST LIMIT $%d", expr, dir, len(args))
	} else {
		query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("backtest: list results: query: %w", err)
	}
	defer rows.Close()

	var out []ResultRecord
	for rows.Next() {
		var rec ResultRecord
		var paramsJSON, metricsJSON []byte
		if err := rows.Scan(&rec.ID, &rec.Strategy, &rec.Symbol, &paramsJSON, &metricsJSON,
			&rec.StartTs, &rec.EndTs, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("backtest: list results: scan: %w", err)
		}
		if err := json.Unmarshal(paramsJSON, &rec.Params); err != nil {
			return nil, fmt.Errorf("backtest: list results: params: %w", err)
		}
		if err := json.Unmarshal(metricsJSON, &rec.Metrics); err != nil {
			return nil, fmt.Errorf("backtest: list results: metrics: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("backtest: list results: rows: %w", err)
	}
	return out, nil
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
	return ListResults(ctx, db, symbol, strategy, "", 0, limit, "", false)
}

// LoadResult reads one run by id including its equity_curve/trades trace;
// ErrResultNotFound when no row has that id.
func LoadResult(ctx context.Context, db *sql.DB, id int64) (*ResultRecord, error) {
	if db == nil {
		return nil, errors.New("backtest: load result: nil db")
	}
	if id <= 0 {
		return nil, errors.New("backtest: load result: id must be positive")
	}
	var rec ResultRecord
	var paramsJSON, metricsJSON, curveJSON, tradesJSON []byte
	err := db.QueryRowContext(ctx, `
SELECT id, strategy, symbol, params, metrics, start_ts, end_ts, created_at, equity_curve, trades
FROM backtest_results WHERE id = $1`, id).
		Scan(&rec.ID, &rec.Strategy, &rec.Symbol, &paramsJSON, &metricsJSON,
			&rec.StartTs, &rec.EndTs, &rec.CreatedAt, &curveJSON, &tradesJSON)
	if err == sql.ErrNoRows {
		return nil, ErrResultNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("backtest: load result: query: %w", err)
	}
	if err := json.Unmarshal(paramsJSON, &rec.Params); err != nil {
		return nil, fmt.Errorf("backtest: load result: params: %w", err)
	}
	if err := json.Unmarshal(metricsJSON, &rec.Metrics); err != nil {
		return nil, fmt.Errorf("backtest: load result: metrics: %w", err)
	}
	if curveJSON != nil {
		if err := json.Unmarshal(curveJSON, &rec.EquityCurve); err != nil {
			return nil, fmt.Errorf("backtest: load result: equity curve: %w", err)
		}
	}
	if tradesJSON != nil {
		if err := json.Unmarshal(tradesJSON, &rec.Trades); err != nil {
			return nil, fmt.Errorf("backtest: load result: trades: %w", err)
		}
	}
	return &rec, nil
}
