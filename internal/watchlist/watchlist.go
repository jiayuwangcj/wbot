// Package watchlist persists tracked symbols with per-symbol strategy bindings
// (migration 003 watchlist table). The strategy template contract served by
// GET /v1/strategies renders from internal/strategy (single source).
package watchlist

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/strategy"
)

// Item is one watchlist row (symbol PK; strategy/params NOT NULL in migration 003).
type Item struct {
	Symbol    string
	Strategy  string
	Params    map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate checks params against the strategy contract: buy-hold is the
// engine's first-class no-param strategy (backtestexec); everything else
// delegates to the internal/strategy registry — the single source of truth
// (doc/BACKTEST.md). The /v1/strategies contract renders from the same
// registry (strategy.ContractTemplates + appended buy-hold, httpapi).
func Validate(name string, params map[string]any) error {
	if name == "buy-hold" {
		for k := range params {
			return fmt.Errorf("unknown parameter %q for strategy %q", k, name)
		}
		return nil
	}
	return strategy.Validate(name, params)
}

// List returns all watchlist rows ordered by symbol.
func List(ctx context.Context, db *sql.DB) ([]Item, error) {
	if db == nil {
		return nil, errors.New("watchlist: list: nil db")
	}
	rows, err := db.QueryContext(ctx, `
SELECT symbol, strategy, params, created_at, updated_at FROM watchlist ORDER BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("watchlist: list: query: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var it Item
		var paramsJSON []byte
		if err := rows.Scan(&it.Symbol, &it.Strategy, &paramsJSON, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, fmt.Errorf("watchlist: list: scan: %w", err)
		}
		if err := json.Unmarshal(paramsJSON, &it.Params); err != nil {
			return nil, fmt.Errorf("watchlist: list: params: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("watchlist: list: rows: %w", err)
	}
	return out, nil
}

// Upsert inserts or replaces one symbol's binding (ON CONFLICT DO UPDATE keeps
// created_at, refreshes updated_at) and returns the stored row.
func Upsert(ctx context.Context, db *sql.DB, symbol, strategy string, params map[string]any) (Item, error) {
	if db == nil {
		return Item{}, errors.New("watchlist: upsert: nil db")
	}
	if symbol == "" {
		return Item{}, errors.New("watchlist: upsert: empty symbol")
	}
	if strategy == "" {
		return Item{}, errors.New("watchlist: upsert: empty strategy")
	}
	if params == nil {
		params = map[string]any{}
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: params: %w", err)
	}
	var it Item
	var raw []byte
	err = db.QueryRowContext(ctx, `
INSERT INTO watchlist (symbol, strategy, params, updated_at)
VALUES ($1, $2, $3::jsonb, now())
ON CONFLICT (symbol) DO UPDATE SET
	strategy = EXCLUDED.strategy,
	params = EXCLUDED.params,
	updated_at = now()
RETURNING symbol, strategy, params, created_at, updated_at`,
		symbol, strategy, string(paramsJSON)).Scan(&it.Symbol, &it.Strategy, &raw, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: %w", err)
	}
	if err := json.Unmarshal(raw, &it.Params); err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: params: %w", err)
	}
	if it.Params == nil {
		it.Params = map[string]any{}
	}
	return it, nil
}

// Delete removes one symbol; returns false when the symbol is not on the list.
func Delete(ctx context.Context, db *sql.DB, symbol string) (bool, error) {
	if db == nil {
		return false, errors.New("watchlist: delete: nil db")
	}
	res, err := db.ExecContext(ctx, `DELETE FROM watchlist WHERE symbol = $1`, symbol)
	if err != nil {
		return false, fmt.Errorf("watchlist: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("watchlist: delete: rows affected: %w", err)
	}
	return n > 0, nil
}
