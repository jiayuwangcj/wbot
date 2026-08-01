package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/backtestexec"
)

// BacktestExecutor executes and persists one backtest run (POST /v1/backtests).
type BacktestExecutor interface {
	RunOne(ctx context.Context, symbol, strategy string, params map[string]any) (*backtest.ResultRecord, error)
}

// NewDBBacktestExecutor returns a BacktestExecutor backed by PostgreSQL: runs
// via internal/backtestexec (the same path as `wbot backtest -dsn`) and
// persists with SaveResult, returning the full stored record.
func NewDBBacktestExecutor(db *sql.DB) BacktestExecutor {
	return backtestExecutor{db: db}
}

type backtestExecutor struct {
	db *sql.DB
}

// RunOne mirrors `wbot backtest -dsn ... -save` with the API's documented
// defaults (timeframe 1d, adjust fwd, cash 10000, fee 0, limit 10000);
// doc/API.md. A canceled context aborts before persisting anything.
func (e backtestExecutor) RunOne(ctx context.Context, symbol, strategy string, params map[string]any) (*backtest.ResultRecord, error) {
	o := backtestexec.Options{
		Symbol:    symbol,
		Strategy:  strategy,
		Params:    params,
		Timeframe: "1d",
		Adjust:    "fwd",
		Limit:     10000,
		Cash:      10000,
		Fee:       0,
	}
	outcome, err := backtestexec.Run(ctx, e.db, o)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := backtest.SaveResult(ctx, e.db, strategy, symbol, backtestexec.SaveParams(o), outcome.Result, outcome.StartTs, outcome.EndTs)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "httpapi: backtests: exec %s saved id=%d\n", symbol, id)
	return backtest.LoadResult(ctx, e.db, id)
}

// executeRequest is the POST /v1/backtests body (draft 2026-08-02 S4).
type executeRequest struct {
	Symbol        string         `json:"symbol"`
	Strategy      string         `json:"strategy"`
	Params        map[string]any `json:"params"`
	FromWatchlist bool           `json:"from_watchlist"`
}

// backtestExecuteHandler owns the single-process run mutex (busy → 409) and
// the execution timeout (default 5 minutes, draft 2026-08-02 S4).
type backtestExecuteHandler struct {
	exec    BacktestExecutor
	wstore  WatchlistStore
	timeout time.Duration
	mu      sync.Mutex
}

// BacktestExecuteHandler serves POST /v1/backtests: one manual run
// ({symbol, strategy, params}) or the whole watchlist ({from_watchlist: true},
// serial, one saved row per entry). Execution is synchronous — the response is
// the created result detail (201) or an error body (422/409/503).
func BacktestExecuteHandler(exec BacktestExecutor, wstore WatchlistStore) http.Handler {
	return newBacktestExecuteHandler(exec, wstore, 5*time.Minute)
}

// newBacktestExecuteHandler builds the execute handler with an explicit
// timeout (tests use short deadlines).
func newBacktestExecuteHandler(exec BacktestExecutor, wstore WatchlistStore, timeout time.Duration) http.Handler {
	h := &backtestExecuteHandler{exec: exec, wstore: wstore, timeout: timeout}
	mux := http.NewServeMux()
	mux.HandleFunc(backtestsPath, h.handle)
	// Any other path under the backtests namespace: JSON 404 (shape of S1).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeErrorBody(w, http.StatusNotFound, errorJSON{Code: "not_found", Message: "not found", Action: ""})
	})
	return mux
}

func (h *backtestExecuteHandler) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorBody(w, http.StatusMethodNotAllowed, errorJSON{Code: "method_not_allowed", Message: "method not allowed", Action: "use POST"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorBody(w, http.StatusUnprocessableEntity, errorJSON{Code: "invalid_request", Message: "invalid request body", Action: "send a JSON body and retry"})
		return
	}
	var req executeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeErrorBody(w, http.StatusUnprocessableEntity, errorJSON{Code: "invalid_request", Message: "invalid JSON body", Action: "send a JSON body and retry"})
		return
	}
	if req.FromWatchlist {
		h.execWatchlist(w, r, req)
		return
	}
	h.execOne(w, r, req)
}

// execOne runs one manual backtest: validation (422) before the mutex, so an
// invalid request never bumps a running backtest out.
func (h *backtestExecuteHandler) execOne(w http.ResponseWriter, r *http.Request, req executeRequest) {
	symbol := strings.TrimSpace(req.Symbol)
	strategyName := strings.TrimSpace(req.Strategy)
	if symbol == "" {
		writeErrorBody(w, http.StatusUnprocessableEntity, errorJSON{Code: "invalid_request", Message: "missing symbol", Action: `add "symbol": "HK.00700" to the body`})
		return
	}
	if strategyName == "" {
		writeErrorBody(w, http.StatusUnprocessableEntity, errorJSON{Code: "invalid_request", Message: "missing strategy", Action: `add "strategy": "covered-call" to the body`})
		return
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	if _, _, err := backtestexec.Build(strategyName, req.Params); err != nil {
		writeErrorBody(w, http.StatusUnprocessableEntity, errorJSON{Code: "invalid_request", Message: err.Error(), Action: "check the strategy name and params against GET /v1/strategies"})
		return
	}
	if !h.mu.TryLock() {
		writeErrorBody(w, http.StatusConflict, errorJSON{Code: "busy", Message: "a backtest is already running", Action: "wait for it to finish, then retry"})
		return
	}
	defer h.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	rec, err := h.exec.RunOne(ctx, symbol, strategyName, req.Params)
	if err != nil {
		h.writeExecError(w, ctx, err, symbol)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toBacktestDetail(*rec))
}

// execWatchlist runs every watchlist row serially (symbol order) and saves each;
// the first failing row aborts the batch with its error (runs already saved
// stay saved). One run at a time: the mutex covers the whole batch.
func (h *backtestExecuteHandler) execWatchlist(w http.ResponseWriter, r *http.Request, req executeRequest) {
	if req.Symbol != "" || req.Strategy != "" || req.Params != nil {
		writeErrorBody(w, http.StatusUnprocessableEntity, errorJSON{Code: "invalid_request", Message: "from_watchlist is mutually exclusive with symbol/strategy/params", Action: `send either {"from_watchlist": true} or {symbol, strategy, params}`})
		return
	}
	items, err := h.wstore.List(r.Context())
	if err != nil {
		fmt.Fprintf(os.Stderr, "httpapi: backtests: exec: watchlist: %v\n", err)
		writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "dependency_failed", Message: "watchlist unavailable", Action: "check server logs and retry"})
		return
	}
	if len(items) == 0 {
		writeErrorBody(w, http.StatusUnprocessableEntity, errorJSON{Code: "empty_watchlist", Message: "watchlist is empty; nothing to run", Action: "add entries via PUT /v1/watchlist/{symbol} first"})
		return
	}
	if !h.mu.TryLock() {
		writeErrorBody(w, http.StatusConflict, errorJSON{Code: "busy", Message: "a backtest is already running", Action: "wait for it to finish, then retry"})
		return
	}
	defer h.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	runs := make([]backtestDetailJSON, 0, len(items))
	for _, it := range items {
		rec, err := h.exec.RunOne(ctx, it.Symbol, it.Strategy, it.Params)
		if err != nil {
			h.writeExecError(w, ctx, err, it.Symbol)
			return
		}
		runs = append(runs, toBacktestDetail(*rec))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"runs": runs})
}

// writeExecError maps a run failure to its API error: 503 no_data when the
// input data is missing, 503 timeout when the execution deadline hit,
// otherwise 503 dependency_failed (draft 2026-08-02 S4).
func (h *backtestExecuteHandler) writeExecError(w http.ResponseWriter, ctx context.Context, err error, symbol string) {
	fmt.Fprintf(os.Stderr, "httpapi: backtests: exec %s: %v\n", symbol, err)
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "timeout", Message: "the run exceeded the execution timeout", Action: "retry later or with a narrower bar range"})
	case errors.Is(err, backtestexec.ErrNoBars):
		writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "no_data", Message: fmt.Sprintf("no bars data for %s", symbol), Action: fmt.Sprintf("ingest first: `wbot ingest futu -symbol %s -timeframe 1d`", symbol)})
	case errors.Is(err, backtestexec.ErrNoOptionData):
		writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "no_data", Message: fmt.Sprintf("no option quote data for %s", symbol), Action: fmt.Sprintf("ingest first: `wbot ingest futu-option -symbol %s`", symbol)})
	default:
		writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "dependency_failed", Message: "the backtest run failed", Action: "check server logs and retry"})
	}
}
