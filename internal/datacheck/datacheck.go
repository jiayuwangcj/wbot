// Package datacheck checks that every watchlist symbol has the market data
// matrix needed by ingestion and backtesting.
package datacheck

import (
	"sort"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

// State is one required data set's completeness state.
type State string

const (
	StateComplete State = "complete"
	StateMissing  State = "missing"
	StateStale    State = "stale"
)

// Policy describes the data matrix checked for every watchlist symbol.
type Policy struct {
	Timeframes []string
	Adjusts    []string
	Options    bool
	// Calendar may override the checked-in offline exchange calendar. A nil
	// value uses NewExchangeCalendar.
	Calendar Calendar
	// SessionAware compares intraday/daily timestamps with the latest expected
	// market weekday instead of wall-clock ages. This avoids declaring US data
	// stale before that market opens in the process's local timezone.
	SessionAware bool
}

// DefaultPolicy is the full matrix supported by the Futu gateway.
func DefaultPolicy() Policy {
	return Policy{
		Timeframes:   []string{"1m", "5m", "15m", "30m", "60m", "1d", "1w", "1mo"},
		Adjusts:      []string{"none", "fwd", "back"},
		Options:      true,
		SessionAware: true,
	}
}

// OptionCoverage is the latest stored chain data for one underlying. MaxExpiry
// guards against a fresh timestamp that only belongs to an expired chain.
type OptionCoverage struct {
	Underlying string
	MaxTs      time.Time
	MaxExpiry  time.Time
}

// Item is one required bars series or option chain in a report.
type Item struct {
	Symbol     string    `json:"symbol"`
	Kind       string    `json:"kind"`
	Timeframe  string    `json:"timeframe,omitempty"`
	Adjust     string    `json:"adjust,omitempty"`
	State      State     `json:"state"`
	MaxTs      time.Time `json:"max_ts,omitzero"`
	MaxExpiry  time.Time `json:"max_expiry,omitzero"`
	AgeSeconds int64     `json:"age_seconds,omitempty"`
}

// Report is a deterministic snapshot of watchlist completeness.
type Report struct {
	CheckedAt time.Time `json:"checked_at"`
	Symbols   int       `json:"symbols"`
	Total     int       `json:"total"`
	Missing   int       `json:"missing"`
	Stale     int       `json:"stale"`
	Items     []Item    `json:"items"`
}

// Complete reports whether every required item is present and fresh.
func (r Report) Complete() bool { return r.Missing == 0 && r.Stale == 0 }

// Check compares the watchlist against stored bars and option coverage.
func Check(symbols []string, bars []ingest.BarCoverage, options []OptionCoverage, now time.Time, policy Policy) Report {
	barByKey := make(map[string]ingest.BarCoverage, len(bars))
	for _, bar := range bars {
		key := bar.Symbol + "\x00" + bar.Timeframe + "\x00" + bar.Adjust
		if old, ok := barByKey[key]; !ok || bar.MaxTs.After(old.MaxTs) {
			barByKey[key] = bar
		}
	}
	optionBySymbol := make(map[string]OptionCoverage, len(options))
	for _, option := range options {
		if old, ok := optionBySymbol[option.Underlying]; !ok || option.MaxTs.After(old.MaxTs) {
			optionBySymbol[option.Underlying] = option
		}
	}

	uniqueSymbols := uniqueSorted(symbols)
	report := Report{CheckedAt: now, Symbols: len(uniqueSymbols), Items: []Item{}}
	for _, symbol := range uniqueSymbols {
		for _, timeframe := range policy.Timeframes {
			for _, adjust := range policy.Adjusts {
				item := Item{Symbol: symbol, Kind: "bars", Timeframe: timeframe, Adjust: adjust, State: StateMissing}
				if coverage, ok := barByKey[symbol+"\x00"+timeframe+"\x00"+adjust]; ok {
					item.MaxTs = coverage.MaxTs
					item.AgeSeconds = ageSeconds(now, coverage.MaxTs)
					item.State = StateComplete
					if barsStale(symbol, timeframe, coverage.MaxTs, now, policy) {
						item.State = StateStale
					}
				}
				report.add(item)
			}
		}
		if policy.Options {
			item := Item{Symbol: symbol, Kind: "options", State: StateMissing}
			if coverage, ok := optionBySymbol[symbol]; ok {
				item.MaxTs = coverage.MaxTs
				item.MaxExpiry = coverage.MaxExpiry
				item.AgeSeconds = ageSeconds(now, coverage.MaxTs)
				item.State = StateComplete
				if optionsStale(symbol, coverage, now, policy) {
					item.State = StateStale
				}
			}
			report.add(item)
		}
	}
	return report
}

func (r *Report) add(item Item) {
	r.Total++
	switch item.State {
	case StateMissing:
		r.Missing++
	case StateStale:
		r.Stale++
	}
	r.Items = append(r.Items, item)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func ageSeconds(now, then time.Time) int64 {
	age := now.Sub(then)
	if age < 0 {
		return 0
	}
	return int64(age.Seconds())
}

func expiryBefore(expiry, now time.Time) bool {
	if expiry.IsZero() {
		return true
	}
	y, m, d := now.Date()
	startOfDay := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return expiry.Before(startOfDay)
}

func barsStale(symbol, timeframe string, maxTs, now time.Time, policy Policy) bool {
	if policy.SessionAware && timeframe != "1w" && timeframe != "1mo" {
		return marketDate(maxTs, symbol) < expectedSessionDateWithCalendar(now, symbol, policy.Calendar)
	}
	return ingest.JudgeFreshness(maxTs, now, ingest.MaxAgeForTimeframe(timeframe)) != ingest.Fresh
}

func optionsStale(symbol string, coverage OptionCoverage, now time.Time, policy Policy) bool {
	stale := ingest.JudgeFreshness(coverage.MaxTs, now, ingest.MaxAgeForOptions) != ingest.Fresh
	if policy.SessionAware {
		stale = marketDate(coverage.MaxTs, symbol) < expectedSessionDateWithCalendar(now, symbol, policy.Calendar)
	}
	return stale || expiryBefore(coverage.MaxExpiry, now)
}

func expectedSessionDate(now time.Time, symbol string) int {
	return expectedSessionDateWithCalendar(now, symbol, nil)
}

func expectedSessionDateWithCalendar(now time.Time, symbol string, calendar Calendar) int {
	loc := marketLocation(symbol)
	marketNow := now.In(loc)
	if calendar == nil {
		calendar = NewExchangeCalendar()
	}

	candidate := marketNow
	session := calendar.Session(symbol, candidate)
	y, m, d := candidate.Date()
	readyAt := time.Date(y, m, d, session.ReadyHour, session.ReadyMinute, 0, 0, loc)
	if !session.TradingDay || marketNow.Before(readyAt) {
		candidate = candidate.AddDate(0, 0, -1)
	}
	for scanned := 0; scanned < 370; scanned++ {
		if calendar.Session(symbol, candidate).TradingDay {
			return dateKey(candidate)
		}
		candidate = candidate.AddDate(0, 0, -1)
	}
	// A broken injected calendar must not hang a report. Fall back to the
	// checked-in calendar, which itself degrades to weekdays outside 2026.
	return expectedSessionDateWithCalendar(now, symbol, NewExchangeCalendar())
}

func marketDate(value time.Time, symbol string) int {
	return dateKey(value.In(marketLocation(symbol)))
}

func marketLocation(symbol string) *time.Location {
	name := "Asia/Shanghai"
	switch marketForSymbol(symbol) {
	case marketUS:
		name = "America/New_York"
	case marketHK:
		name = "Asia/Hong_Kong"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc = time.UTC
	}
	return loc
}

func dateKey(value time.Time) int {
	y, m, d := value.Date()
	return y*10000 + int(m)*100 + d
}
