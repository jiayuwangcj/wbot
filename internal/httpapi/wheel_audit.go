package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/wheelstore"
)

// WheelAuditStore is deliberately limited to read operations.  It is the
// only store surface used by the Wheel audit API, so handlers and tests cannot
// accidentally gain access to config, signal, or operator-action writes.
type WheelAuditStore interface {
	ListWheelConfigs(context.Context, string, int) ([]wheelstore.ConfigRecord, error)
	ListWheelSignals(context.Context, string, string, int) ([]wheelstore.SignalRecord, error)
	ListWheelSignalActions(context.Context, int64) ([]wheelstore.ActionRecord, error)
}

// wheelConfigJSON, wheelSignalJSON, and wheelActionJSON are API DTOs.  The
// explicit conversion keeps timestamps stable (RFC3339) and ensures optional
// collection fields are encoded as [] rather than null.
type wheelConfigJSON struct {
	ID        int64          `json:"id"`
	Symbol    string         `json:"symbol"`
	Version   int            `json:"version"`
	Config    map[string]any `json:"config"`
	State     map[string]any `json:"state"`
	CreatedAt string         `json:"created_at"`
}

type wheelInventoryJSON struct {
	CurrentPrice       *float64 `json:"current_price,omitempty"`
	ActualInventory    *float64 `json:"actual_inventory,omitempty"`
	OptionDeltaStock   *float64 `json:"option_delta_stock,omitempty"`
	EffectiveInventory *float64 `json:"effective_inventory,omitempty"`
	TargetInventory    *float64 `json:"target_inventory,omitempty"`
	InventoryGap       *float64 `json:"inventory_gap,omitempty"`
}

type wheelSignalJSON struct {
	ID               int64              `json:"id"`
	Symbol           string             `json:"symbol"`
	Action           string             `json:"action"`
	ConfigVersion    int                `json:"config_version"`
	CapabilityStatus string             `json:"capability_status"`
	BlockedBy        []string           `json:"blocked_by"`
	Inventory        wheelInventoryJSON `json:"inventory"`
	Candidates       []map[string]any   `json:"candidates"`
	RejectionReasons []string           `json:"rejection_reasons"`
	Reason           string             `json:"reason"`
	CreatedAt        string             `json:"created_at"`
}

type wheelActionJSON struct {
	ID        int64          `json:"id"`
	SignalID  int64          `json:"signal_id"`
	Action    string         `json:"action"`
	Actor     string         `json:"actor"`
	Note      string         `json:"note"`
	Details   map[string]any `json:"details"`
	CreatedAt string         `json:"created_at"`
}

func timeRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func toWheelConfigJSON(r wheelstore.ConfigRecord) wheelConfigJSON {
	if r.Config == nil {
		r.Config = map[string]any{}
	}
	if r.State == nil {
		r.State = map[string]any{}
	}
	return wheelConfigJSON{ID: r.ID, Symbol: r.Symbol, Version: r.Version, Config: r.Config, State: r.State, CreatedAt: timeRFC3339(r.CreatedAt)}
}

func toWheelSignalJSON(r wheelstore.SignalRecord) wheelSignalJSON {
	blocked := r.BlockedBy
	if blocked == nil {
		blocked = []string{}
	}
	candidates := r.Candidates
	if candidates == nil {
		candidates = []map[string]any{}
	}
	rejections := r.RejectionReasons
	if rejections == nil {
		rejections = []string{}
	}
	return wheelSignalJSON{
		ID: r.ID, Symbol: r.Symbol, Action: r.Action, ConfigVersion: r.ConfigVersion,
		CapabilityStatus: r.CapabilityStatus, BlockedBy: blocked,
		Inventory:  wheelInventoryJSON{CurrentPrice: r.Inventory.CurrentPrice, ActualInventory: r.Inventory.ActualInventory, OptionDeltaStock: r.Inventory.OptionDeltaStock, EffectiveInventory: r.Inventory.EffectiveInventory, TargetInventory: r.Inventory.TargetInventory, InventoryGap: r.Inventory.InventoryGap},
		Candidates: candidates, RejectionReasons: rejections, Reason: r.Reason, CreatedAt: timeRFC3339(r.CreatedAt),
	}
}

func toWheelActionJSON(r wheelstore.ActionRecord) wheelActionJSON {
	if r.Details == nil {
		r.Details = map[string]any{}
	}
	return wheelActionJSON{ID: r.ID, SignalID: r.SignalID, Action: r.Action, Actor: r.Actor, Note: r.Note, Details: r.Details, CreatedAt: timeRFC3339(r.CreatedAt)}
}

func writeWheelJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func wheelAuditBadRequest(w http.ResponseWriter, message string) {
	writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: message, Action: "check the Wheel audit query or signal id and retry"})
}

// WheelAuditHandler serves only GET endpoints:
//   - GET /v1/wheel/configs?symbol=...&limit=...
//   - GET /v1/wheel/signals?symbol=...&action=ALERT|HOLD&limit=...
//   - GET /v1/wheel/signals/{id}/actions
//
// It never calls a write-capable wheelstore method. A nil store is retained as
// a valid handler for deployments/tests that do not configure the audit
// capability; those requests receive a structured 500 response.
func WheelAuditHandler(store WheelAuditStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/wheel/configs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		q := r.URL.Query()
		limit := 100
		if raw := q.Get("limit"); raw != "" {
			var err error
			limit, err = parseLimit(raw)
			if err != nil {
				wheelAuditBadRequest(w, err.Error())
				return
			}
		}
		if store == nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		configs, err := store.ListWheelConfigs(r.Context(), strings.TrimSpace(q.Get("symbol")), limit)
		if err != nil {
			writeWheelAuditStoreError(w, err)
			return
		}
		out := make([]wheelConfigJSON, 0, len(configs))
		for _, config := range configs {
			out = append(out, toWheelConfigJSON(config))
		}
		writeWheelJSON(w, out)
	})

	mux.HandleFunc("/v1/wheel/signals", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		q := r.URL.Query()
		limit := 100
		if raw := q.Get("limit"); raw != "" {
			var err error
			limit, err = parseLimit(raw)
			if err != nil {
				wheelAuditBadRequest(w, err.Error())
				return
			}
		}
		action := strings.ToUpper(strings.TrimSpace(q.Get("action")))
		if action != "" && action != "ALERT" && action != "HOLD" {
			wheelAuditBadRequest(w, fmt.Sprintf("invalid action %q; want ALERT or HOLD", q.Get("action")))
			return
		}
		if store == nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		signals, err := store.ListWheelSignals(r.Context(), strings.TrimSpace(q.Get("symbol")), action, limit)
		if err != nil {
			writeWheelAuditStoreError(w, err)
			return
		}
		out := make([]wheelSignalJSON, 0, len(signals))
		for _, signal := range signals {
			out = append(out, toWheelSignalJSON(signal))
		}
		writeWheelJSON(w, out)
	})

	mux.HandleFunc("/v1/wheel/signals/{id}/actions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		rawID := r.PathValue("id")
		signalID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || signalID <= 0 {
			wheelAuditBadRequest(w, fmt.Sprintf("invalid signal id %q; want a positive integer", rawID))
			return
		}
		if store == nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		actions, err := store.ListWheelSignalActions(r.Context(), signalID)
		if err != nil {
			writeWheelAuditStoreError(w, err)
			return
		}
		out := make([]wheelActionJSON, 0, len(actions))
		for _, action := range actions {
			out = append(out, toWheelActionJSON(action))
		}
		writeWheelJSON(w, out)
	})

	// Keep unknown Wheel paths and trailing-slash variants inside the API's
	// standard JSON error contract instead of falling through to the UI.
	mux.HandleFunc("/v1/wheel/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}

func writeWheelAuditStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, wheelstore.ErrInvalidAction) || errors.Is(err, wheelstore.ErrInvalidRecord) {
		wheelAuditBadRequest(w, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}

// The db-backed general API store also implements the narrow Wheel audit
// interface. Keeping the adapter here avoids widening Store or changing the
// serveMux constructor used by existing callers.
func (s dbStore) ListWheelConfigs(ctx context.Context, symbol string, limit int) ([]wheelstore.ConfigRecord, error) {
	return wheelstore.New(s.db).ListConfigs(ctx, symbol, limit)
}

func (s dbStore) ListWheelSignals(ctx context.Context, symbol, action string, limit int) ([]wheelstore.SignalRecord, error) {
	return wheelstore.New(s.db).ListSignals(ctx, symbol, action, limit)
}

func (s dbStore) ListWheelSignalActions(ctx context.Context, signalID int64) ([]wheelstore.ActionRecord, error) {
	return wheelstore.New(s.db).ListActions(ctx, signalID)
}
