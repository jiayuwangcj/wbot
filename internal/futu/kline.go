package futu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// K-line REST contract (实测 2026-08-01, futu-opend-rs 1.5.0): POST
// /api/history-kline with {"security":{"market":M,"code":C},"kl_type":N,
// "rehab_type":0,"begin_time":"YYYY-MM-DD HH:mm:ss","end_time":...,"max_count":1000,
// "next_req_key":[...]}; paging via the next_req_key cursor. The `symbol` string
// form is broken in this gateway version (strict validation reports a bogus
// unknown field "owner", v1.4.93 BUG-002); use the `security` object form.
// /api/kline needs a subscription and rejects begin/end; see doc/FUTU.md.

// klType values map to the gateway REST enum (实测: 1=1Min 2=Day 3=Week 4=Month
// 5=Year 6=5Min 7=15Min 8=30Min 9=60Min; sub_types: 11/6/12/13/16/7/8/9/10).
var klTypeByName = map[string]int{
	"K_1M": 1, "K_5M": 6, "K_15M": 7, "K_30M": 8, "K_60M": 9,
	"K_DAY": 2, "K_WEEK": 3, "K_MONTH": 4,
}

// ingestByName maps futu K-line names to the ingest timeframe convention
// (bars.timeframe, doc/DATA_PIPELINE.md: 1m 5m 15m 30m 60m 1d 1w 1mo).
var ingestByName = map[string]string{
	"K_1M": "1m", "K_5M": "5m", "K_15M": "15m", "K_30M": "30m", "K_60M": "60m",
	"K_DAY": "1d", "K_WEEK": "1w", "K_MONTH": "1mo",
}

// ParseTimeframe maps a futu K-line name (K_1M..K_MONTH, case-insensitive) to
// the gateway kl_type and the ingest convention; ingest names are as-is.
func ParseTimeframe(s string) (klType int, ingestTF string, err error) {
	name := strings.ToUpper(strings.TrimSpace(s))
	if k, ok := klTypeByName[name]; ok {
		return k, ingestByName[name], nil
	}
	trimmed := strings.TrimSpace(s)
	for k, v := range ingestByName {
		if v == trimmed {
			return klTypeByName[k], v, nil
		}
	}
	return 0, "", fmt.Errorf("unsupported timeframe %q (want K_1M K_5M K_15M K_30M K_60M K_DAY K_WEEK K_MONTH)", s)
}

// KBar is one K-line row: bar start instant (UTC) plus OHLCV.
type KBar struct {
	Ts      time.Time
	Open    float64
	High    float64
	Low     float64
	Close   float64
	Volume  int64
	IsBlank bool
}

// MaxKlinePage is the per-request bar cap (实测 max_count=1000 works).
const MaxKlinePage = 1000

// klinePage mirrors the /api/history-kline s2c payload.
type klinePage struct {
	KLList []struct {
		Timestamp  float64 `json:"timestamp"`
		IsBlank    bool    `json:"is_blank"`
		OpenPrice  float64 `json:"open_price"`
		HighPrice  float64 `json:"high_price"`
		LowPrice   float64 `json:"low_price"`
		ClosePrice float64 `json:"close_price"`
		Volume     int64   `json:"volume"`
	} `json:"kl_list"`
	NextReqKey json.RawMessage `json:"next_req_key"`
}

// futuLoc is the gateway's wall-clock zone (market local +08; response timestamps
// round-trip at this offset).
var futuLoc = time.FixedZone("futu+08", 8*3600)

// HistoryKline fetches K-lines covering [from, to]; zero from defaults to
// 2000-01-01 and zero to to now+24h (covers the forming bar). No subscription
// is needed. Pages are spaced ≥ BatchGap apart and first pages pass the
// official 10-per-30s gate; HTTP 429 retries with backoff inside post().
func (c *Client) HistoryKline(ctx context.Context, symbol string, klType int, from, to time.Time) ([]KBar, error) {
	market, code, err := ParseSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if klType < 1 {
		return nil, fmt.Errorf("bad kl_type %d", klType)
	}
	if from.IsZero() {
		from = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if to.IsZero() {
		to = time.Now().Add(24 * time.Hour)
	}
	body := map[string]any{
		"security":   map[string]any{"market": market, "code": code},
		"kl_type":    klType,
		"rehab_type": 0, // 不复权
		"begin_time": from.In(futuLoc).Format("2006-01-02 15:04:05"),
		"end_time":   to.In(futuLoc).Format("2006-01-02 15:04:05"),
		"max_count":  MaxKlinePage,
	}
	var out []KBar
	for page := 0; ; page++ {
		if page == 0 {
			if err := HistoryPageLimit.Wait(ctx); err != nil {
				return nil, err
			}
		} else {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(BatchGap):
			}
		}
		if err := KlineLimit.Wait(ctx); err != nil {
			return nil, err
		}
		s2c, err := c.post(ctx, "/api/history-kline", body)
		if err != nil {
			return nil, fmt.Errorf("history-kline %s page %d: %w", symbol, page, err)
		}
		var pg klinePage
		if err := json.Unmarshal(s2c, &pg); err != nil {
			return nil, fmt.Errorf("history-kline %s page %d: bad s2c: %w", symbol, page, err)
		}
		for _, b := range pg.KLList {
			out = append(out, KBar{
				Ts:      time.Unix(int64(b.Timestamp), 0).UTC(),
				Open:    b.OpenPrice,
				High:    b.HighPrice,
				Low:     b.LowPrice,
				Close:   b.ClosePrice,
				Volume:  b.Volume,
				IsBlank: b.IsBlank,
			})
		}
		if len(pg.NextReqKey) == 0 || string(pg.NextReqKey) == "null" {
			return out, nil
		}
		body["next_req_key"] = pg.NextReqKey
	}
}
