package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
)

const backtestsPath = "/v1/backtests"

// BacktestStore is the backtest results surface the /v1/backtests endpoints
// need (independent of Store: list summaries + one run's full trace).
type BacktestStore interface {
	List(ctx context.Context, symbol, strategy string, limit int) ([]backtest.ResultRecord, error)
	Get(ctx context.Context, id int64) (*backtest.ResultRecord, error)
}

// NewDBBacktestStore returns a BacktestStore backed by PostgreSQL via internal/backtest.
func NewDBBacktestStore(db *sql.DB) BacktestStore {
	return backtestStore{db: db}
}

type backtestStore struct {
	db *sql.DB
}

func (s backtestStore) List(ctx context.Context, symbol, strategy string, limit int) ([]backtest.ResultRecord, error) {
	return backtest.ListResults(ctx, s.db, symbol, strategy, limit)
}

func (s backtestStore) Get(ctx context.Context, id int64) (*backtest.ResultRecord, error) {
	return backtest.LoadResult(ctx, s.db, id)
}

// backtestSummaryJSON is one run's list shape: summary metrics, no curve.
type backtestSummaryJSON struct {
	ID        int64          `json:"id"`
	Strategy  string         `json:"strategy"`
	Symbol    string         `json:"symbol"`
	Params    map[string]any `json:"params"`
	Metrics   map[string]any `json:"metrics"`
	StartTs   string         `json:"start_ts"`
	EndTs     string         `json:"end_ts"`
	CreatedAt string         `json:"created_at"`
}

// backtestDetailJSON adds the full trace: per-bar equity curve + trade events.
type backtestDetailJSON struct {
	backtestSummaryJSON
	EquityCurve []backtest.EquityPoint `json:"equity_curve"`
	Trades      []backtest.Trade       `json:"trades"`
}

func toBacktestSummary(r backtest.ResultRecord) backtestSummaryJSON {
	if r.Params == nil {
		r.Params = map[string]any{}
	}
	if r.Metrics == nil {
		r.Metrics = map[string]any{}
	}
	return backtestSummaryJSON{
		ID: r.ID, Strategy: r.Strategy, Symbol: r.Symbol,
		Params: r.Params, Metrics: r.Metrics,
		StartTs: r.StartTs.Format(time.RFC3339), EndTs: r.EndTs.Format(time.RFC3339),
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

// toBacktestDetail renders the detail shape; pre-004 rows without a stored
// trace come back as empty arrays.
func toBacktestDetail(r backtest.ResultRecord) backtestDetailJSON {
	d := backtestDetailJSON{backtestSummaryJSON: toBacktestSummary(r), EquityCurve: r.EquityCurve, Trades: r.Trades}
	if d.EquityCurve == nil {
		d.EquityCurve = []backtest.EquityPoint{}
	}
	if d.Trades == nil {
		d.Trades = []backtest.Trade{}
	}
	return d
}

// errorJSON is the backtest endpoints' error body: {code, message, action}
// (new convention from draft 2026-08-02 S1; S5 adopts it across the API).
type errorJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Action  string `json:"action"`
}

func writeErrorBody(w http.ResponseWriter, status int, e errorJSON) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

// BacktestsHandler serves GET /v1/backtests (list, filters symbol/strategy/
// limit, newest first) and GET /v1/backtests/{id} (detail with equity_curve
// and trades; 404 when the id has no row).
func BacktestsHandler(store BacktestStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(backtestsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErrorBody(w, http.StatusMethodNotAllowed, errorJSON{Code: "method_not_allowed", Message: "method not allowed", Action: "use GET"})
			return
		}
		q := r.URL.Query()
		limit := 50
		if s := q.Get("limit"); s != "" {
			n, err := parseLimit(s)
			if err != nil {
				writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: err.Error(), Action: "check the limit value and retry"})
				return
			}
			limit = n
		}
		recs, err := store.List(r.Context(), strings.TrimSpace(q.Get("symbol")), strings.TrimSpace(q.Get("strategy")), limit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: backtests: list: %v\n", err)
			writeErrorBody(w, http.StatusInternalServerError, errorJSON{Code: "internal_error", Message: "internal error", Action: "check server logs and retry"})
			return
		}
		out := make([]backtestSummaryJSON, 0, len(recs))
		for _, rec := range recs {
			out = append(out, toBacktestSummary(rec))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc(backtestsPath+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErrorBody(w, http.StatusMethodNotAllowed, errorJSON{Code: "method_not_allowed", Message: "method not allowed", Action: "use GET"})
			return
		}
		rawID := r.PathValue("id")
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || id <= 0 {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: fmt.Sprintf("invalid id %q; want a positive integer", rawID), Action: "check the id and retry"})
			return
		}
		rec, err := store.Get(r.Context(), id)
		if errors.Is(err, backtest.ErrResultNotFound) {
			writeErrorBody(w, http.StatusNotFound, errorJSON{Code: "not_found", Message: fmt.Sprintf("backtest result %d not found", id), Action: "run `wbot backtest -save` to persist a run first"})
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: backtests: get %d: %v\n", id, err)
			writeErrorBody(w, http.StatusInternalServerError, errorJSON{Code: "internal_error", Message: "internal error", Action: "check server logs and retry"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(toBacktestDetail(*rec))
	})

	// Any other path under the backtests namespace: JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeErrorBody(w, http.StatusNotFound, errorJSON{Code: "not_found", Message: "not found", Action: ""})
	})

	return mux
}
