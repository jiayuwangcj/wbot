// Package wheelstore persists the dynamic Wheel strategy's immutable market
// observations, versioned configuration, signals, and operator audit trail.
// It intentionally contains no broker/order client and exposes no automatic
// execution operation.
package wheelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("wheelstore: record not found")
	ErrNilDB         = errors.New("wheelstore: nil db")
	ErrInvalidRecord = errors.New("wheelstore: invalid record")
	ErrInvalidAction = errors.New("wheelstore: action must be ALERT or HOLD")
	ErrInvalidStatus = errors.New("wheelstore: capability status must be READY or DATA_BLOCKED")
	ErrInvalidOp     = errors.New("wheelstore: action must be CONFIRM, IGNORE, FILL, NOTE, LLM_REVIEW, NO, or REJECTED")
)

// ConfigRecord is one version of a symbol's strategy configuration. Config
// and State are JSON objects so this package remains independent of the Wheel
// domain package while retaining fields added by future versions.
type ConfigRecord struct {
	ID        int64
	Symbol    string
	Version   int
	Config    map[string]any
	State     map[string]any
	CreatedAt time.Time
}

// QuoteSnapshotRecord is an append-only observation. Pointer fields preserve
// an omitted provider field; callers must never turn an incomplete snapshot
// into an ALERT signal.
type QuoteSnapshotRecord struct {
	ID              int64
	Symbol          string
	Underlying      string
	OptionType      string
	Strike          float64
	Expiry          time.Time
	Source          string
	SnapshotKey     string
	UnderlyingPrice *float64
	Delta           *float64
	Bid             *float64
	Ask             *float64
	IV              *float64
	Theta           *float64
	Volume          *int64
	OpenInterest    *int64
	LotSize         *int64
	ObservedAt      time.Time
	IngestedAt      time.Time
}

// Complete reports whether all fields needed by the decision engine are
// present. A persisted incomplete observation is valid for diagnostics, but
// must only result in HOLD.
func (r QuoteSnapshotRecord) Complete() bool {
	if r.Delta == nil || r.Bid == nil || r.Ask == nil || r.IV == nil || r.Theta == nil ||
		r.Volume == nil || r.OpenInterest == nil || r.LotSize == nil ||
		r.UnderlyingPrice == nil || r.Source == "" || r.SnapshotKey == "" {
		return false
	}
	if r.OptionType == "PUT" {
		return *r.Delta >= -1 && *r.Delta <= 0
	}
	return *r.Delta >= 0 && *r.Delta <= 1
}

// InventorySnapshot is persisted both as structured columns and JSON for an
// auditable, forward-compatible inventory snapshot.
type InventorySnapshot struct {
	CurrentPrice       *float64 `json:"current_price,omitempty"`
	ActualInventory    *float64 `json:"actual_inventory,omitempty"`
	OptionDeltaStock   *float64 `json:"option_delta_stock,omitempty"`
	EffectiveInventory *float64 `json:"effective_inventory,omitempty"`
	TargetInventory    *float64 `json:"target_inventory,omitempty"`
	InventoryGap       *float64 `json:"inventory_gap,omitempty"`
}

// SignalRecord contains only an ALERT/HOLD recommendation.
type SignalRecord struct {
	ID               int64
	Symbol           string
	Action           string
	ConfigVersion    int
	CapabilityStatus string
	BlockedBy        []string
	Inventory        InventorySnapshot
	Candidates       []Candidate
	RejectionReasons []string
	Reason           string
	CreatedAt        time.Time
}

// ActionRecord records an operator's disposition. FILL means a human reported
// a fill; it does not submit, reconcile, or trigger an order.
type ActionRecord struct {
	ID        int64
	SignalID  int64
	Action    string
	Actor     string
	Note      string
	Details   map[string]any
	CreatedAt time.Time
}

// Store is a repository backed by PostgreSQL.
type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) check() error {
	if s == nil || s.db == nil {
		return ErrNilDB
	}
	return nil
}

func validateJSONMap(name string, v map[string]any, allowNil bool) ([]byte, error) {
	if v == nil && !allowNil {
		return nil, fmt.Errorf("%w: %s must not be nil", ErrInvalidRecord, name)
	}
	if v == nil {
		v = map[string]any{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidRecord, name, err)
	}
	return b, nil
}

func validateConfig(r ConfigRecord) ([]byte, []byte, error) {
	if strings.TrimSpace(r.Symbol) == "" || r.Version <= 0 {
		return nil, nil, fmt.Errorf("%w: symbol and positive version are required", ErrInvalidRecord)
	}
	cfg, err := validateJSONMap("config", r.Config, false)
	if err != nil {
		return nil, nil, err
	}
	state, err := validateJSONMap("state", r.State, true)
	if err != nil {
		return nil, nil, err
	}
	return cfg, state, nil
}

// AppendConfig inserts a new immutable version. Versions are unique per
// symbol; use a new version rather than overwriting history.
func (s *Store) AppendConfig(ctx context.Context, r ConfigRecord) (int64, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	cfg, state, err := validateConfig(r)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
INSERT INTO wheel_configs (symbol, version, config, state)
VALUES ($1, $2, $3::jsonb, $4::jsonb)
RETURNING id`, r.Symbol, r.Version, string(cfg), string(state)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("wheelstore: append config: %w", err)
	}
	return id, nil
}

func scanMap(raw []byte, name string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("wheelstore: decode %s: %w", name, err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func scanConfig(scanner interface{ Scan(...any) error }) (ConfigRecord, error) {
	var r ConfigRecord
	var cfg, state []byte
	if err := scanner.Scan(&r.ID, &r.Symbol, &r.Version, &cfg, &state, &r.CreatedAt); err != nil {
		return r, err
	}
	var err error
	if r.Config, err = scanMap(cfg, "config"); err != nil {
		return r, err
	}
	if r.State, err = scanMap(state, "state"); err != nil {
		return r, err
	}
	return r, nil
}

func (s *Store) GetConfig(ctx context.Context, symbol string, version int) (*ConfigRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(symbol) == "" || version <= 0 {
		return nil, fmt.Errorf("%w: symbol and positive version are required", ErrInvalidRecord)
	}
	r, err := scanConfig(s.db.QueryRowContext(ctx, `
SELECT id, symbol, version, config, state, created_at
FROM wheel_configs WHERE symbol = $1 AND version = $2`, symbol, version))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("wheelstore: get config: %w", err)
	}
	return &r, nil
}

func (s *Store) LatestConfig(ctx context.Context, symbol string) (*ConfigRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("%w: symbol is required", ErrInvalidRecord)
	}
	r, err := scanConfig(s.db.QueryRowContext(ctx, `
SELECT id, symbol, version, config, state, created_at
FROM wheel_configs WHERE symbol = $1 ORDER BY version DESC LIMIT 1`, symbol))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("wheelstore: latest config: %w", err)
	}
	return &r, nil
}

func (s *Store) ListConfigs(ctx context.Context, symbol string, limit int) ([]ConfigRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	args := []any{limit}
	q := `SELECT id, symbol, version, config, state, created_at FROM wheel_configs`
	if strings.TrimSpace(symbol) != "" {
		args = []any{symbol, limit}
		q += ` WHERE symbol = $1`
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("wheelstore: list configs: %w", err)
	}
	defer rows.Close()
	var out []ConfigRecord
	for rows.Next() {
		r, e := scanConfig(rows)
		if e != nil {
			return nil, fmt.Errorf("wheelstore: list configs scan: %w", e)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wheelstore: list configs rows: %w", err)
	}
	return out, nil
}

func validateQuote(r QuoteSnapshotRecord) error {
	if strings.TrimSpace(r.Symbol) == "" || strings.TrimSpace(r.Underlying) == "" || strings.TrimSpace(r.Source) == "" || strings.TrimSpace(r.SnapshotKey) == "" || r.Expiry.IsZero() || r.ObservedAt.IsZero() || r.Strike <= 0 || !finite(r.Strike) {
		return fmt.Errorf("%w: quote identity/timestamps/strike are required", ErrInvalidRecord)
	}
	r.OptionType = strings.ToUpper(strings.TrimSpace(r.OptionType))
	if r.OptionType != "PUT" && r.OptionType != "CALL" {
		return fmt.Errorf("%w: option type must be PUT or CALL", ErrInvalidRecord)
	}
	for name, p := range map[string]*float64{"delta": r.Delta, "bid": r.Bid, "ask": r.Ask, "iv": r.IV, "theta": r.Theta} {
		if p != nil && !finite(*p) {
			return fmt.Errorf("%w: %s is not finite", ErrInvalidRecord, name)
		}
	}
	if r.Bid != nil && *r.Bid <= 0 || r.Ask != nil && *r.Ask < 0 {
		return fmt.Errorf("%w: bid must be positive and ask non-negative", ErrInvalidRecord)
	}
	if r.Bid != nil && r.Ask != nil && *r.Ask < *r.Bid {
		return fmt.Errorf("%w: ask is below bid", ErrInvalidRecord)
	}
	if r.IV != nil && *r.IV < 0 {
		return fmt.Errorf("%w: implied volatility must be non-negative", ErrInvalidRecord)
	}
	if r.UnderlyingPrice != nil && (*r.UnderlyingPrice < 0 || !finite(*r.UnderlyingPrice)) {
		return fmt.Errorf("%w: underlying price must be non-negative and finite", ErrInvalidRecord)
	}
	if r.Delta != nil {
		if r.OptionType == "PUT" && (*r.Delta < -1 || *r.Delta > 0) || r.OptionType == "CALL" && (*r.Delta < 0 || *r.Delta > 1) {
			return fmt.Errorf("%w: delta is outside the %s range", ErrInvalidRecord, r.OptionType)
		}
	}
	for name, p := range map[string]*int64{"volume": r.Volume, "open_interest": r.OpenInterest} {
		if p != nil && *p < 0 {
			return fmt.Errorf("%w: %s must be non-negative", ErrInvalidRecord, name)
		}
	}
	if r.LotSize != nil && *r.LotSize <= 0 {
		return fmt.Errorf("%w: lot size must be positive", ErrInvalidRecord)
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func farg(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
func iarg(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func (s *Store) AppendQuoteSnapshot(ctx context.Context, r QuoteSnapshotRecord) (int64, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	if err := validateQuote(r); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO option_quote_snapshots
(symbol, underlying, option_type, strike, expiry, source, snapshot_key, underlying_price, delta, bid, ask, iv, theta, volume, open_interest, lot_size, observed_at, ingested_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,COALESCE($18, now())) RETURNING id`,
		r.Symbol, r.Underlying, strings.ToUpper(r.OptionType), r.Strike, r.Expiry, r.Source, r.SnapshotKey, farg(r.UnderlyingPrice), farg(r.Delta), farg(r.Bid), farg(r.Ask), farg(r.IV), farg(r.Theta), iarg(r.Volume), iarg(r.OpenInterest), iarg(r.LotSize), r.ObservedAt, nullableTime(r.IngestedAt)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("wheelstore: append quote snapshot: %w", err)
	}
	return id, nil
}

func nullableTime(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return v
}

// QueryQuoteSnapshots returns newest observations, optionally filtered by
// contract symbol and bounded by observed time.
func (s *Store) QueryQuoteSnapshots(ctx context.Context, symbol string, since, until time.Time, limit int) ([]QuoteSnapshotRecord, error) {
	return s.queryQuoteSnapshots(ctx, "symbol", symbol, since, until, limit)
}

// QueryUnderlyingQuoteSnapshots returns complete atomic batches for one
// underlying. limit counts batches, never contract rows, so a multi-contract
// snapshot cannot be truncated into a synthetic partial batch.
func (s *Store) QueryUnderlyingQuoteSnapshots(ctx context.Context, underlying string, since, until time.Time, limit int) ([]QuoteSnapshotRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	underlying = strings.TrimSpace(underlying)
	if underlying == "" {
		return nil, fmt.Errorf("%w: underlying is required", ErrInvalidRecord)
	}
	if limit <= 0 {
		limit = 100
	}
	conds := []string{"underlying = $1"}
	args := []any{underlying}
	if !since.IsZero() {
		args = append(args, since)
		conds = append(conds, fmt.Sprintf("observed_at >= $%d", len(args)))
	}
	if !until.IsZero() {
		args = append(args, until)
		conds = append(conds, fmt.Sprintf("observed_at <= $%d", len(args)))
	}
	args = append(args, limit)
	q := fmt.Sprintf(`
WITH batches AS (
  SELECT observed_at, snapshot_key, MAX(id) AS newest_id
  FROM option_quote_snapshots
  WHERE %s
  GROUP BY observed_at, snapshot_key
  ORDER BY observed_at DESC, newest_id DESC
  LIMIT $%d
)
SELECT q.id,q.symbol,q.underlying,q.option_type,q.strike,q.expiry,q.source,q.snapshot_key,
       q.underlying_price,q.delta,q.bid,q.ask,q.iv,q.theta,q.volume,q.open_interest,
       q.lot_size,q.observed_at,q.ingested_at
FROM option_quote_snapshots q
JOIN batches b ON b.observed_at = q.observed_at AND b.snapshot_key = q.snapshot_key
WHERE q.underlying = $1
ORDER BY q.observed_at DESC, q.snapshot_key DESC, q.id DESC`, strings.Join(conds, " AND "), len(args))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("wheelstore: query underlying quote snapshots: %w", err)
	}
	defer rows.Close()
	return scanQuoteSnapshotRows(rows)
}

// QueryQuoteSnapshotsByUnderlying is an explicit alias for adapters whose
// naming follows the existing QueryQuoteSnapshots method.
func (s *Store) QueryQuoteSnapshotsByUnderlying(ctx context.Context, underlying string, since, until time.Time, limit int) ([]QuoteSnapshotRecord, error) {
	return s.QueryUnderlyingQuoteSnapshots(ctx, underlying, since, until, limit)
}

func (s *Store) queryQuoteSnapshots(ctx context.Context, identityColumn, identity string, since, until time.Time, limit int) ([]QuoteSnapshotRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	conds := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(identity) != "" {
		args = append(args, identity)
		conds = append(conds, fmt.Sprintf("symbol = $%d", len(args)))
		if identityColumn == "underlying" {
			conds[len(conds)-1] = fmt.Sprintf("underlying = $%d", len(args))
		}
	}
	if !since.IsZero() {
		args = append(args, since)
		conds = append(conds, fmt.Sprintf("observed_at >= $%d", len(args)))
	}
	if !until.IsZero() {
		args = append(args, until)
		conds = append(conds, fmt.Sprintf("observed_at <= $%d", len(args)))
	}
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT id,symbol,underlying,option_type,strike,expiry,source,snapshot_key,underlying_price,delta,bid,ask,iv,theta,volume,open_interest,lot_size,observed_at,ingested_at FROM option_quote_snapshots WHERE %s ORDER BY observed_at DESC, id DESC LIMIT $%d`, strings.Join(conds, " AND "), len(args))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("wheelstore: query quote snapshots: %w", err)
	}
	defer rows.Close()
	return scanQuoteSnapshotRows(rows)
}

func scanQuoteSnapshotRows(rows *sql.Rows) ([]QuoteSnapshotRecord, error) {
	var out []QuoteSnapshotRecord
	for rows.Next() {
		var r QuoteSnapshotRecord
		var up, d, b, a, iv, th sql.NullFloat64
		var vol, oi, lot sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Symbol, &r.Underlying, &r.OptionType, &r.Strike, &r.Expiry, &r.Source, &r.SnapshotKey, &up, &d, &b, &a, &iv, &th, &vol, &oi, &lot, &r.ObservedAt, &r.IngestedAt); err != nil {
			return nil, fmt.Errorf("wheelstore: query quote snapshots scan: %w", err)
		}
		r.UnderlyingPrice = nullFloat(up)
		r.Delta = nullFloat(d)
		r.Bid = nullFloat(b)
		r.Ask = nullFloat(a)
		r.IV = nullFloat(iv)
		r.Theta = nullFloat(th)
		r.Volume = nullInt(vol)
		r.OpenInterest = nullInt(oi)
		r.LotSize = nullInt(lot)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wheelstore: query quote snapshots rows: %w", err)
	}
	return out, nil
}

// ListQuoteSnapshots is the common bounded-list form without time filters.
func (s *Store) ListQuoteSnapshots(ctx context.Context, symbol string, limit int) ([]QuoteSnapshotRecord, error) {
	return s.QueryQuoteSnapshots(ctx, symbol, time.Time{}, time.Time{}, limit)
}

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	x := v.Float64
	return &x
}
func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}

func inventoryJSON(v InventorySnapshot) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, fmt.Errorf("%w: inventory: %v", ErrInvalidRecord, e)
	}
	return b, nil
}
func arrayJSON[T any](v []T, name string) ([]byte, error) {
	if v == nil {
		v = []T{}
	}
	b, e := json.Marshal(v)
	if e != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidRecord, name, e)
	}
	return b, nil
}

func validateSignal(r *SignalRecord) error {
	r.Action = strings.ToUpper(strings.TrimSpace(r.Action))
	if r.Action != "ALERT" && r.Action != "HOLD" {
		return ErrInvalidAction
	}
	if strings.TrimSpace(r.Symbol) == "" || r.ConfigVersion <= 0 || strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("%w: signal symbol, config version, and reason are required", ErrInvalidRecord)
	}
	r.CapabilityStatus = strings.ToUpper(strings.TrimSpace(r.CapabilityStatus))
	switch r.CapabilityStatus {
	case "READY":
		if len(r.BlockedBy) != 0 {
			return fmt.Errorf("%w: READY capability must not have blockers", ErrInvalidRecord)
		}
	case "DATA_BLOCKED":
		if len(r.BlockedBy) == 0 {
			return fmt.Errorf("%w: DATA_BLOCKED capability requires at least one blocker", ErrInvalidRecord)
		}
	default:
		return fmt.Errorf("%w: capability status must be READY or DATA_BLOCKED", ErrInvalidRecord)
	}
	if r.Action == "ALERT" && r.CapabilityStatus != "READY" {
		return fmt.Errorf("%w: ALERT requires READY capability", ErrInvalidRecord)
	}
	// ALERT is deliberately stricter than HOLD: an actionable recommendation
	// must have a complete inventory snapshot and at least one candidate. This
	// makes missing quote/Greeks data fail closed at the persistence boundary.
	if r.Action == "ALERT" {
		if r.Inventory.CurrentPrice == nil || r.Inventory.ActualInventory == nil ||
			r.Inventory.OptionDeltaStock == nil || r.Inventory.EffectiveInventory == nil ||
			r.Inventory.TargetInventory == nil || r.Inventory.InventoryGap == nil {
			return fmt.Errorf("%w: ALERT requires a complete inventory snapshot", ErrInvalidRecord)
		}
		if len(r.Candidates) == 0 {
			return fmt.Errorf("%w: ALERT requires at least one candidate", ErrInvalidRecord)
		}
	}
	return nil
}

func (s *Store) AppendSignal(ctx context.Context, r SignalRecord) (int64, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	if err := validateSignal(&r); err != nil {
		return 0, err
	}
	inv, e := inventoryJSON(r.Inventory)
	if e != nil {
		return 0, e
	}
	c, e := arrayJSON(r.Candidates, "candidates")
	if e != nil {
		return 0, e
	}
	rej, e := arrayJSON(r.RejectionReasons, "rejection reasons")
	if e != nil {
		return 0, e
	}
	blocked, e := arrayJSON(r.BlockedBy, "blocked by")
	if e != nil {
		return 0, e
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO wheel_signals (symbol,action,config_version,capability_status,blocked_by,current_price,actual_inventory,option_delta_stock,effective_inventory,target_inventory,inventory_gap,inventory_snapshot,candidates,rejection_reasons,reason) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14::jsonb,$15) RETURNING id`, r.Symbol, r.Action, r.ConfigVersion, r.CapabilityStatus, string(blocked), farg(r.Inventory.CurrentPrice), farg(r.Inventory.ActualInventory), farg(r.Inventory.OptionDeltaStock), farg(r.Inventory.EffectiveInventory), farg(r.Inventory.TargetInventory), farg(r.Inventory.InventoryGap), string(inv), string(c), string(rej), r.Reason).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("wheelstore: append signal: %w", err)
	}
	return id, nil
}
func scanSignal(scanner interface{ Scan(...any) error }) (SignalRecord, error) {
	var r SignalRecord
	var inv, c, rej, blocked []byte
	var cp, ai, ods, ei, ti, ig sql.NullFloat64
	if err := scanner.Scan(&r.ID, &r.Symbol, &r.Action, &r.ConfigVersion, &r.CapabilityStatus, &blocked, &cp, &ai, &ods, &ei, &ti, &ig, &inv, &c, &rej, &r.Reason, &r.CreatedAt); err != nil {
		return r, err
	}
	r.Inventory = InventorySnapshot{nullFloat(cp), nullFloat(ai), nullFloat(ods), nullFloat(ei), nullFloat(ti), nullFloat(ig)}
	if err := json.Unmarshal(blocked, &r.BlockedBy); err != nil {
		return r, fmt.Errorf("wheelstore: decode blocked by: %w", err)
	}
	if r.Candidates == nil {
		r.Candidates = []Candidate{}
	}
	if err := json.Unmarshal(c, &r.Candidates); err != nil {
		return r, fmt.Errorf("wheelstore: decode candidates: %w", err)
	}
	if err := json.Unmarshal(rej, &r.RejectionReasons); err != nil {
		return r, fmt.Errorf("wheelstore: decode rejection reasons: %w", err)
	}
	return r, nil
}

func (s *Store) GetSignal(ctx context.Context, id int64) (*SignalRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, fmt.Errorf("%w: positive signal id required", ErrInvalidRecord)
	}
	r, e := scanSignal(s.db.QueryRowContext(ctx, `SELECT id,symbol,action,config_version,capability_status,blocked_by,current_price,actual_inventory,option_delta_stock,effective_inventory,target_inventory,inventory_gap,inventory_snapshot,candidates,rejection_reasons,reason,created_at FROM wheel_signals WHERE id=$1`, id))
	if e == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, fmt.Errorf("wheelstore: get signal: %w", e)
	}
	return &r, nil
}

func (s *Store) ListSignals(ctx context.Context, symbol, action, capability string, limit int) ([]SignalRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	conds := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(symbol) != "" {
		args = append(args, symbol)
		conds = append(conds, fmt.Sprintf("symbol=$%d", len(args)))
	}
	if strings.TrimSpace(action) != "" {
		action = strings.ToUpper(action)
		if action != "ALERT" && action != "HOLD" {
			return nil, ErrInvalidAction
		}
		args = append(args, action)
		conds = append(conds, fmt.Sprintf("action=$%d", len(args)))
	}
	if strings.TrimSpace(capability) != "" {
		capability = strings.ToUpper(capability)
		if capability != "READY" && capability != "DATA_BLOCKED" {
			return nil, ErrInvalidStatus
		}
		args = append(args, capability)
		conds = append(conds, fmt.Sprintf("capability_status=$%d", len(args)))
	}
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT id,symbol,action,config_version,capability_status,blocked_by,current_price,actual_inventory,option_delta_stock,effective_inventory,target_inventory,inventory_gap,inventory_snapshot,candidates,rejection_reasons,reason,created_at FROM wheel_signals WHERE %s ORDER BY created_at DESC,id DESC LIMIT $%d`, strings.Join(conds, " AND "), len(args))
	rows, e := s.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, fmt.Errorf("wheelstore: list signals: %w", e)
	}
	defer rows.Close()
	var out []SignalRecord
	for rows.Next() {
		r, e := scanSignal(rows)
		if e != nil {
			return nil, fmt.Errorf("wheelstore: list signals scan: %w", e)
		}
		out = append(out, r)
	}
	if e := rows.Err(); e != nil {
		return nil, fmt.Errorf("wheelstore: list signals rows: %w", e)
	}
	return out, nil
}

func (s *Store) QuerySignals(ctx context.Context, symbol, action string, limit int) ([]SignalRecord, error) {
	return s.ListSignals(ctx, symbol, action, "", limit)
}

func validAction(a string) bool {
	switch strings.ToUpper(strings.TrimSpace(a)) {
	case "CONFIRM", "IGNORE", "FILL", "NOTE", "LLM_REVIEW", "NO", "REJECTED":
		return true
	}
	return false
}
func validateAction(r *ActionRecord) error {
	if r.SignalID <= 0 || strings.TrimSpace(r.Actor) == "" {
		return fmt.Errorf("%w: signal id and actor are required", ErrInvalidRecord)
	}
	r.Action = strings.ToUpper(strings.TrimSpace(r.Action))
	if !validAction(r.Action) {
		return ErrInvalidOp
	}
	return nil
}
func (s *Store) AppendAction(ctx context.Context, r ActionRecord) (int64, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	if err := validateAction(&r); err != nil {
		return 0, err
	}
	d, e := validateJSONMap("details", r.Details, true)
	if e != nil {
		return 0, e
	}
	var id int64
	e = s.db.QueryRowContext(ctx, `INSERT INTO wheel_signal_actions (signal_id,action,actor,note,details) VALUES ($1,$2,$3,$4,$5::jsonb) RETURNING id`, r.SignalID, r.Action, r.Actor, r.Note, string(d)).Scan(&id)
	if e != nil {
		return 0, fmt.Errorf("wheelstore: append action: %w", e)
	}
	return id, nil
}
func scanAction(scanner interface{ Scan(...any) error }) (ActionRecord, error) {
	var r ActionRecord
	var d []byte
	if e := scanner.Scan(&r.ID, &r.SignalID, &r.Action, &r.Actor, &r.Note, &d, &r.CreatedAt); e != nil {
		return r, e
	}
	var e error
	r.Details, e = scanMap(d, "action details")
	return r, e
}
func (s *Store) ListActions(ctx context.Context, signalID int64) ([]ActionRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if signalID <= 0 {
		return nil, fmt.Errorf("%w: positive signal id required", ErrInvalidRecord)
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id,signal_id,action,actor,note,details,created_at FROM wheel_signal_actions WHERE signal_id=$1 ORDER BY created_at ASC,id ASC`, signalID)
	if e != nil {
		return nil, fmt.Errorf("wheelstore: list actions: %w", e)
	}
	defer rows.Close()
	var out []ActionRecord
	for rows.Next() {
		r, e := scanAction(rows)
		if e != nil {
			return nil, fmt.Errorf("wheelstore: list actions scan: %w", e)
		}
		out = append(out, r)
	}
	if e := rows.Err(); e != nil {
		return nil, fmt.Errorf("wheelstore: list actions rows: %w", e)
	}
	return out, nil
}

func (s *Store) QueryActions(ctx context.Context, signalID int64) ([]ActionRecord, error) {
	return s.ListActions(ctx, signalID)
}

// LatestLLMReview returns the most recent LLM_REVIEW action for a signal
// (the pre-order gate's verdict; ErrNotFound when no review exists).
func (s *Store) LatestLLMReview(ctx context.Context, signalID int64) (*ActionRecord, error) {
	return s.LatestAction(ctx, signalID, "LLM_REVIEW")
}

// LatestAction returns the most recent action of the requested type for a
// signal. ErrNotFound means the signal has no such action.
func (s *Store) LatestAction(ctx context.Context, signalID int64, action string) (*ActionRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if signalID <= 0 {
		return nil, fmt.Errorf("%w: positive signal id required", ErrInvalidRecord)
	}
	action = strings.ToUpper(strings.TrimSpace(action))
	if !validAction(action) {
		return nil, ErrInvalidOp
	}
	r, err := scanAction(s.db.QueryRowContext(ctx, `
SELECT id,signal_id,action,actor,note,details,created_at
FROM wheel_signal_actions
WHERE signal_id = $1 AND action = $2
ORDER BY created_at DESC, id DESC LIMIT 1`, signalID, action))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("wheelstore: latest action: %w", err)
	}
	return &r, nil
}

// HasAction reports whether the signal already carries the action (the
// Telegram yes path dedups on CONFIRM before placing an order).
func (s *Store) HasAction(ctx context.Context, signalID int64, action string) (bool, error) {
	if err := s.check(); err != nil {
		return false, err
	}
	if signalID <= 0 {
		return false, fmt.Errorf("%w: positive signal id required", ErrInvalidRecord)
	}
	if !validAction(action) {
		return false, ErrInvalidOp
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM wheel_signal_actions WHERE signal_id = $1 AND action = $2)`,
		signalID, strings.ToUpper(strings.TrimSpace(action))).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("wheelstore: has action: %w", err)
	}
	return exists, nil
}

// ClaimOrder atomically reserves the one broker order allowed for a signal.
// The row is durable and shared by Telegram and Discord. claimed=false means
// another process/channel already owns (or completed) the order.
func (s *Store) ClaimOrder(ctx context.Context, signalID int64, actor string) (claimed bool, err error) {
	if err := s.check(); err != nil {
		return false, err
	}
	actor = strings.TrimSpace(actor)
	if signalID <= 0 || actor == "" {
		return false, fmt.Errorf("%w: signal id and actor are required", ErrInvalidRecord)
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO wheel_order_claims (signal_id, actor)
VALUES ($1, $2)
ON CONFLICT (signal_id) DO NOTHING`, signalID, actor)
	if err != nil {
		return false, fmt.Errorf("wheelstore: claim order: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("wheelstore: claim order rows: %w", err)
	}
	return n == 1, nil
}

// CompleteOrderClaim stores the broker identity on the durable claim.  The
// claim remains authoritative even if the separate CONFIRM audit append fails.
func (s *Store) CompleteOrderClaim(ctx context.Context, signalID int64, orderID uint64, orderIDEx string, details map[string]any) error {
	if err := s.check(); err != nil {
		return err
	}
	if signalID <= 0 || orderID == 0 {
		return fmt.Errorf("%w: signal id and broker order id are required", ErrInvalidRecord)
	}
	d, err := validateJSONMap("order claim details", details, true)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE wheel_order_claims
SET order_id=$2, order_id_ex=$3, details=$4::jsonb, placed_at=now()
WHERE signal_id=$1`, signalID, orderID, strings.TrimSpace(orderIDEx), string(d))
	if err != nil {
		return fmt.Errorf("wheelstore: complete order claim: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("wheelstore: complete order claim rows: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("wheelstore: complete order claim: %w", ErrNotFound)
	}
	return nil
}

// HasRecentUndisposedSignal implements the scheduler's restart-safe DB
// dedupe.  Only actionable ALERT signals count as pending: HOLD/record-only
// signals (capability or strategy noise) must not block the next evaluation,
// otherwise a symbol with recurring HOLDs would never produce a new decision.
func (s *Store) HasRecentUndisposedSignal(ctx context.Context, symbol string, since time.Time) (bool, error) {
	if err := s.check(); err != nil {
		return false, err
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || since.IsZero() {
		return false, fmt.Errorf("%w: symbol and since are required", ErrInvalidRecord)
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM wheel_signals s
	WHERE s.symbol=$1 AND s.created_at >= $2 AND s.action='ALERT'
	  AND NOT EXISTS (
		SELECT 1 FROM wheel_signal_actions a
		WHERE a.signal_id=s.id AND a.action IN ('CONFIRM','NO','REJECTED')
	  )
)`, symbol, since).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("wheelstore: recent undisposed signal: %w", err)
	}
	return exists, nil
}

// QuerySignalsSince returns signals with id > afterID in id order (the
// Telegram push loop's cursor query; action filters when non-empty).
func (s *Store) QuerySignalsSince(ctx context.Context, action string, afterID int64, limit int) ([]SignalRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	conds := []string{"id > $1"}
	args := []any{afterID}
	if strings.TrimSpace(action) != "" {
		action = strings.ToUpper(action)
		if action != "ALERT" && action != "HOLD" {
			return nil, ErrInvalidAction
		}
		args = append(args, action)
		conds = append(conds, fmt.Sprintf("action = $%d", len(args)))
	}
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT id,symbol,action,config_version,capability_status,blocked_by,current_price,actual_inventory,option_delta_stock,effective_inventory,target_inventory,inventory_gap,inventory_snapshot,candidates,rejection_reasons,reason,created_at FROM wheel_signals WHERE %s ORDER BY id ASC LIMIT $%d`, strings.Join(conds, " AND "), len(args))
	rows, e := s.db.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, fmt.Errorf("wheelstore: query signals since: %w", e)
	}
	defer rows.Close()
	var out []SignalRecord
	for rows.Next() {
		r, e := scanSignal(rows)
		if e != nil {
			return nil, fmt.Errorf("wheelstore: query signals since scan: %w", e)
		}
		out = append(out, r)
	}
	if e := rows.Err(); e != nil {
		return nil, fmt.Errorf("wheelstore: query signals since rows: %w", e)
	}
	return out, nil
}

// MaxSignalID returns the newest signal id (0 when no signals exist); the
// push loop seeds its in-memory cursor from it so old signals are not pushed
// after a restart.
func (s *Store) MaxSignalID(ctx context.Context) (int64, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM wheel_signals`).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("wheelstore: max signal id: %w", err)
	}
	return id, nil
}

// Dismiss silences a symbol for one calendar day (idempotent).
func (s *Store) Dismiss(ctx context.Context, symbol string, date time.Time) error {
	if err := s.check(); err != nil {
		return err
	}
	if strings.TrimSpace(symbol) == "" || date.IsZero() {
		return fmt.Errorf("%w: symbol and date are required", ErrInvalidRecord)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO wheel_signal_dismissals (symbol, dismiss_date) VALUES ($1, $2) ON CONFLICT (symbol, dismiss_date) DO NOTHING`, strings.TrimSpace(symbol), date)
	if err != nil {
		return fmt.Errorf("wheelstore: dismiss: %w", err)
	}
	return nil
}

// IsDismissed reports whether symbol is silenced on date (UTC calendar day).
func (s *Store) IsDismissed(ctx context.Context, symbol string, date time.Time) (bool, error) {
	if err := s.check(); err != nil {
		return false, err
	}
	if strings.TrimSpace(symbol) == "" || date.IsZero() {
		return false, fmt.Errorf("%w: symbol and date are required", ErrInvalidRecord)
	}
	var dismissed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM wheel_signal_dismissals WHERE symbol = $1 AND dismiss_date = $2)`, strings.TrimSpace(symbol), date).Scan(&dismissed)
	if err != nil {
		return false, fmt.Errorf("wheelstore: is dismissed: %w", err)
	}
	return dismissed, nil
}
