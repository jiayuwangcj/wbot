package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/domain"
	"github.com/jiayu/wbot/internal/futu"
)

const (
	// TencentKlineEndpoint is Tencent Finance's unauthenticated K-line endpoint.
	TencentKlineEndpoint = "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get"
	// TencentMaxBars is the largest documented/requested backfill window used by
	// this adapter. The endpoint may additionally return the forming daily bar.
	TencentMaxBars = 1000
)

var (
	// Tencent requests share one process-wide one-second gate. Retries pass the
	// same gate and add exponential backoff, so a failing endpoint is never hit
	// more aggressively than a successful one.
	tencentRequestLimit = futu.NewLimiter(time.Second)
	tencentRetryBackoff = []time.Duration{time.Second, 2 * time.Second}
	tencentDateLocation = time.FixedZone("tencent+08", 8*60*60)
)

// TencentInstrument is the canonical wbot symbol plus the code expected by
// Tencent Finance (for example HK.00700 -> hk00700).
type TencentInstrument struct {
	Symbol       domain.Symbol
	ProviderCode string
	Market       string
}

// ParseTencentInstrument validates and canonicalizes a wbot market-qualified
// symbol for Tencent Finance. The four equity markets already accepted by the
// ingestion stack are supported; this task's acceptance targets HK and US.
func ParseTencentInstrument(symbol string) (TencentInstrument, error) {
	market, code, err := futu.ParseSymbol(strings.TrimSpace(symbol))
	if err != nil {
		return TencentInstrument{}, fmt.Errorf("ingest: tencent source: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return TencentInstrument{}, errors.New("ingest: tencent source: empty instrument code")
	}
	var marketName, providerPrefix string
	switch market {
	case 1:
		marketName, providerPrefix = "HK", "hk"
	case 11:
		marketName, providerPrefix = "US", "us"
		code = strings.ToUpper(code)
	case 21:
		marketName, providerPrefix = "SH", "sh"
	case 22:
		marketName, providerPrefix = "SZ", "sz"
	default:
		return TencentInstrument{}, fmt.Errorf("ingest: tencent source: unsupported market %d", market)
	}
	return TencentInstrument{
		Symbol:       domain.Symbol(marketName + "." + code),
		ProviderCode: providerPrefix + code,
		Market:       marketName,
	}, nil
}

// TencentSource pulls qfq daily OHLCV bars from Tencent Finance. Count defaults
// to 1000. Only daily bars are accepted: the backfill is deliberately separate
// from the real-time Futu snapshot path.
type TencentSource struct {
	Client         *http.Client
	Endpoint       string
	Count          int
	IncludeForming bool
	now            func() time.Time
}

type tencentResponse struct {
	Code int                        `json:"code"`
	Msg  string                     `json:"msg"`
	Data map[string]json.RawMessage `json:"data"`
}

type tencentSeries struct {
	Day    []json.RawMessage `json:"day"`
	QFQDay []json.RawMessage `json:"qfqday"`
}

// Bars fetches qfq daily bars in [from, to]. Tencent rows are
// date/open/close/high/low/volume strings; dates are normalized from the +08
// wall clock to UTC instants, matching the repository's HK daily-bar convention.
func (s TencentSource) Bars(ctx context.Context, symbol domain.Symbol, timeframe string, from, to time.Time) ([]Bar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if timeframe != "1d" && !strings.EqualFold(timeframe, "K_DAY") && !strings.EqualFold(timeframe, "day") {
		return nil, fmt.Errorf("ingest: tencent source: unsupported timeframe %q (want 1d)", timeframe)
	}
	instrument, err := ParseTencentInstrument(string(symbol))
	if err != nil {
		return nil, err
	}
	count := s.Count
	if count == 0 {
		count = TencentMaxBars
	}
	if count < 1 || count > TencentMaxBars {
		return nil, fmt.Errorf("ingest: tencent source: count must be between 1 and %d", TencentMaxBars)
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, errors.New("ingest: tencent source: from after to")
	}

	endpoint := strings.TrimSpace(s.Endpoint)
	if endpoint == "" {
		endpoint = TencentKlineEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("ingest: tencent source: bad endpoint: %w", err)
	}
	q := u.Query()
	start, end := "", ""
	if !from.IsZero() {
		start = from.In(tencentDateLocation).Format("2006-01-02")
	}
	if !to.IsZero() {
		end = to.In(tencentDateLocation).Format("2006-01-02")
	}
	q.Set("param", strings.Join([]string{instrument.ProviderCode, "day", start, end, strconv.Itoa(count), "qfq"}, ","))
	u.RawQuery = q.Encode()

	body, err := s.get(ctx, u.String())
	if err != nil {
		return nil, err
	}
	var envelope tencentResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("ingest: tencent source: bad JSON: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("ingest: tencent source: provider code %d: %s", envelope.Code, strings.TrimSpace(envelope.Msg))
	}
	raw, ok := envelope.Data[instrument.ProviderCode]
	if !ok {
		for key, candidate := range envelope.Data {
			if strings.EqualFold(key, instrument.ProviderCode) {
				raw, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return nil, fmt.Errorf("ingest: tencent source: response missing %s", instrument.ProviderCode)
	}
	var series tencentSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		return nil, fmt.Errorf("ingest: tencent source: bad series for %s: %w", instrument.ProviderCode, err)
	}
	rows := series.QFQDay
	if len(rows) == 0 {
		// HK and US currently return the requested qfq data under "day".
		rows = series.Day
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("ingest: tencent source: no daily bars for %s", instrument.Symbol)
	}

	bars := make([]Bar, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for i, row := range rows {
		bar, err := parseTencentBar(row)
		if err != nil {
			return nil, fmt.Errorf("ingest: tencent source: row %d: %w", i, err)
		}
		key := bar.Ts.UnixNano()
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		bars = append(bars, bar)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Ts.Before(bars[j].Ts) })
	bars = filterRange(bars, from, to)
	bars = filterTencentFormingBar(bars, s.IncludeForming, s.currentTime())
	if len(bars) == 0 {
		return nil, fmt.Errorf("ingest: tencent source: no completed daily bars for %s in requested range (use -include-forming to retain today's partial bar)", instrument.Symbol)
	}
	return bars, nil
}

func (s TencentSource) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// filterTencentFormingBar removes only the newest row when its Tencent +08
// calendar date is today. Daily timestamps represent the start of that local
// date, so comparing instants or a rolling 24-hour duration would be wrong.
func filterTencentFormingBar(bars []Bar, include bool, now time.Time) []Bar {
	if include || len(bars) == 0 {
		return bars
	}
	last := bars[len(bars)-1].Ts.In(tencentDateLocation)
	today := now.In(tencentDateLocation)
	if last.Year() == today.Year() && last.YearDay() == today.YearDay() {
		return bars[:len(bars)-1]
	}
	return bars
}

func (s TencentSource) get(ctx context.Context, requestURL string) ([]byte, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt <= len(tencentRetryBackoff); attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(tencentRetryBackoff[attempt-1])
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		if err := tencentRequestLimit.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("ingest: tencent source: request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "wbot/tencent-datafill")
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("request: %w", err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}
		if closeErr != nil {
			lastErr = fmt.Errorf("close response: %w", closeErr)
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}
		message := strings.TrimSpace(string(body))
		if len(message) > 256 {
			message = message[:256]
		}
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, message)
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			break
		}
	}
	return nil, fmt.Errorf("ingest: tencent source: %w", lastErr)
}

func parseTencentBar(raw json.RawMessage) (Bar, error) {
	var row []json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil {
		return Bar{}, fmt.Errorf("bad row: %w", err)
	}
	if len(row) < 6 {
		return Bar{}, fmt.Errorf("need at least 6 fields, got %d", len(row))
	}
	fields := make([]string, 6)
	for i := range fields {
		if err := json.Unmarshal(row[i], &fields[i]); err != nil {
			return Bar{}, fmt.Errorf("field %d is not a string: %w", i, err)
		}
	}
	ts, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(fields[0]), tencentDateLocation)
	if err != nil {
		return Bar{}, fmt.Errorf("bad date %q: %w", fields[0], err)
	}
	parsePrice := func(name, raw string) (float64, error) {
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return 0, fmt.Errorf("bad %s %q", name, raw)
		}
		return value, nil
	}
	open, err := parsePrice("open", fields[1])
	if err != nil {
		return Bar{}, err
	}
	closePrice, err := parsePrice("close", fields[2])
	if err != nil {
		return Bar{}, err
	}
	high, err := parsePrice("high", fields[3])
	if err != nil {
		return Bar{}, err
	}
	low, err := parsePrice("low", fields[4])
	if err != nil {
		return Bar{}, err
	}
	volumeFloat, err := strconv.ParseFloat(strings.TrimSpace(fields[5]), 64)
	if err != nil || math.IsNaN(volumeFloat) || math.IsInf(volumeFloat, 0) || volumeFloat < 0 || math.Trunc(volumeFloat) != volumeFloat || volumeFloat > math.MaxInt64 {
		return Bar{}, fmt.Errorf("bad volume %q", fields[5])
	}
	return Bar{
		Ts: ts.UTC(), Open: open, High: high, Low: low, Close: closePrice, Volume: int64(volumeFloat),
	}, nil
}
