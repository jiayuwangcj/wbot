package httpapi

// DB-backed account-snapshot series: GET /v1/account/snapshots serves the
// points written by `wbot ingest account` (asset curve, 资产曲线) for the
// browser. Purely DB-local — no gateway, no credentials (PRIVACY).

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jiayu/wbot/internal/ingest"
)

const accountSnapshotsPath = "/v1/account/snapshots"

// AccountSnapshotLister is the narrow surface the endpoint needs; implemented
// by httpapi.dbStore (PostgreSQL via internal/ingest).
type AccountSnapshotLister interface {
	AccountSnapshots(ctx context.Context, env string, limit int) ([]ingest.AccountSnapshotRow, error)
}

// accountSnapshotPointJSON is one point of the equity curve.
type accountSnapshotPointJSON struct {
	CapturedAt  string  `json:"captured_at"` // RFC3339 UTC
	TotalAssets float64 `json:"total_assets"`
	Cash        float64 `json:"cash"`
	MarketVal   float64 `json:"market_val"`
}

// accountSnapshotsJSON is the GET /v1/account/snapshots success body.
type accountSnapshotsJSON struct {
	Env    string                     `json:"env"` // canonical EnvName ("simulate"|"real")
	Limit  int                        `json:"limit"`
	Points []accountSnapshotPointJSON `json:"points"` // chronological, oldest first
}

// AccountSnapshotsHandler serves GET /v1/account/snapshots?env=sim&limit=120:
// the latest `limit` funds snapshots for the env, oldest first (the 资产曲线).
func AccountSnapshotsHandler(lister AccountSnapshotLister) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(accountSnapshotsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		env := "simulate" // 默认模拟盘,与 /v1/futu/account 同语义
		if s := strings.TrimSpace(r.URL.Query().Get("env")); s != "" {
			switch strings.ToLower(s) {
			case "sim", "simulate", "paper":
			case "real":
				env = "real"
			default:
				writeError(w, http.StatusBadRequest, "invalid env (want sim or real)")
				return
			}
		}
		limit := 120
		if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 || n > 10000 {
				writeError(w, http.StatusBadRequest, "invalid limit (want 1..10000)")
				return
			}
			limit = n
		}
		rows, err := lister.AccountSnapshots(r.Context(), env, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query account snapshots: "+err.Error())
			return
		}
		out := accountSnapshotsJSON{
			Env:    env,
			Limit:  limit,
			Points: make([]accountSnapshotPointJSON, 0, len(rows)),
		}
		for _, row := range rows {
			out.Points = append(out.Points, accountSnapshotPointJSON{
				CapturedAt:  row.CapturedAt.Format("2006-01-02T15:04:05Z07:00"),
				TotalAssets: row.TotalAssets,
				Cash:        row.Cash,
				MarketVal:   row.MarketVal,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}
