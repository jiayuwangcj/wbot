// Package watchlist persists tracked symbols with per-symbol strategy bindings
// (migration 003 watchlist table) and holds the strategy template contract
// served by GET /v1/strategies.
package watchlist

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Item is one watchlist row (symbol PK; strategy/params NOT NULL in migration 003).
type Item struct {
	Symbol    string
	Strategy  string
	Params    map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Template is the strategy template contract served by GET /v1/strategies.
// 待 ⑫-b（feat/strategy-impl）合入后接入 internal/strategy 注册表（Templates/Factory）。
type Template struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Params      []Param `json:"params"`
}

// Param declares one strategy parameter's schema (name/type/default).
type Param struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // number | string | choice
	Default     any      `json:"default"`
	Choices     []string `json:"choices,omitempty"` // required when Type == choice
	Description string   `json:"description"`
}

// Templates returns the strategy templates in contract order (⑫-b draft ⑫-c).
func Templates() []Template {
	// Numeric defaults are float64 so the JSON contract round-trips bit-identically.
	cc := []Param{
		{Name: "strike_pct_otm", Type: "number", Default: 0.03, Description: "行权价偏离度：行权价 = 现价×(1+pct) 就近 chain 档"},
		{Name: "expiry_rule", Type: "choice", Default: "next_expiry", Choices: []string{"next_expiry"}, Description: "到期选择规则"},
		{Name: "days_to_expiry", Type: "number", Default: 28.0, Description: "目标到期天数"},
		{Name: "fee_per_contract", Type: "number", Default: 0.0, Description: "每合约费用"},
	}
	return []Template{
		// buy-hold 是引擎一等策略(backtestexec 直接支持,无 params);
		// watchlist 作为「回测计划列表」收录它,使 from_watchlist 回测
		// 模式在无期权数据的环境(本地 mock)也可整表跑通。
		{Name: "buy-hold", Description: "买入持有：不调仓", Params: nil},
		{Name: "covered-call", Description: "备兑看涨：持有正股 + 卖出看涨", Params: cc},
		{Name: "cash-secured-put", Description: "现金担保看跌：卖出看跌、现金担保", Params: cc},
	}
}

// Validate checks params against the named template's schema: unknown template
// or parameter, or a type mismatch, returns an error.
func Validate(name string, params map[string]any) error {
	schema := map[string]Param{}
	var found bool
	for _, t := range Templates() {
		if t.Name == name {
			found = true
			for _, p := range t.Params {
				schema[p.Name] = p
			}
		}
	}
	if !found {
		return fmt.Errorf("unknown strategy template %q", name)
	}
	for key, v := range params {
		p, ok := schema[key]
		if !ok {
			return fmt.Errorf("unknown parameter %q for strategy %q", key, name)
		}
		switch p.Type {
		case "number":
			if _, ok := v.(float64); !ok {
				return fmt.Errorf("parameter %q: want number, got %T", key, v)
			}
		case "string":
			if _, ok := v.(string); !ok {
				return fmt.Errorf("parameter %q: want string, got %T", key, v)
			}
		case "choice":
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("parameter %q: want one of %v, got %T", key, p.Choices, v)
			}
			if !slices.Contains(p.Choices, s) {
				return fmt.Errorf("parameter %q: want one of %v, got %q", key, p.Choices, s)
			}
		}
	}
	return nil
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
