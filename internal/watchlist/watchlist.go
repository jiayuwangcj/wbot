// Package watchlist persists tracked symbols with their Wheel configuration.
package watchlist

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	strategycfg "github.com/jiayu/wbot/internal/strategy"
)

// Execution status values for a binding (migration 005 whitelist, same set
// the wheel runner writes and the audit UI renders).
const (
	StatusReady            = "READY"
	StatusDataBlocked      = "DATA_BLOCKED"
	StatusNeedsReconfigure = "NEEDS_RECONFIGURATION"
)

// Item is one watchlist row (symbol PK; strategy/params NOT NULL in migration 003).
type Item struct {
	Symbol             string
	Strategy           string
	Params             map[string]any
	ConfigVersion      *int
	ExecutionStatus    string
	InvalidationReason string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Validate delegates to the single strategy registry. Internal benchmark
// strategies are deliberately not accepted by the product watchlist.
func Validate(name string, params map[string]any) error {
	return strategycfg.Validate(name, params)
}

// List returns all watchlist rows ordered by symbol.
func List(ctx context.Context, db *sql.DB) ([]Item, error) {
	if db == nil {
		return nil, errors.New("watchlist: list: nil db")
	}
	rows, err := db.QueryContext(ctx, `
SELECT symbol, strategy, params, config_version, execution_status, invalidation_reason, created_at, updated_at
FROM watchlist ORDER BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("watchlist: list: query: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var it Item
		var paramsJSON []byte
		var version sql.NullInt64
		var status, reason sql.NullString
		if err := rows.Scan(&it.Symbol, &it.Strategy, &paramsJSON, &version, &status, &reason, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, fmt.Errorf("watchlist: list: scan: %w", err)
		}
		if version.Valid {
			v := int(version.Int64)
			it.ConfigVersion = &v
		}
		if status.Valid {
			it.ExecutionStatus = status.String
		}
		if reason.Valid {
			it.InvalidationReason = reason.String
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

// Upsert validates and persists one complete Wheel binding. A symbol-scoped
// transaction lock makes version allocation serial even when two writers race
// to create the first row. wheel_configs is append-only; the watchlist merely
// points at the newly-created immutable version.
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
	if err := Validate(strategy, params); err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: validate: %w", err)
	}
	canonical, err := strategycfg.CanonicalParams(params)
	if err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: validate: %w", err)
	}
	params = canonical
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: params: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// hashtextextended is deterministic for a given symbol and the advisory
	// lock is released automatically when this transaction commits/rolls back.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, symbol); err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: lock: %w", err)
	}
	var version int
	if strategy == "wheel" {
		if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(version), 0) + 1 FROM wheel_configs WHERE symbol = $1`, symbol).Scan(&version); err != nil {
			return Item{}, fmt.Errorf("watchlist: upsert: next version: %w", err)
		}
		configJSON, err := json.Marshal(map[string]any{"strategy": "wheel", "params": params})
		if err != nil {
			return Item{}, fmt.Errorf("watchlist: upsert: config: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO wheel_configs (symbol, version, config)
VALUES ($1, $2, $3::jsonb)`, symbol, version, string(configJSON)); err != nil {
			return Item{}, fmt.Errorf("watchlist: upsert: append config: %w", err)
		}
	}
	const blockedReason = "waiting for complete quote snapshot"
	var it Item
	var raw []byte
	var storedVersion sql.NullInt64
	var storedStatus, storedReason sql.NullString
	err = tx.QueryRowContext(ctx, `
INSERT INTO watchlist (symbol, strategy, params, config_version, execution_status, invalidation_reason, updated_at)
VALUES ($1, $5, $2::jsonb, NULLIF($3,0), 'DATA_BLOCKED', $4, now())
ON CONFLICT (symbol) DO UPDATE SET
	params = EXCLUDED.params,
	strategy = $5,
	config_version = NULLIF($3,0),
	execution_status = 'DATA_BLOCKED',
	invalidation_reason = $4,
	updated_at = now()
	RETURNING symbol, strategy, params, config_version, execution_status, invalidation_reason, created_at, updated_at`,
		symbol, string(paramsJSON), version, blockedReason, strategy).Scan(&it.Symbol, &it.Strategy, &raw, &storedVersion, &storedStatus, &storedReason, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: %w", err)
	}
	if storedVersion.Valid {
		v := int(storedVersion.Int64)
		it.ConfigVersion = &v
	}
	if storedStatus.Valid {
		it.ExecutionStatus = storedStatus.String
	}
	if storedReason.Valid {
		it.InvalidationReason = storedReason.String
	}
	if err := json.Unmarshal(raw, &it.Params); err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: params: %w", err)
	}
	if it.Params == nil {
		it.Params = map[string]any{}
	}
	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("watchlist: upsert: commit: %w", err)
	}
	return it, nil
}

// SetExecutionStatus updates one binding's execution status and reason
// (READY clears the reason to NULL). Status is validated against the
// migration-005 whitelist before any DB call; a missing row returns a
// sql.ErrNoRows-wrapped error.
func SetExecutionStatus(ctx context.Context, db *sql.DB, symbol, status, reason string) error {
	status = strings.ToUpper(strings.TrimSpace(status))
	switch status {
	case StatusReady, StatusDataBlocked, StatusNeedsReconfigure:
	default:
		return fmt.Errorf("watchlist: set execution status: invalid status %q (want READY, DATA_BLOCKED or NEEDS_RECONFIGURATION)", status)
	}
	if symbol == "" {
		return errors.New("watchlist: set execution status: empty symbol")
	}
	if db == nil {
		return errors.New("watchlist: set execution status: nil db")
	}
	var storedReason any
	if status != StatusReady {
		storedReason = reason
	}
	res, err := db.ExecContext(ctx, `
UPDATE watchlist SET execution_status = $2, invalidation_reason = $3, updated_at = now()
WHERE symbol = $1`, symbol, status, storedReason)
	if err != nil {
		return fmt.Errorf("watchlist: set execution status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("watchlist: set execution status: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("watchlist: set execution status: %w: symbol %s", sql.ErrNoRows, symbol)
	}
	return nil
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
