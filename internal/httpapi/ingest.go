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
// RunOptions pulls the underlying's option-chain K-lines (same pipeline as
// `wbot ingest futu-option`, doc/FUTU.md §10).
type IngestRunner interface {
	RunBars(ctx context.Context, symbol, timeframe, adjust string) error
	RunOptions(ctx context.Context, underlying, adjust string) error
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

// RunOptions pulls the underlying's nearest-expiry option chain (7-day daily
// window, same defaults as `wbot ingest futu-option`); ON CONFLICT keeps the
// pull idempotent for rows already in option_quotes.
func (r ingestRunner) RunOptions(ctx context.Context, underlying, adjust string) error {
	_, adjustName, err := futu.ParseAdjust(adjust)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = ingest.RunOptionIngestion(ctx, r.db, futu.NewClient(r.addr), underlying, adjustName, now.AddDate(0, 0, -7), now.Add(24*time.Hour), 1)
	return err
}

// IngestHandler serves POST /v1/ingest: body {symbol, timeframe, adjust,
// kind}. kind=bars (default) runs one-shot bars ingestion; kind=option pulls
// the symbol's option chain (timeframe ignored). Data 页「补数据」/「拉取期权链」
// 按钮的落点;浏览器无法直连网关(CORS/安全),serve 代理执行并返回运行结果。
func IngestHandler(runner IngestRunner) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(ingestPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErrorBody(w, http.StatusMethodNotAllowed, errorJSON{Code: "method_not_allowed", Message: "method not allowed", Action: "use POST"})
			return
		}
		var req struct {
			Kind      string `json:"kind"`
			Symbol    string `json:"symbol"`
			Timeframe string `json:"timeframe"`
			Adjust    string `json:"adjust"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "invalid JSON body: " + err.Error(), Action: "send {symbol, timeframe, adjust, kind}"})
			return
		}
		symbol := strings.TrimSpace(req.Symbol)
		if symbol == "" {
			writeErrorBody(w, http.StatusBadRequest, errorJSON{Code: "invalid_request", Message: "symbol is required", Action: "send {symbol, timeframe, adjust, kind}"})
			return
		}
		adjust := strings.TrimSpace(req.Adjust)
		if adjust == "" {
			adjust = futu.AdjustFwd
		}
		kind := strings.TrimSpace(req.Kind)
		if kind == "" {
			kind = "bars"
		}
		timeout := 2 * time.Minute
		if kind == "option" {
			// Option chain: one serial K-line request per contract under the
			// gateway snapshot rate limit; a 60-contract expiry can take ~9min
			// (实测 2026-08-03). 15min is a service-side guard; the CLI has none.
			timeout = 15 * time.Minute
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if kind == "option" {
			if err := runner.RunOptions(ctx, symbol, adjust); err != nil {
				action := "check the gateway and retry; or use `wbot ingest futu-option -symbol " + symbol + "`"
				if errors.Is(err, context.DeadlineExceeded) {
					writeErrorBody(w, http.StatusGatewayTimeout, errorJSON{Code: "timeout", Message: "ingest timed out", Action: action})
					return
				}
				fmt.Fprintf(os.Stderr, "httpapi: ingest option %s: %v\n", symbol, err)
				writeErrorBody(w, http.StatusServiceUnavailable, errorJSON{Code: "ingest_failed", Message: err.Error(), Action: action})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"kind":   "option",
				"symbol": symbol,
				"adjust": adjust,
				"status": "ok",
			})
			return
		}
		timeframe := strings.TrimSpace(req.Timeframe)
		if timeframe == "" {
			timeframe = "1d"
		}
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
