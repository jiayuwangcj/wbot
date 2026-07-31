package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	adminStatusPath = "/v1/admin/status"
	pingTimeout     = 3 * time.Second
)

// Pinger is the DB liveness surface the admin status endpoint needs.
// Matches the Store.Ping signature introduced with GET /v1/health.
// Implementations must respect ctx and return promptly once it is done.
type Pinger interface {
	Ping(ctx context.Context) error
}

// PingerFunc adapts a Ping-style function (e.g. *sql.DB.PingContext) to Pinger.
type PingerFunc func(ctx context.Context) error

func (f PingerFunc) Ping(ctx context.Context) error { return f(ctx) }

// ProcessMeta is the process-level status serve injects at startup.
type ProcessMeta struct {
	Version    string
	StartedAt  time.Time
	ListenAddr string
}

// statusJSON is the GET /v1/admin/status response body.
type statusJSON struct {
	Version       string       `json:"version"`
	PID           int          `json:"pid"`
	StartedAt     string       `json:"started_at"`
	UptimeSeconds float64      `json:"uptime_seconds"`
	ListenAddr    string       `json:"listen_addr"`
	DB            dbStatusJSON `json:"db"`
}

type dbStatusJSON struct {
	OK        bool    `json:"ok"`
	LatencyMS float64 `json:"latency_ms,omitempty"` // omitted while DB is down
}

// AdminHandler returns an http.Handler serving GET /v1/admin/status.
// meta is injected by serve; pinger reports DB liveness (down → 200 + ok:false).
func AdminHandler(meta ProcessMeta, pinger Pinger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(adminStatusPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		out := statusJSON{
			Version:       meta.Version,
			PID:           os.Getpid(),
			StartedAt:     meta.StartedAt.Format(time.RFC3339),
			UptimeSeconds: time.Since(meta.StartedAt).Seconds(),
			ListenAddr:    meta.ListenAddr,
			DB:            dbStatusJSON{OK: true},
		}
		pctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()
		start := time.Now()
		err := pinger.Ping(pctx)
		if err == nil && pctx.Err() == nil {
			out.DB.LatencyMS = time.Since(start).Seconds() * 1000
		} else {
			out.DB = dbStatusJSON{OK: false}
			if err == nil {
				err = pctx.Err()
			}
			fmt.Fprintf(os.Stderr, "httpapi: admin: db ping failed: %v\n", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	// Any other path under /v1/admin/: JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return mux
}
