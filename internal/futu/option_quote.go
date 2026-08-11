package futu

// OptionQuotes feeds the wheel live runner's option data plane from two REST
// layers (实测 2026-08-11, futu-opend-rs 1.4.106, doc/FUTU.md §10):
//  1. snapshot  /api/quote batch — cur_price/volume/update_time only; the real
//     gateway carries no bid/ask and option_ex_data is always null.
//  2. greeks    /api/option-quote per contract (免订阅, 1 req/3s) — price/mid/
//     iv/delta/theta/open_interest/contract_size; no order book, so Bid and
//     Ask both take mid (or price when mid is zero).
// The snapshot answers every chain leg immediately each tick; the greeks pass
// only touches legs missing from the package cache or older than greeksTTL
// (cold 85-leg pass ≈255s ≈ one 5min tick, so with the 10min TTL the pass
// repeats every other tick and the snapshot still runs every tick).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// greeksTTL is how long a contract's option-quote payload is reused before a
// refetch; a package variable like the rate pools so tests can shrink it.
var greeksTTL = 10 * time.Minute

// greeksCache is the package-level option-quote cache: keyed by canonical
// MARKET.CODE, shared by every Client so ticks refetch only stale legs.
var (
	greeksCacheMu sync.Mutex
	greeksCache   = map[string]greeksEntry{}
)

// greeksEntry is one contract's cached option-quote payload. Bid/Ask both
// equal the mid (the endpoint has no order book) or the price when mid is
// zero; IV is normalized from the gateway's percent to the wheel fraction
// convention (实测 iv 122.07 → 1.2207, like the backtest fixtures' 0.25).
type greeksEntry struct {
	fetched time.Time
	Bid     float64
	Ask     float64
	Last    float64 // option-quote price; snapshot cur_price stays the primary Last
	IV      float64
	Delta   float64
	Theta   *float64
	OI      int64
	LotSize int
}

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

// quotePage mirrors the /api/quote s2c for option contracts: the real gateway
// answers with the same shape as stocks (cur_price/volume/update_time).
type quotePage struct {
	BasicQotList []struct {
		Security struct {
			Market int    `json:"market"`
			Code   string `json:"code"`
		} `json:"security"`
		CurPrice   float64 `json:"cur_price"`
		Volume     int64   `json:"volume"`
		UpdateTime string  `json:"update_time"`
	} `json:"basic_qot_list"`
}

// optionQuotePage mirrors the /api/option-quote s2c (one contract per call).
type optionQuotePage struct {
	OptionQuoteList []struct {
		Price        float64  `json:"price"`
		Mid          float64  `json:"mid"`
		IV           float64  `json:"iv"`
		Delta        float64  `json:"delta"`
		Theta        *float64 `json:"theta"`
		OpenInterest int64    `json:"open_interest"`
		LotSize      int      `json:"contract_size"`
	} `json:"option_quote_list"`
}

// OptionQuotes returns quotes keyed by canonical MARKET.CODE. Every symbol
// gets the batch snapshot immediately; the greeks layer then fills Delta/IV/
// Theta/OI/Bid/Ask from /api/option-quote for legs that are missing or stale
// in the package cache. Symbols the gateway does not answer stay absent; a
// failed greeks fetch leaves the leg at zero fields (Validate rejects it —
// safe, never a fake ALERT).
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
		"sub_types":        []int{1}, // SubType_Basic; 实测 2026-08-11: sub_types=[1,7] 不改变字段
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
	stale := make([]string, 0, len(pg.BasicQotList))
	for _, q := range pg.BasicQotList {
		sym := marketPrefix(q.Security.Market) + q.Security.Code
		if sym == "" {
			continue
		}
		quote := OptionQuoteEx{
			Symbol:    sym,
			Last:      q.CurPrice,
			Volume:    q.Volume,
			QuoteTime: parseQuoteTime(q.UpdateTime),
		}
		greeksCacheMu.Lock()
		e, cached := greeksCache[sym]
		greeksCacheMu.Unlock()
		if cached && time.Since(e.fetched) <= greeksTTL {
			applyGreeks(&quote, e)
		} else {
			stale = append(stale, sym)
		}
		out[sym] = quote
	}
	greeksFailed := 0
	for _, sym := range stale {
		quote := out[sym]
		if err := c.fetchOptionQuote(ctx, sym, &quote); err != nil {
			greeksFailed++
			fmt.Fprintf(os.Stderr, "futu: option-quotes: greeks %s: %v\n", sym, err)
			continue
		}
		out[sym] = quote
	}
	// warn on mismatched shapes so ops can tell a wrong field path (cur_price
	// missing → 0 Last, greeks_failed → permanent DATA_BLOCKED) apart from a
	// real market halt
	if len(pg.BasicQotList) != len(symbols) || greeksFailed > 0 {
		fmt.Fprintf(os.Stderr, "futu: option-quotes: requested=%d answered=%d greeks_failed=%d\n", len(symbols), len(pg.BasicQotList), greeksFailed)
	}
	return out, nil
}

// fetchOptionQuote pulls one contract's /api/option-quote payload (single leg,
// 免订阅, gated by SnapshotLimit 1 req/3s), fills the quote's Greeks plus
// Bid/Ask and caches the payload for greeksTTL.
func (c *Client) fetchOptionQuote(ctx context.Context, sym string, quote *OptionQuoteEx) error {
	market, code, err := ParseSymbol(sym)
	if err != nil {
		return err
	}
	if err := SnapshotLimit.Wait(ctx); err != nil {
		return err
	}
	s2c, err := c.post(ctx, "/api/option-quote", map[string]any{
		"multi_legs": []map[string]any{{
			"security":  map[string]any{"market": market, "code": code},
			"side":      1,
			"qty_ratio": 1,
		}},
	})
	if err != nil {
		return fmt.Errorf("option-quote: %w", err)
	}
	var pg optionQuotePage
	if err := json.Unmarshal(s2c, &pg); err != nil {
		return fmt.Errorf("option-quote: bad s2c: %w", err)
	}
	if len(pg.OptionQuoteList) == 0 {
		return fmt.Errorf("option-quote: empty response")
	}
	leg := pg.OptionQuoteList[0]
	e := greeksEntry{
		fetched: time.Now(),
		Last:    leg.Price,
		IV:      leg.IV / 100, // gateway iv is percent; wheel convention is fraction
		Delta:   leg.Delta,
		Theta:   leg.Theta,
		OI:      leg.OpenInterest,
		LotSize: leg.LotSize,
	}
	if leg.Mid > 0 {
		e.Bid, e.Ask = leg.Mid, leg.Mid
	} else {
		e.Bid, e.Ask = leg.Price, leg.Price
	}
	greeksCacheMu.Lock()
	greeksCache[sym] = e
	greeksCacheMu.Unlock()
	applyGreeks(quote, e)
	return nil
}

// applyGreeks copies a cached/fresh greeks payload into the snapshot quote;
// Last keeps the snapshot cur_price and only falls back to the option-quote
// price when the snapshot carried zero (限价语义 = 成交价, task 2026-08-11).
func applyGreeks(q *OptionQuoteEx, e greeksEntry) {
	q.Bid, q.Ask = e.Bid, e.Ask
	q.ImpliedVol = e.IV
	q.Delta = e.Delta
	q.Theta = e.Theta
	q.OpenInterest = e.OI
	if q.LotSize <= 0 {
		q.LotSize = e.LotSize
	}
	if q.Last == 0 {
		q.Last = e.Last
	}
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
