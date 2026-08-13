package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/jiayu/wbot/internal/futu"
)

const futuQuotePath = "/v1/futu/quote"

// FutuQuoter is the live-quote surface the /v1/futu/quote endpoint needs;
// backed by internal/futu.Client (subscribe-then-quote, idempotent; rate
// limits live inside the client, see doc/FUTU.md §8).
type FutuQuoter interface {
	Quote(ctx context.Context, symbol string) (json.RawMessage, error)
}

// LLMOptionQuoter is the richer live option surface used only when assembling
// the complete inventory snapshot for an LLM signal.
type LLMOptionQuoter interface {
	OptionQuotes(ctx context.Context, symbols []string) (map[string]futu.OptionQuoteEx, error)
}

type futuQuoter struct {
	client *futu.Client
}

func (q futuQuoter) Quote(ctx context.Context, symbol string) (json.RawMessage, error) {
	return q.client.Quote(ctx, symbol)
}

func (q futuQuoter) OptionQuotes(ctx context.Context, symbols []string) (map[string]futu.OptionQuoteEx, error) {
	return q.client.OptionQuotes(ctx, symbols)
}

// FutuGatewayURL returns the gateway REST base URL: $FUTU_GATEWAY_URL or the
// default loopback (doc/FUTU.md). Env-only for now; config.yaml lands later.
func FutuGatewayURL() string {
	if v := strings.TrimSpace(os.Getenv("FUTU_GATEWAY_URL")); v != "" {
		return v
	}
	return futu.DefaultAddr
}

// NewFutuQuoter returns a FutuQuoter talking to the gateway at FutuGatewayURL().
func NewFutuQuoter() FutuQuoter {
	return futuQuoter{client: futu.NewClient(FutuGatewayURL())}
}

// FutuQuoteHandler serves GET /v1/futu/quote?symbol=HK.00700: the browser
// cannot reach the gateway directly (CORS/security), so serve proxies it and
// returns the /api/quote s2c passthrough.
func FutuQuoteHandler(quoter FutuQuoter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(futuQuotePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
		if symbol == "" {
			writeError(w, http.StatusBadRequest, "missing query parameter: symbol")
			return
		}
		if _, _, err := futu.ParseSymbol(symbol); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s2c, err := quoter.Quote(r.Context(), symbol)
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: futu quote %s: %v\n", symbol, err)
			status, msg := quoteError(err)
			writeErrorBody(w, status, errorJSON{
				Code:    codeForStatus(status),
				Message: msg,
				Action:  quoteAction(status),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s2c)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}

// quoteError maps a futu client error to the API status and message: gateway
// unreachable (connection/timeout) → 503; upstream HTTP/business errors → 502
// with the gateway message passed through.
func quoteError(err error) (int, string) {
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusServiceUnavailable, "Futu gateway unreachable"
	}
	return http.StatusBadGateway, err.Error()
}

func quoteAction(status int) string {
	if status == http.StatusServiceUnavailable {
		return "start the Futu gateway container (docker compose -f configs/docker-compose.futu.yml up -d) and retry"
	}
	return "check the symbol and gateway state, then retry"
}
