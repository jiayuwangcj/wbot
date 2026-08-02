package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/ingest"
)

const ingestPath = "/v1/ingest"

// IngestRunner runs a one-shot bar ingestion for a symbol (POST /v1/ingest).
// Backed by the same pipeline as `wbot ingest futu` (internal/ingest), so a
// request and the CLI write identical bars; the run is labeled source=http-api.
type IngestRunner interface {
	RunBars(ctx context.Context, symbol, timeframe, adjust string) error
}

type ingestRunner struct {
	db   *sql.DB
	addr string
}

// NewIngestRunner returns an IngestRunner writing into database via the
// gateway at FutuGatewayURL() (same pattern as NewFutuQuoter).
func NewIngestRunner(database *sql.DB) IngestRunner {
	return ingestRunner{db: database, addr: FutuGatewayURL()}
}

// RunBars fetches one range of bars and writes one ingestion run.
func (r ingestRunner) RunBars(ctx context.Context, symbol, timeframe, adjust string) error {
	_, ingestTF, err := futu.ParseTimeframe(timeframe)
	if err != nil {
		return err
	}
	_, adjustName, err := futu.ParseAdjust(adjust)
	if err != nil {
		return err
	}
	src := ingest.FutuSource{Client: futu.NewClient(r.addr), Adjust: adjustName}
	return ingest.RunIngestion(ctx, r.db, "http-api", domain.Symbol(symbol), ingestTF, adjustName, "futu", time.Time{}, time.Now(), src)
}

// IngestHandler serves POST /v1/ingest: body {symbol, timeframe, adjust},
// one-shot bars ingestion into the cache. Data 页「补数据」按钮的落点;
// 浏览器无法直连网关(CORS/安全),serve 代理执行并返回运行结果。
func IngestHandler(runner IngestRunner) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(ingestPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErrorBody(w, http.StatusMethodNotAllowed, errorJSON{Code: "method_not_allowed", Message: "method not allowed", Action: "use POST"})
			return
		}
		var req struct {
			Symbol    string `json:"symbol"`
			Timeframe string `json:"timeframe"`
			Adjust    string `json:"adjust"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "invalid JSON body: " + err.Error(), Action: "send {symbol, timeframe, adjust}"})
			return
		}
		symbol := strings.TrimSpace(req.Symbol)
		if symbol == "" {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "symbol is required", Action: "send {symbol, timeframe, adjust}"})
			return
		}
		timeframe := strings.TrimSpace(req.Timeframe)
		if timeframe == "" {
			timeframe = "1d"
		}
		adjust := strings.TrimSpace(req.Adjust)
		if adjust == "" {
			adjust = futu.AdjustFwd
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if err := runner.RunBars(ctx, symbol, timeframe, adjust); err != nil {
			action := "check the gateway and retry; or use `wbot ingest futu -symbol " + symbol + " -timeframe " + timeframe + "`"
			if errors.Is(err, context.DeadlineExceeded) {
				writeErrorBody(w, http.StatusGatewayTimeout, errorJSON{Code: "timeout", Message: "ingest timed out", Action: action})
				return
			}
			fmt.Fprintf(os.Stderr, "httpapi: ingest %s: %v\n", symbol, err)
			writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "ingest_failed", Message: err.Error(), Action: action})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol":    symbol,
			"timeframe": timeframe,
			"adjust":    adjust,
			"status":    "ok",
		})
	})
	return mux
}
