package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// underlyingQuoter fetches the live quote JSON for a symbol so the card can
// show the underlying's display name (futuQuoter implements; fakes in tests).
// QuoteRaw is separate from the wheel quoter's float64 Quote so the card
// never couples to the polling adapter. The name lookup is best-effort: cards
// fall back to the bare code when the quote is unavailable, so push never
// blocks on it.
type underlyingQuoter interface {
	QuoteRaw(ctx context.Context, symbol string) (json.RawMessage, error)
}

// underlyingName returns the exchange display name (腾讯控股) from the live
// quote page, or "" when the quote is missing/unparseable. 5s budget so a
// stalled gateway never holds up the push loop.
func underlyingName(ctx context.Context, q underlyingQuoter, symbol string) string {
	if q == nil {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, err := q.QuoteRaw(cctx, symbol)
	if err != nil {
		return ""
	}
	var page struct {
		List []struct {
			Name string `json:"name"`
		} `json:"basic_qot_list"`
	}
	if err := json.Unmarshal(raw, &page); err != nil || len(page.List) == 0 {
		return ""
	}
	return strings.TrimSpace(page.List[0].Name)
}

// codeFromSymbol strips the market prefix: HK.00700 → 00700, US.AAPL → AAPL.
func codeFromSymbol(symbol string) string {
	_, code, ok := strings.Cut(symbol, ".")
	if !ok {
		return symbol
	}
	return code
}

// underlyingLabel renders 「腾讯控股 · 00700」; without a name the bare code
// is enough (老板指令 2026-08-13: 正股价格区多一份底层资产名字和编号)。
func underlyingLabel(name, symbol string) string {
	code := codeFromSymbol(symbol)
	if strings.TrimSpace(name) == "" {
		return code
	}
	return name + " · " + code
}
