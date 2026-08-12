package wheelrun

import (
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/datacheck"
)

// MarketOpenFunc is injectable so the runner's wall-clock gate can be tested
// without waiting for an exchange session. Production uses marketIsOpen.
type MarketOpenFunc func(symbol string, now time.Time) bool

// marketIsOpen is deliberately small and offline. The calendar supplies
// weekends and exchange holidays; the intraday windows below are evaluated in
// the exchange's own timezone, which also handles US daylight-saving changes.
func marketIsOpen(symbol string, now time.Time, calendar datacheck.Calendar) bool {
	market := symbolMarket(symbol)
	loc := marketLocation(market)
	marketNow := now.In(loc)
	if calendar == nil {
		calendar = datacheck.NewExchangeCalendar()
	}
	session := calendar.Session(symbol, marketNow)
	if !session.TradingDay {
		return false
	}

	minute := marketNow.Hour()*60 + marketNow.Minute()
	closeMinute := normalCloseMinute(market)
	if session.ReadyHour > 0 || session.ReadyMinute > 0 {
		candidate := session.ReadyHour*60 + session.ReadyMinute - 30
		if candidate > 0 {
			closeMinute = candidate
		}
	}
	switch market {
	case "hk":
		// HKEX continuous sessions are 09:30-12:00 and 13:00-16:00 HKT.
		morningEnd := 12 * 60
		if closeMinute < morningEnd {
			morningEnd = closeMinute
		}
		return inWindow(minute, 9*60+30, morningEnd) || inWindow(minute, 13*60, closeMinute)
	case "us":
		// NYSE 09:30-16:00 America/New_York becomes 21:30-04:00 HKT
		// during summer and 22:30-05:00 HKT during standard time.
		return inWindow(minute, 9*60+30, closeMinute)
	default:
		// Keep the fallback useful for SH/SZ and future mainland symbols while
		// retaining the same lunch-break rule as the exchange sessions.
		morningEnd := 11*60 + 30
		if closeMinute < morningEnd {
			morningEnd = closeMinute
		}
		return inWindow(minute, 9*60+30, morningEnd) || inWindow(minute, 13*60, closeMinute)
	}
}

func inWindow(minute, begin, end int) bool { return minute >= begin && minute < end }

func normalCloseMinute(market string) int {
	if market == "mainland" {
		return 15 * 60
	}
	return 16 * 60
}

func symbolMarket(symbol string) string {
	prefix, _, _ := strings.Cut(symbol, ".")
	switch strings.ToUpper(prefix) {
	case "HK":
		return "hk"
	case "US":
		return "us"
	default:
		return "mainland"
	}
}

func marketLocation(market string) *time.Location {
	name := "Asia/Shanghai"
	switch market {
	case "hk":
		name = "Asia/Hong_Kong"
	case "us":
		name = "America/New_York"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
