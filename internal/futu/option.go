package futu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Option REST contract (实测 2026-08-01, futu-opend-rs 1.5.0): POST
// /api/option-expiration-date with {"owner":{"market":M,"code":C}} and POST
// /api/option-chain with {"owner":{...},"begin_time":"YYYY-MM-DD",
// "end_time":"YYYY-MM-DD"} (expiration window, inclusive). The chain groups
// contracts by strike_time: option[].call / option[].put each carry basic
// (security code like TCH260807C335000) and option_ex_data (type/strike).
// No subscription needed; snapshot-class rate limits apply (doc/FUTU.md §10).

// OptionExpiry is one listed expiration of an underlying's option chain.
type OptionExpiry struct {
	Date         string // "2026-08-07" (strike_time, market-local date)
	Timestamp    time.Time
	DistanceDays int
	Cycle        int
}

// OptionContract is one call or put from the chain.
type OptionContract struct {
	Symbol     string // e.g. HK.TCH260807C335000
	Underlying string // e.g. HK.00700 (the owner passed in)
	OptionType string // "call" or "put"
	Strike     float64
	Expiry     time.Time
	LotSize    int
}

// expirationPage mirrors the /api/option-expiration-date s2c payload.
type expirationPage struct {
	DateList []struct {
		StrikeTime   string  `json:"strike_time"`
		StrikeTs     float64 `json:"strike_timestamp"`
		DistanceDays int     `json:"option_expiry_date_distance"`
		Cycle        int     `json:"cycle"`
	} `json:"date_list"`
}

// chainPage mirrors the /api/option-chain s2c payload (call/put as raw JSON
// because both arms share one shape).
type chainPage struct {
	OptionChain []struct {
		StrikeTime string  `json:"strike_time"`
		StrikeTs   float64 `json:"strike_timestamp"`
		Option     []struct {
			Call json.RawMessage `json:"call"`
			Put  json.RawMessage `json:"put"`
		} `json:"option"`
	} `json:"option_chain"`
}

// chainLeg is one call/put arm inside an option-chain entry.
type chainLeg struct {
	Basic struct {
		Security struct {
			Market int    `json:"market"`
			Code   string `json:"code"`
		} `json:"security"`
		LotSize int `json:"lot_size"`
	} `json:"basic"`
	OptionExData struct {
		Type        int     `json:"type"`
		StrikePrice float64 `json:"strike_price"`
		StrikeTime  string  `json:"strike_time"`
	} `json:"option_ex_data"`
}

// OptionExpirations lists the underlying's listed expirations (all cycles).
func (c *Client) OptionExpirations(ctx context.Context, symbol string) ([]OptionExpiry, error) {
	market, code, err := ParseSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if err := SnapshotLimit.Wait(ctx); err != nil {
		return nil, err
	}
	s2c, err := c.post(ctx, "/api/option-expiration-date", map[string]any{
		"owner": map[string]any{"market": market, "code": code},
	})
	if err != nil {
		return nil, fmt.Errorf("option-expiration-date %s: %w", symbol, err)
	}
	var pg expirationPage
	if err := json.Unmarshal(s2c, &pg); err != nil {
		return nil, fmt.Errorf("option-expiration-date %s: bad s2c: %w", symbol, err)
	}
	out := make([]OptionExpiry, 0, len(pg.DateList))
	for _, d := range pg.DateList {
		out = append(out, OptionExpiry{
			Date:         d.StrikeTime,
			Timestamp:    time.Unix(int64(d.StrikeTs), 0).UTC(),
			DistanceDays: d.DistanceDays,
			Cycle:        d.Cycle,
		})
	}
	return out, nil
}

// OptionChain returns every call/put contract for the expiries in the closed
// window [begin, end] (dates only; the gateway ignores the time of day).
func (c *Client) OptionChain(ctx context.Context, symbol string, begin, end time.Time) ([]OptionContract, error) {
	market, code, err := ParseSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if begin.IsZero() || end.IsZero() || begin.After(end) {
		return nil, fmt.Errorf("option chain: need begin <= end, got %v..%v", begin, end)
	}
	if err := SnapshotLimit.Wait(ctx); err != nil {
		return nil, err
	}
	// strike_timestamp is +08 market-local midnight (实测 2026-08-01); the
	// gateway takes dates in that zone, so format in futuLoc (like HistoryKline).
	s2c, err := c.post(ctx, "/api/option-chain", map[string]any{
		"owner":      map[string]any{"market": market, "code": code},
		"begin_time": begin.In(futuLoc).Format("2006-01-02"),
		"end_time":   end.In(futuLoc).Format("2006-01-02"),
	})
	if err != nil {
		return nil, fmt.Errorf("option-chain %s: %w", symbol, err)
	}
	var pg chainPage
	if err := json.Unmarshal(s2c, &pg); err != nil {
		return nil, fmt.Errorf("option-chain %s: bad s2c: %w", symbol, err)
	}
	var out []OptionContract
	for _, group := range pg.OptionChain {
		expiry, err := parseOptionDate(group.StrikeTime)
		if err != nil {
			return nil, fmt.Errorf("option-chain %s: %w", symbol, err)
		}
		for _, o := range group.Option {
			for _, arm := range []struct {
				name string
				raw  json.RawMessage
			}{{"call", o.Call}, {"put", o.Put}} {
				if len(arm.raw) == 0 || string(arm.raw) == "null" {
					continue
				}
				var leg chainLeg
				if err := json.Unmarshal(arm.raw, &leg); err != nil {
					return nil, fmt.Errorf("option-chain %s: bad %s leg: %w", symbol, arm.name, err)
				}
				oc := OptionContract{
					Symbol:     marketPrefix(leg.Basic.Security.Market) + leg.Basic.Security.Code,
					Underlying: symbol,
					OptionType: arm.name,
					Strike:     leg.OptionExData.StrikePrice,
					Expiry:     expiry,
					LotSize:    leg.Basic.LotSize,
				}
				out = append(out, oc)
			}
		}
	}
	return out, nil
}

// parseOptionDate parses the chain's "YYYY-MM-DD" strike_time as UTC midnight.
func parseOptionDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad strike_time %q", s)
	}
	return t, nil
}

// marketPrefix maps a futu market enum back to the symbol prefix.
func marketPrefix(market int) string {
	for pre, m := range marketCode {
		if m == market {
			return pre + "."
		}
	}
	return ""
}
