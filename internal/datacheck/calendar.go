package datacheck

import (
	"strings"
	"time"
)

// Calendar describes whether a market is open and when its data should be
// available. Implementations must be safe to call without network access.
type Calendar interface {
	Session(symbol string, date time.Time) MarketSession
}

// MarketSession is one exchange day. ReadyHour and ReadyMinute include the
// ingestion grace period after the official close.
type MarketSession struct {
	TradingDay  bool
	ReadyHour   int
	ReadyMinute int
}

// ExchangeCalendar contains the checked-in official 2026 exchange calendars.
// Dates outside the embedded coverage safely fall back to Monday-Friday and
// the market's normal close; callers can inject a newer Calendar through Policy.
type ExchangeCalendar struct{}

// NewExchangeCalendar returns the offline default calendar.
func NewExchangeCalendar() ExchangeCalendar { return ExchangeCalendar{} }

func (ExchangeCalendar) Session(symbol string, date time.Time) MarketSession {
	market := marketForSymbol(symbol)
	hour, minute := normalReadyTime(market)
	session := MarketSession{
		TradingDay:  date.Weekday() != time.Saturday && date.Weekday() != time.Sunday,
		ReadyHour:   hour,
		ReadyMinute: minute,
	}
	if !session.TradingDay || date.Year() != 2026 {
		return session
	}

	key := dateKey(date)
	switch market {
	case marketUS:
		// https://www.nyse.com/markets/hours-calendars
		if containsDate(nyseClosed2026, key) {
			session.TradingDay = false
		} else if containsDate(nyseEarlyClose2026, key) {
			session.ReadyHour, session.ReadyMinute = 13, 30
		}
	case marketHK:
		// https://www.hkex.com.hk/-/media/HKEX-Market/Services/Circulars-and-Notices/Participant-and-Members-Circulars/SEHK/2025/ce_SEHK_CT_075_2025.pdf
		if containsDate(hkexClosed2026, key) {
			session.TradingDay = false
		} else if containsDate(hkexHalfDay2026, key) {
			session.ReadyHour, session.ReadyMinute = 12, 30
		}
	default:
		// SSE and SZSE publish the same 2026 securities-market closures.
		// https://www.sse.com.cn/disclosure/announcement/general/c/c_20251222_10802507.shtml
		// https://www.szse.cn/disclosure/notice/t20251222_618087.html
		if containsDate(mainlandClosed2026, key) {
			session.TradingDay = false
		}
	}
	return session
}

type market string

const (
	marketMainland market = "mainland"
	marketHK       market = "hk"
	marketUS       market = "us"
)

func marketForSymbol(symbol string) market {
	prefix, _, _ := strings.Cut(symbol, ".")
	switch strings.ToUpper(prefix) {
	case "US":
		return marketUS
	case "HK":
		return marketHK
	default:
		return marketMainland
	}
}

func normalReadyTime(market market) (int, int) {
	if market == marketMainland {
		return 15, 30
	}
	return 16, 30
}

func containsDate(dates []int, target int) bool {
	for _, date := range dates {
		if date == target {
			return true
		}
	}
	return false
}

var mainlandClosed2026 = []int{
	20260101, 20260102,
	20260216, 20260217, 20260218, 20260219, 20260220, 20260223,
	20260406,
	20260501, 20260504, 20260505,
	20260619,
	20260925,
	20261001, 20261002, 20261005, 20261006, 20261007,
}

var hkexClosed2026 = []int{
	20260101,
	20260217, 20260218, 20260219,
	20260403, 20260406, 20260407,
	20260501, 20260525,
	20260619,
	20260701,
	20261001, 20261019,
	20261225,
}

var hkexHalfDay2026 = []int{20260216, 20261224, 20261231}

var nyseClosed2026 = []int{
	20260101, 20260119, 20260216, 20260403, 20260525,
	20260619, 20260703, 20260907, 20261126, 20261225,
}

var nyseEarlyClose2026 = []int{20261127, 20261224}
