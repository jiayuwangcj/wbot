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
	Volume  int64
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

// flexInt accepts both integer and floating-point JSON numbers because the
// gateway serializes some integer-valued option fields as floats (for example,
// contract_size: 100.0 and open_interest: 3204.0).
type flexInt int64

func (i *flexInt) UnmarshalJSON(data []byte) error {
	var n float64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*i = flexInt(int64(n))
	return nil
}

// optionQuotePage mirrors the /api/option-quote s2c (one contract per call).
type optionQuotePage struct {
	OptionQuoteList []struct {
		Price        float64  `json:"price"`
		Mid          float64  `json:"mid"`
		Vol          flexInt  `json:"vol"`
		MarkPrice    float64  `json:"mark_price"`
		IV           float64  `json:"iv"`
		Delta        float64  `json:"delta"`
		Theta        *float64 `json:"theta"`
		OpenInterest flexInt  `json:"open_interest"`
		LotSize      flexInt  `json:"contract_size"`
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
	// The gateway stalls on very large /api/quote security lists (实测
	// 2026-08-12: 86 contracts time out after 10s, 26 answer in ~0s), so the
	// snapshot quote is sliced; every slice stays well inside the gateway's
	// budget and each slice re-enters the snapshot rate gate.
	const quoteBatch = 30
	var pg quotePage
	for start := 0; start < len(secs); start += quoteBatch {
		if err := SnapshotLimit.Wait(ctx); err != nil {
			return nil, err
		}
		end := start + quoteBatch
		if end > len(secs) {
			end = len(secs)
		}
		s2c, err := c.post(ctx, "/api/quote", map[string]any{"security_list": secs[start:end]})
		if err != nil {
			fmt.Fprintf(os.Stderr, "futu: option-quotes: snapshot [%d/%d]: %v\n", end, len(secs), err)
			continue
		}
		var part quotePage
		if err := json.Unmarshal(s2c, &part); err != nil {
			fmt.Fprintf(os.Stderr, "futu: option-quotes: snapshot [%d/%d]: bad s2c: %v\n", end, len(secs), err)
			continue
		}
		pg.BasicQotList = append(pg.BasicQotList, part.BasicQotList...)
	}
	snapshots := make(map[string]struct {
		market     int
		last       float64
		volume     int64
		updateTime string
	}, len(pg.BasicQotList))
	for _, q := range pg.BasicQotList {
		sym := marketPrefix(q.Security.Market) + q.Security.Code
		if sym == "" {
			continue
		}
		snapshots[sym] = struct {
			market     int
			last       float64
			volume     int64
			updateTime string
		}{q.Security.Market, q.CurPrice, q.Volume, q.UpdateTime}
	}
	stale := make([]string, 0, len(canonical))
	for _, sym := range canonical {
		snapshot, answered := snapshots[sym]
		quote := OptionQuoteEx{
			Symbol:    sym,
			Last:      snapshot.last,
			Volume:    snapshot.volume,
			QuoteTime: parseQuoteTime(snapshot.updateTime, snapshot.market),
		}
		greeksCacheMu.Lock()
		e, cached := greeksCache[sym]
		greeksCacheMu.Unlock()
		if answered && cached && time.Since(e.fetched) <= greeksTTL {
			applyGreeks(&quote, e)
		} else {
			stale = append(stale, sym)
		}
		if answered {
			out[sym] = quote
		}
	}
	greeksFailed := 0
	for _, sym := range stale {
		quote, present := out[sym]
		if !present {
			quote.Symbol = sym
		}
		_, snapshotAnswered := snapshots[sym]
		if err := c.fetchOptionQuote(ctx, sym, &quote, !snapshotAnswered); err != nil {
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
func (c *Client) fetchOptionQuote(ctx context.Context, sym string, quote *OptionQuoteEx, greeksOnly bool) error {
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
		Volume:  int64(leg.Vol),
		IV:      leg.IV / 100, // gateway iv is percent; wheel convention is fraction
		Delta:   leg.Delta,
		Theta:   leg.Theta,
		OI:      int64(leg.OpenInterest),
		LotSize: int(leg.LotSize),
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
	if greeksOnly {
		quote.QuoteTime = time.Now()
	}
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
	if q.Volume == 0 && e.Volume > 0 {
		q.Volume = e.Volume
	}
	if q.LotSize <= 0 {
		q.LotSize = e.LotSize
	}
	if q.Last == 0 {
		q.Last = e.Last
	}
}

// parseQuoteTime parses the gateway's "YYYY-MM-DD HH:MM:SS" wall-clock string
// in the security's market-local zone: the gateway reports HK/CN update times
// in +08 but US times in America/New_York wall clock (实测 2026-08-13: US
// option update_time 12:14 while HKT was 00:19 — parsing US in +08 mislabels
// a fresh mid-session quote as 12h stale and the LLM review gate rejects it).
// Malformed or empty input yields the zero time (never an error).
func parseQuoteTime(s string, market int) time.Time {
	loc := futuLoc
	if market == 11 { // US
		if ny, err := time.LoadLocation("America/New_York"); err == nil {
			loc = ny
		}
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}
