// Package httpapi serves the WeChat miniapp's read-only data API: /v1/bars, /v1/runs, /v1/health.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

const (
	barsPath   = "/v1/bars"
	runsPath   = "/v1/runs"
	healthPath = "/v1/health"
)

// Store is the read-only data surface the API serves.
type Store interface {
	QueryBars(ctx context.Context, symbol string, timeframe string, from, to time.Time, limit int) ([]ingest.Bar, error)
	RecentRuns(ctx context.Context, limit int) ([]ingest.RunStatus, error)
	Ping(ctx context.Context) error
}

// NewDBStore returns a Store backed by PostgreSQL via internal/ingest queries.
func NewDBStore(db *sql.DB) Store {
	return dbStore{db: db}
}

type dbStore struct {
	db *sql.DB
}

func (s dbStore) QueryBars(ctx context.Context, symbol string, timeframe string, from, to time.Time, limit int) ([]ingest.Bar, error) {
	return ingest.QueryBars(ctx, s.db, symbol, timeframe, from, to, limit)
}

func (s dbStore) RecentRuns(ctx context.Context, limit int) ([]ingest.RunStatus, error) {
	return ingest.RecentRuns(ctx, s.db, limit)
}

func (s dbStore) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

// barJSON mirrors the `ingest bars -json` output shape (ts RFC3339).
type barJSON struct {
	Ts     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

type runJSON struct {
	ID         int64   `json:"id"`
	Source     string  `json:"source"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"` // null while the run is still running
}

// healthJSON is the GET /v1/health success body.
type healthJSON struct {
	Status string `json:"status"`
}

// Handler returns an http.Handler serving GET /v1/bars, /v1/runs, and /v1/health.
func Handler(store Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(barsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		q := r.URL.Query()
		symbol := strings.TrimSpace(q.Get("symbol"))
		if symbol == "" {
			writeError(w, http.StatusBadRequest, "missing query parameter: symbol")
			return
		}
		timeframe := strings.TrimSpace(q.Get("timeframe"))
		if timeframe == "" {
			writeError(w, http.StatusBadRequest, "missing query parameter: timeframe")
			return
		}
		from, err := parseTime("from", q.Get("from"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		to, err := parseTime("to", q.Get("to"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		limit := 100
		if s := q.Get("limit"); s != "" {
			limit, err = parseLimit(s)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		bars, err := store.QueryBars(r.Context(), symbol, timeframe, from, to, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out := make([]barJSON, 0, len(bars))
		for _, b := range bars {
			out = append(out, barJSON{b.Ts.Format(time.RFC3339), b.Open, b.High, b.Low, b.Close, b.Volume})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc(runsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		limit := 10
		if s := r.URL.Query().Get("limit"); s != "" {
			var err error
			limit, err = parseLimit(s)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		runs, err := store.RecentRuns(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out := make([]runJSON, 0, len(runs))
		for _, r := range runs {
			run := runJSON{ID: r.ID, Source: r.Source, Status: r.Status, StartedAt: r.StartedAt.Format(time.RFC3339)}
			if r.FinishedAt != nil {
				finished := r.FinishedAt.Format(time.RFC3339)
				run.FinishedAt = &finished
			}
			out = append(out, run)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc(healthPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := store.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthJSON{Status: "ok"})
	})

	// Any other path: JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return mux
}

// parseTime parses a from/to query value. Empty means unbounded (zero time).
func parseTime(name, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: want RFC3339 time, got %q", name, s)
	}
	return t, nil
}

func parseLimit(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid limit: got %q, want positive integer", s)
	}
	return n, nil
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
