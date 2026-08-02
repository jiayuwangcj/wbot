package httpapi

// Futu options-chain proxy: GET /v1/futu/options serves the underlying's
// listed expirations plus one expiry's call/put chain for the browser, which
// cannot reach the loopback gateway directly (CORS/security). Same narrow-
// interface proxy pattern as the quote endpoint; snapshot-class rate limits
// (1 req/3s) live inside the client (doc/FUTU.md §8/§10).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/ingest"
)

const futuOptionsPath = "/v1/futu/options"

// FutuOptionChainer is the expirations+chain surface the /v1/futu/options
// endpoint needs; backed by internal/futu.Client (OptionExpirations/OptionChain).
type FutuOptionChainer interface {
	Expirations(ctx context.Context, symbol string) ([]futu.OptionExpiry, error)
	Chain(ctx context.Context, symbol string, begin, end time.Time) ([]futu.OptionContract, error)
}

type futuOptionChainer struct {
	client *futu.Client
}

func (c futuOptionChainer) Expirations(ctx context.Context, symbol string) ([]futu.OptionExpiry, error) {
	return c.client.OptionExpirations(ctx, symbol)
}

func (c futuOptionChainer) Chain(ctx context.Context, symbol string, begin, end time.Time) ([]futu.OptionContract, error) {
	return c.client.OptionChain(ctx, symbol, begin, end)
}

// NewFutuOptionChainer returns a FutuOptionChainer talking to the gateway at
// FutuGatewayURL() (same $FUTU_GATEWAY_URL env as the quote proxy).
func NewFutuOptionChainer() FutuOptionChainer {
	return futuOptionChainer{client: futu.NewClient(FutuGatewayURL())}
}

// optionExpiryJSON is one listed expiration of the underlying.
type optionExpiryJSON struct {
	Date         string `json:"date"`          // "2026-08-07" (strike_time, market-local)
	Timestamp    string `json:"timestamp"`     // RFC3339 UTC
	DistanceDays int    `json:"distance_days"` // negative = already expired
	Cycle        int    `json:"cycle"`
}

// FutuOptionPremier supplies the stored daily-close premium (权利金) for one
// contract symbol from option_quotes (P3a 2026-08-03, doc/FUTU.md §10); the
// gateway chain payload itself carries no premium. nil is allowed: pure-gateway
// mode omits the premium fields.
type FutuOptionPremier interface {
	LatestOptionQuote(ctx context.Context, symbol string) (*ingest.OptionQuoteRow, error)
}

// optionContractJSON is one call/put leg of the chain. The gateway chain
// payload carries no premium (权利金) — only code/strike/lot size; the daily-
// close premium comes from stored contract daily K (option_quotes), live
// per-contract option-quote/IV stays P3 (doc/FUTU.md §10).
type optionContractJSON struct {
	Expiry         string   `json:"expiry"`      // "2026-08-07"
	OptionType     string   `json:"option_type"` // "call" or "put"
	Strike         float64  `json:"strike"`
	Symbol         string   `json:"symbol"` // e.g. HK.TCH260807C335000
	LotSize        int      `json:"lot_size"`
	PremiumClose   *float64 `json:"premium_close,omitempty"`    // 最近日 K 收盘权利金（option_quotes 落库数据，非实时）
	PremiumCloseTs *string  `json:"premium_close_ts,omitempty"` // 上述价格的数据日期（RFC3339 UTC）
}

// optionsJSON is the GET /v1/futu/options success body.
type optionsJSON struct {
	Symbol      string               `json:"symbol"`
	Expiry      string               `json:"expiry"` // expiry the contracts belong to ("" = none)
	Expirations []optionExpiryJSON   `json:"expirations"`
	Contracts   []optionContractJSON `json:"contracts"`
}

// FutuOptionsHandler serves GET /v1/futu/options?symbol=HK.00700[&expiry=YYYY-MM-DD]:
// the expirations list (for the UI dropdown) plus the call/put chain of one
// expiry — the requested one, or the nearest future expiry when omitted.
// An optional FutuOptionPremier (variadic) decorates each contract with the
// stored daily-close premium; without it the premium fields are omitted.
func FutuOptionsHandler(chainer FutuOptionChainer, premier ...FutuOptionPremier) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(futuOptionsPath, func(w http.ResponseWriter, r *http.Request) {
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
		var requested time.Time
		if s := strings.TrimSpace(r.URL.Query().Get("expiry")); s != "" {
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid expiry %q (want YYYY-MM-DD)", s))
				return
			}
			requested = t
		}
		expirations, err := chainer.Expirations(r.Context(), symbol)
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: futu options %s: %v\n", symbol, err)
			status, msg := quoteError(err)
			writeErrorBody(w, status, errorJSON{
				Code:    codeForStatus(status),
				Message: msg,
				Action:  quoteAction(status),
			})
			return
		}
		out := optionsJSON{
			Symbol:      symbol,
			Expirations: make([]optionExpiryJSON, 0, len(expirations)),
			Contracts:   []optionContractJSON{},
		}
		for _, e := range expirations {
			out.Expirations = append(out.Expirations, optionExpiryJSON{
				Date:         e.Date,
				Timestamp:    e.Timestamp.Format(time.RFC3339),
				DistanceDays: e.DistanceDays,
				Cycle:        e.Cycle,
			})
		}
		expiry, ok := pickExpiry(expirations, requested)
		if ok {
			// The window uses the date string (strike_time is the authoritative
			// date; the timestamp instant is +08 midnight and shifts in UTC).
			begin, _ := time.Parse("2006-01-02", expiry)
			contracts, err := chainer.Chain(r.Context(), symbol, begin, begin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "httpapi: futu options %s: %v\n", symbol, err)
				status, msg := quoteError(err)
				writeErrorBody(w, status, errorJSON{
					Code:    codeForStatus(status),
					Message: msg,
					Action:  quoteAction(status),
				})
				return
			}
			out.Expiry = expiry
			out.Contracts = make([]optionContractJSON, 0, len(contracts))
			var p FutuOptionPremier
			if len(premier) > 0 {
				p = premier[0]
			}
			for _, c := range contracts {
				leg := optionContractJSON{
					Expiry:     expiry,
					OptionType: c.OptionType,
					Strike:     c.Strike,
					Symbol:     c.Symbol,
					LotSize:    c.LotSize,
				}
				if p != nil {
					if row, err := p.LatestOptionQuote(r.Context(), c.Symbol); err != nil {
						// Premium is a decoration; a query failure must not fail the chain.
						fmt.Fprintf(os.Stderr, "httpapi: futu options premium %s: %v\n", c.Symbol, err)
					} else if row != nil {
						leg.PremiumClose = &row.Close
						ts := row.Ts.Format(time.RFC3339)
						leg.PremiumCloseTs = &ts
					}
				}
				out.Contracts = append(out.Contracts, leg)
			}
			sort.Slice(out.Contracts, func(i, j int) bool {
				if out.Contracts[i].Strike != out.Contracts[j].Strike {
					return out.Contracts[i].Strike < out.Contracts[j].Strike
				}
				return out.Contracts[i].OptionType < out.Contracts[j].OptionType
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

// pickExpiry selects the chain expiry date: the requested one when given,
// else the nearest future expiry (smallest distance_days >= 0). ok=false
// means nothing to query (no expirations at all).
func pickExpiry(expirations []futu.OptionExpiry, requested time.Time) (string, bool) {
	if !requested.IsZero() {
		return requested.Format("2006-01-02"), true
	}
	var best *futu.OptionExpiry
	for i := range expirations {
		if expirations[i].DistanceDays < 0 {
			continue
		}
		if best == nil || expirations[i].DistanceDays < best.DistanceDays {
			best = &expirations[i]
		}
	}
	if best == nil {
		return "", false
	}
	return best.Date, true
}
