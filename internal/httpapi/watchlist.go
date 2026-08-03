package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/strategy"
	"github.com/jiayu/wbot/internal/watchlist"
)

const (
	strategiesPath = "/v1/strategies"
	watchlistPath  = "/v1/watchlist"
)

// WatchlistStore is the watchlist surface the watchlist endpoints need.
type WatchlistStore interface {
	List(ctx context.Context) ([]watchlist.Item, error)
	Upsert(ctx context.Context, symbol, strategy string, params map[string]any) (watchlist.Item, error)
	Delete(ctx context.Context, symbol string) (bool, error)
}

// NewDBWatchlistStore returns a WatchlistStore backed by PostgreSQL via internal/watchlist.
func NewDBWatchlistStore(db *sql.DB) WatchlistStore {
	return watchlistStore{db: db}
}

type watchlistStore struct {
	db *sql.DB
}

func (s watchlistStore) List(ctx context.Context) ([]watchlist.Item, error) {
	return watchlist.List(ctx, s.db)
}

func (s watchlistStore) Upsert(ctx context.Context, symbol, strategy string, params map[string]any) (watchlist.Item, error) {
	return watchlist.Upsert(ctx, s.db, symbol, strategy, params)
}

func (s watchlistStore) Delete(ctx context.Context, symbol string) (bool, error) {
	return watchlist.Delete(ctx, s.db, symbol)
}

// watchlistItemJSON is the API shape of one watchlist row (times RFC3339).
type watchlistItemJSON struct {
	Symbol    string         `json:"symbol"`
	Strategy  string         `json:"strategy"`
	Params    map[string]any `json:"params"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

func toWatchlistItemJSON(it watchlist.Item) watchlistItemJSON {
	if it.Params == nil {
		it.Params = map[string]any{}
	}
	return watchlistItemJSON{
		Symbol:    it.Symbol,
		Strategy:  it.Strategy,
		Params:    it.Params,
		CreatedAt: it.CreatedAt.Format(time.RFC3339),
		UpdatedAt: it.UpdatedAt.Format(time.RFC3339),
	}
}

// WatchlistHandler serves GET /v1/strategies and GET /v1/watchlist with
// PUT/DELETE /v1/watchlist/{symbol}; params are validated against the template
// schema (unknown strategy/parameter or type mismatch → 400).
func WatchlistHandler(store WatchlistStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(strategiesPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// buy-hold 是引擎一等策略(backtestexec 直接支持,无 params);
		// watchlist 作为「回测计划列表」收录它,使 from_watchlist 回测
		// 模式在无期权数据的环境(本地 mock)也可整表跑通。
		tmpls := append([]strategy.ContractTemplate{
			{Name: "buy-hold", Description: "买入持有：不调仓"},
		}, strategy.ContractTemplates()...)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tmpls)
	})

	mux.HandleFunc(watchlistPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		items, err := store.List(r.Context())
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: watchlist: list: %v\n", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out := make([]watchlistItemJSON, 0, len(items))
		for _, it := range items {
			out = append(out, toWatchlistItemJSON(it))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc(watchlistPath+"/{symbol}", func(w http.ResponseWriter, r *http.Request) {
		symbol := strings.TrimSpace(r.PathValue("symbol"))
		switch r.Method {
		case http.MethodPut:
			if symbol == "" {
				writeError(w, http.StatusBadRequest, "missing symbol")
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			var req struct {
				Strategy string         `json:"strategy"`
				Params   map[string]any `json:"params"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
			req.Strategy = strings.TrimSpace(req.Strategy)
			if req.Strategy == "" {
				writeError(w, http.StatusBadRequest, "missing strategy")
				return
			}
			if req.Params == nil {
				req.Params = map[string]any{}
			}
			if err := watchlist.Validate(req.Strategy, req.Params); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			it, err := store.Upsert(r.Context(), symbol, req.Strategy, req.Params)
			if err != nil {
				fmt.Fprintf(os.Stderr, "httpapi: watchlist: upsert %s: %v\n", symbol, err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(toWatchlistItemJSON(it))
		case http.MethodDelete:
			if symbol == "" {
				writeError(w, http.StatusBadRequest, "missing symbol")
				return
			}
			found, err := store.Delete(r.Context(), symbol)
			if err != nil {
				fmt.Fprintf(os.Stderr, "httpapi: watchlist: delete %s: %v\n", symbol, err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if !found {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"symbol": symbol, "deleted": true})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	// Any other path under the watchlist namespace: JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return mux
}
