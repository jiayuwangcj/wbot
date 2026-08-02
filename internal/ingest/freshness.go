package ingest

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// FreshnessStatus is the tri-state staleness of a symbol×timeframe combination.
type FreshnessStatus string

const (
	Fresh   FreshnessStatus = "fresh"
	Stale   FreshnessStatus = "stale"
	Unknown FreshnessStatus = "unknown"
)

// Freshness is one symbol×timeframe×adjust combination's max_ts age at a query's now.
type Freshness struct {
	Symbol     string
	Timeframe  string
	Adjust     string
	MaxTs      time.Time // zero when the combination has no bars
	AgeSeconds int64     // whole seconds between MaxTs and now (0 for future timestamps)
}

// JudgeFreshness classifies a combination's max_ts age against maxAge:
// no data (zero max_ts) → unknown; age ≤ maxAge → fresh (boundary inclusive);
// age > maxAge → stale (doc/DATA_PIPELINE.md).
func JudgeFreshness(maxTs time.Time, now time.Time, maxAge time.Duration) FreshnessStatus {
	if maxTs.IsZero() {
		return Unknown
	}
	if now.Sub(maxTs) <= maxAge {
		return Fresh
	}
	return Stale
}

// MaxAgeForTimeframe maps a bars.timeframe value to its default staleness
// threshold: 3× the nominal bar interval, floored at 10 minutes — 1d → 3 days,
// 1m → 10 minutes (freshness issue draft); unparseable names fall back to 24h.
func MaxAgeForTimeframe(timeframe string) time.Duration {
	d, ok := parseBarInterval(timeframe)
	if !ok {
		return 24 * time.Hour
	}
	if t := 3 * d; t >= 10*time.Minute {
		return t
	}
	return 10 * time.Minute
}

// parseBarInterval maps a bars.timeframe value to its nominal bar interval
// (convention, doc/DATA_PIPELINE.md: 1m 5m 15m 30m 60m 1d 1w 1mo).
func parseBarInterval(timeframe string) (time.Duration, bool) {
	suffixes := []struct {
		suf  string
		mult time.Duration
	}{
		{"mo", 30 * 24 * time.Hour},
		{"w", 7 * 24 * time.Hour},
		{"d", 24 * time.Hour},
		{"h", time.Hour},
		{"m", time.Minute},
	}
	tf := strings.TrimSpace(timeframe)
	for _, s := range suffixes {
		if !strings.HasSuffix(tf, s.suf) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(tf, s.suf))
		if err != nil || n <= 0 {
			return 0, false
		}
		return time.Duration(n) * s.mult, true
	}
	return 0, false
}

// QueryFreshness computes max_ts ages for every symbol×timeframe×adjust
// combination in the bars table against now; combinations without bars are
// absent (no data → unknown, doc/DATA_PIPELINE.md).
func QueryFreshness(ctx context.Context, db *sql.DB, now time.Time) ([]Freshness, error) {
	coverage, err := QueryBarCoverage(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make([]Freshness, 0, len(coverage))
	for _, c := range coverage {
		age := now.Sub(c.MaxTs)
		if age < 0 {
			age = 0
		}
		out = append(out, Freshness{
			Symbol: c.Symbol, Timeframe: c.Timeframe, Adjust: c.Adjust,
			MaxTs: c.MaxTs, AgeSeconds: int64(age.Seconds()),
		})
	}
	return out, nil
}

// MaxAgeForOptions is the default staleness threshold for option quotes
// (option_quotes, no timeframe): intraday market data, fresh within 4 hours
// (freshness draft extension; -max-age overrides globally).
const MaxAgeForOptions = 4 * time.Hour

// OptionFreshness is one underlying×source combination's option_quotes
// max_ts age at a query's now.
type OptionFreshness struct {
	Underlying string
	Source     string
	MaxTs      time.Time // zero when the underlying has no quotes
	AgeSeconds int64     // whole seconds between MaxTs and now (0 for future timestamps)
}

// QueryOptionFreshness computes max_ts ages for every underlying×source
// combination in option_quotes against now; underlyings without quotes are
// absent (no data → unknown semantics, same as bars).
func QueryOptionFreshness(ctx context.Context, db *sql.DB, now time.Time) ([]OptionFreshness, error) {
	if db == nil {
		return nil, errors.New("nil db")
	}
	rows, err := db.QueryContext(ctx, `
SELECT underlying, source, MAX(ts) AS max_ts
FROM option_quotes
GROUP BY underlying, source
ORDER BY underlying, source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OptionFreshness{}
	for rows.Next() {
		var f OptionFreshness
		if err := rows.Scan(&f.Underlying, &f.Source, &f.MaxTs); err != nil {
			return nil, err
		}
		age := now.Sub(f.MaxTs)
		if age < 0 {
			age = 0
		}
		f.AgeSeconds = int64(age.Seconds())
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
