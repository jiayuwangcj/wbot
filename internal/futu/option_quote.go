package futu

// OptionQuotes batch-subscribes option contracts and pulls one /api/quote
// snapshot (basic + Greeks). Field paths follow the OpenD REST convention and
// are frozen by test fixture — the real gateway was offline 2026-08-11, verify
// the JSON keys against it before enabling live runners (doc/FUTU.md §10).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// OptionQuoteEx is one contract's realtime quote: basic market data plus
// Greeks. Zero fields mean the gateway did not provide them; callers then
// treat the snapshot as DATA_BLOCKED (the runner falls back, never errors).
// Theta is a pointer like wheel.OptionQuote.Theta: nil = provider field
// missing, non-nil zero = a real observed value of zero.
type OptionQuoteEx struct {
	Symbol       string
	Bid          float64
	Ask          float64
	Last         float64
	Volume       int64
	OpenInterest int64
	ImpliedVol   float64
	Delta        float64
	Theta        *float64
	QuoteTime    time.Time
	LotSize      int
}

// quotePage mirrors the /api/quote s2c for option contracts (basic fields on
// the list item, Greeks in the option extension object).
type quotePage struct {
	BasicQotList []struct {
		Security struct {
			Market int    `json:"market"`
			Code   string `json:"code"`
		} `json:"security"`
		BidPrice   float64 `json:"bid_price"`
		AskPrice   float64 `json:"ask_price"`
		LastPrice  float64 `json:"last_price"`
		Volume     int64   `json:"volume"`
		UpdateTime string  `json:"update_time"`
		ExData     struct {
			ImpliedVolatility float64  `json:"implied_volatility"`
			Delta             float64  `json:"delta"`
			Theta             *float64 `json:"theta"`
			OpenInterest      int64    `json:"open_interest"`
			LotSize           int      `json:"lot_size"`
		} `json:"ex_data"`
	} `json:"basic_qot_list"`
}

// OptionQuotes subscribes to every symbol (one batch call) then takes a single
// snapshot; returns quotes keyed by canonical MARKET.CODE. Symbols the gateway
// does not answer stay absent from the map.
func (c *Client) OptionQuotes(ctx context.Context, symbols []string) (map[string]OptionQuoteEx, error) {
	out := make(map[string]OptionQuoteEx, len(symbols))
	if len(symbols) == 0 {
		return out, nil
	}
	canonical := make([]string, 0, len(symbols))
	secs := make([]map[string]any, 0, len(symbols))
	for _, s := range symbols {
		market, code, err := ParseSymbol(s)
		if err != nil {
			return nil, fmt.Errorf("option-quotes: %w", err)
		}
		canonical = append(canonical, marketPrefix(market)+code)
		secs = append(secs, map[string]any{"market": market, "code": code})
	}
	if _, err := c.post(ctx, "/api/subscribe", map[string]any{
		"symbols":          canonical,
		"sub_types":        []int{1}, // SubType_Basic; TODO(slice-b): Greeks may need SubType_Option (7)
		"is_sub_or_un_sub": true,
	}); err != nil {
		return nil, fmt.Errorf("option-quotes subscribe: %w", err)
	}
	if err := SnapshotLimit.Wait(ctx); err != nil {
		return nil, err
	}
	s2c, err := c.post(ctx, "/api/quote", map[string]any{"security_list": secs})
	if err != nil {
		return nil, fmt.Errorf("option-quotes quote: %w", err)
	}
	var pg quotePage
	if err := json.Unmarshal(s2c, &pg); err != nil {
		return nil, fmt.Errorf("option-quotes: bad s2c: %w", err)
	}
	bidAskZero := 0
	for _, q := range pg.BasicQotList {
		if q.BidPrice == 0 && q.AskPrice == 0 {
			bidAskZero++
		}
		sym := marketPrefix(q.Security.Market) + q.Security.Code
		if sym == "" {
			continue
		}
		out[sym] = OptionQuoteEx{
			Symbol:       sym,
			Bid:          q.BidPrice,
			Ask:          q.AskPrice,
			Last:         q.LastPrice,
			Volume:       q.Volume,
			OpenInterest: q.ExData.OpenInterest,
			ImpliedVol:   q.ExData.ImpliedVolatility,
			Delta:        q.ExData.Delta,
			Theta:        q.ExData.Theta,
			QuoteTime:    parseQuoteTime(q.UpdateTime),
			LotSize:      q.ExData.LotSize,
		}
	}
	// warn on mismatched shapes so ops can tell a wrong field path (always-zero
	// bid/ask → permanent DATA_BLOCKED) apart from a real market halt
	if len(pg.BasicQotList) != len(symbols) || bidAskZero > 0 {
		fmt.Fprintf(os.Stderr, "futu: option-quotes: requested=%d answered=%d bidask_zero=%d\n", len(symbols), len(pg.BasicQotList), bidAskZero)
	}
	return out, nil
}

// parseQuoteTime parses the gateway's "YYYY-MM-DD HH:MM:SS" wall-clock string
// in futuLoc; malformed or empty input yields the zero time (never an error).
func parseQuoteTime(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, futuLoc)
	if err != nil {
		return time.Time{}
	}
	return t
}
