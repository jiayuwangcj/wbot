package wheelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	StrategyCacheResearchCandidate = "RESEARCH_CANDIDATE"
	StrategyCacheApprovedCandidate = "APPROVED_CANDIDATE"
)

type StrategyCacheWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// StrategyCacheRecord is research evidence, not a published Wheel config.
// Payload is versioned by its schema_version field and intentionally excludes
// ES generation trajectories.
type StrategyCacheRecord struct {
	Symbol        string
	Market        string
	Currency      string
	ConfigVersion int
	Payload       map[string]any
	ModelVersion  string
	DataWindow    StrategyCacheWindow
	ApprovedState string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func validateStrategyCache(r StrategyCacheRecord) ([]byte, []byte, error) {
	if strings.TrimSpace(r.Symbol) == "" || strings.TrimSpace(r.Market) == "" || strings.TrimSpace(r.Currency) == "" || r.ConfigVersion <= 0 || strings.TrimSpace(r.ModelVersion) == "" {
		return nil, nil, fmt.Errorf("%w: strategy cache identity fields are required", ErrInvalidRecord)
	}
	if r.ApprovedState != StrategyCacheResearchCandidate && r.ApprovedState != StrategyCacheApprovedCandidate {
		return nil, nil, fmt.Errorf("%w: invalid strategy cache approved_state", ErrInvalidRecord)
	}
	if strings.TrimSpace(r.DataWindow.From) == "" || strings.TrimSpace(r.DataWindow.To) == "" {
		return nil, nil, fmt.Errorf("%w: strategy cache data window is required", ErrInvalidRecord)
	}
	payload, err := validateJSONMap("strategy cache payload", r.Payload, false)
	if err != nil {
		return nil, nil, err
	}
	if r.Payload["schema_version"] == nil {
		return nil, nil, fmt.Errorf("%w: strategy cache payload schema_version is required", ErrInvalidRecord)
	}
	if r.ApprovedState == StrategyCacheApprovedCandidate && !strategyCacheApprovalGates(r.Payload) {
		return nil, nil, fmt.Errorf("%w: APPROVED_CANDIDATE requires data, sample-out, and human approval gates", ErrInvalidRecord)
	}
	window, err := json.Marshal(r.DataWindow)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: strategy cache data window: %v", ErrInvalidRecord, err)
	}
	return payload, window, nil
}

func strategyCacheApprovalGates(payload map[string]any) bool {
	gates, ok := payload["approval_gates"].(map[string]any)
	if !ok {
		return false
	}
	data, _ := gates["data_gate_passed"].(bool)
	sampleOut, _ := gates["sample_out_passed"].(bool)
	human, _ := gates["human_approved"].(bool)
	return data && sampleOut && human
}

// ApproveStrategyCache is the explicit human-approval seam. It cannot bypass
// either automated gate and remains separate from configuration publication.
func (s *Store) ApproveStrategyCache(ctx context.Context, symbol string) error {
	r, err := s.GetStrategyCache(ctx, symbol)
	if err != nil {
		return err
	}
	gates, ok := r.Payload["approval_gates"].(map[string]any)
	if !ok {
		return fmt.Errorf("%w: strategy cache approval gates are missing", ErrInvalidRecord)
	}
	data, _ := gates["data_gate_passed"].(bool)
	sampleOut, _ := gates["sample_out_passed"].(bool)
	if !data || !sampleOut {
		return fmt.Errorf("%w: strategy cache data and sample-out gates must pass before human approval", ErrInvalidRecord)
	}
	gates["human_approved"] = true
	r.ApprovedState = StrategyCacheApprovedCandidate
	return s.UpsertStrategyCache(ctx, *r)
}

// UpsertStrategyCache overwrites the one cache row for a symbol.  The primary
// key makes repeated caching of the same report idempotent while preserving
// created_at and advancing updated_at.
func (s *Store) UpsertStrategyCache(ctx context.Context, r StrategyCacheRecord) error {
	if err := s.check(); err != nil {
		return err
	}
	payload, window, err := validateStrategyCache(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO strategy_cache
    (symbol, market, currency, config_version, payload, model_version, data_window, approved_state)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7::jsonb, $8)
ON CONFLICT (symbol) DO UPDATE SET
    market = EXCLUDED.market,
    currency = EXCLUDED.currency,
    config_version = EXCLUDED.config_version,
    payload = EXCLUDED.payload,
    model_version = EXCLUDED.model_version,
    data_window = EXCLUDED.data_window,
    approved_state = EXCLUDED.approved_state,
    updated_at = now()`, r.Symbol, r.Market, r.Currency, r.ConfigVersion, string(payload), r.ModelVersion, string(window), r.ApprovedState)
	if err != nil {
		return fmt.Errorf("wheelstore: upsert strategy cache: %w", err)
	}
	return nil
}

func (s *Store) GetStrategyCache(ctx context.Context, symbol string) (*StrategyCacheRecord, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("%w: strategy cache symbol is required", ErrInvalidRecord)
	}
	var r StrategyCacheRecord
	var payload, window []byte
	err := s.db.QueryRowContext(ctx, `
SELECT symbol, market, currency, config_version, payload, model_version,
       data_window, approved_state, created_at, updated_at
FROM strategy_cache WHERE symbol = $1`, symbol).Scan(&r.Symbol, &r.Market, &r.Currency, &r.ConfigVersion, &payload, &r.ModelVersion, &window, &r.ApprovedState, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("wheelstore: get strategy cache: %w", err)
	}
	if err := json.Unmarshal(payload, &r.Payload); err != nil {
		return nil, fmt.Errorf("wheelstore: decode strategy cache payload: %w", err)
	}
	if err := json.Unmarshal(window, &r.DataWindow); err != nil {
		return nil, fmt.Errorf("wheelstore: decode strategy cache data window: %w", err)
	}
	return &r, nil
}
