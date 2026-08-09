package datacheck

import (
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
)

func TestCheckReportsMissingAndStaleData(t *testing.T) {
	now := time.Date(2026, 8, 7, 17, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	policy := Policy{
		Timeframes: []string{"1m", "1d"},
		Adjusts:    []string{"none", "fwd"},
		Options:    true,
	}
	report := Check([]string{"HK.00700"}, []ingest.BarCoverage{
		{Symbol: "HK.00700", Timeframe: "1m", Adjust: "none", MaxTs: now.Add(-5 * time.Minute)},
		{Symbol: "HK.00700", Timeframe: "1m", Adjust: "fwd", MaxTs: now.Add(-20 * time.Minute)},
		{Symbol: "HK.00700", Timeframe: "1d", Adjust: "none", MaxTs: now.Add(-24 * time.Hour)},
	}, []OptionCoverage{{Underlying: "HK.00700", MaxTs: now.Add(-5 * time.Hour), MaxExpiry: now.AddDate(0, 1, 0)}}, now, policy)

	if report.Complete() {
		t.Fatal("Complete() = true; want false")
	}
	if got, want := report.Total, 5; got != want {
		t.Fatalf("Total = %d; want %d", got, want)
	}
	if got, want := report.Missing, 1; got != want {
		t.Fatalf("Missing = %d; want %d", got, want)
	}
	if got, want := report.Stale, 2; got != want {
		t.Fatalf("Stale = %d; want %d", got, want)
	}
	assertItemState(t, report, "bars", "1m", "fwd", StateStale)
	assertItemState(t, report, "bars", "1d", "fwd", StateMissing)
	assertItemState(t, report, "options", "", "", StateStale)
}

func TestCheckCompleteAndExpiredOptionChain(t *testing.T) {
	now := time.Date(2026, 8, 7, 17, 30, 0, 0, time.UTC)
	policy := Policy{Timeframes: []string{"1d"}, Adjusts: []string{"fwd"}, Options: true}
	bars := []ingest.BarCoverage{{Symbol: "US.AAPL", Timeframe: "1d", Adjust: "fwd", MaxTs: now.Add(-time.Hour)}}

	fresh := Check([]string{"US.AAPL"}, bars, []OptionCoverage{{Underlying: "US.AAPL", MaxTs: now.Add(-time.Hour), MaxExpiry: now.AddDate(0, 0, 7)}}, now, policy)
	if !fresh.Complete() {
		t.Fatalf("fresh report = %+v; want complete", fresh)
	}

	expired := Check([]string{"US.AAPL"}, bars, []OptionCoverage{{Underlying: "US.AAPL", MaxTs: now.Add(-time.Hour), MaxExpiry: now.AddDate(0, 0, -1)}}, now, policy)
	assertItemState(t, expired, "options", "", "", StateStale)
}

func TestDefaultPolicyCoversFetchableMatrix(t *testing.T) {
	p := DefaultPolicy()
	if got, want := len(p.Timeframes), 8; got != want {
		t.Fatalf("timeframes = %d; want %d", got, want)
	}
	if got, want := len(p.Adjusts), 3; got != want {
		t.Fatalf("adjusts = %d; want %d", got, want)
	}
	if !p.Options {
		t.Fatal("Options = false; want true")
	}
	if !p.SessionAware {
		t.Fatal("SessionAware = false; want true")
	}
}

func TestDefaultPolicyUsesLatestExpectedMarketWeekday(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 17, 30, 0, 0, shanghai) // Monday; US has not opened.
	policy := DefaultPolicy()
	policy.Timeframes = []string{"1d"}
	policy.Adjusts = []string{"fwd"}
	policy.Options = false

	hkFriday := time.Date(2026, 8, 7, 16, 0, 0, 0, shanghai)
	hk := Check([]string{"HK.00700"}, []ingest.BarCoverage{{Symbol: "HK.00700", Timeframe: "1d", Adjust: "fwd", MaxTs: hkFriday}}, nil, now, policy)
	assertItemState(t, hk, "bars", "1d", "fwd", StateStale)

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	usFriday := time.Date(2026, 8, 7, 16, 0, 0, 0, newYork)
	us := Check([]string{"US.AAPL"}, []ingest.BarCoverage{{Symbol: "US.AAPL", Timeframe: "1d", Adjust: "fwd", MaxTs: usFriday}}, nil, now, policy)
	assertItemState(t, us, "bars", "1d", "fwd", StateComplete)
}

func TestExchangeCalendarSkipsMainlandHoliday(t *testing.T) {
	shanghai := mustLocation(t, "Asia/Shanghai")
	now := time.Date(2026, 1, 2, 18, 0, 0, 0, shanghai)
	if got, want := expectedSessionDate(now, "SH.600000"), 20251231; got != want {
		t.Fatalf("expected session = %d; want %d", got, want)
	}
}

func TestExchangeCalendarHonorsHKHalfDay(t *testing.T) {
	hongKong := mustLocation(t, "Asia/Hong_Kong")
	beforeReady := time.Date(2026, 2, 16, 12, 29, 0, 0, hongKong)
	if got, want := expectedSessionDate(beforeReady, "HK.00700"), 20260213; got != want {
		t.Fatalf("before half-day ready = %d; want %d", got, want)
	}
	afterReady := time.Date(2026, 2, 16, 12, 30, 0, 0, hongKong)
	if got, want := expectedSessionDate(afterReady, "HK.00700"), 20260216; got != want {
		t.Fatalf("after half-day ready = %d; want %d", got, want)
	}
}

func TestExchangeCalendarHonorsNYSEEarlyClose(t *testing.T) {
	newYork := mustLocation(t, "America/New_York")
	beforeReady := time.Date(2026, 11, 27, 13, 29, 0, 0, newYork)
	if got, want := expectedSessionDate(beforeReady, "US.AAPL"), 20261125; got != want {
		t.Fatalf("before early-close ready = %d; want %d", got, want)
	}
	afterReady := time.Date(2026, 11, 27, 13, 30, 0, 0, newYork)
	if got, want := expectedSessionDate(afterReady, "US.AAPL"), 20261127; got != want {
		t.Fatalf("after early-close ready = %d; want %d", got, want)
	}
}

func TestExchangeCalendarUsesUSDST(t *testing.T) {
	for _, test := range []struct {
		name string
		now  time.Time
		want int
	}{
		{name: "standard time", now: time.Date(2026, 3, 6, 21, 30, 0, 0, time.UTC), want: 20260306},
		{name: "daylight time", now: time.Date(2026, 3, 9, 20, 30, 0, 0, time.UTC), want: 20260309},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := expectedSessionDate(test.now, "US.AAPL"); got != test.want {
				t.Fatalf("expected session = %d; want %d", got, test.want)
			}
		})
	}
}

func TestPolicyAcceptsInjectedCalendar(t *testing.T) {
	shanghai := mustLocation(t, "Asia/Shanghai")
	now := time.Date(2026, 8, 10, 18, 0, 0, 0, shanghai)
	policy := Policy{
		Timeframes:   []string{"1d"},
		Adjusts:      []string{"fwd"},
		SessionAware: true,
		Calendar: calendarFunc(func(_ string, date time.Time) MarketSession {
			return MarketSession{TradingDay: dateKey(date) == 20260807, ReadyHour: 15, ReadyMinute: 30}
		}),
	}
	report := Check([]string{"SH.600000"}, []ingest.BarCoverage{{
		Symbol: "SH.600000", Timeframe: "1d", Adjust: "fwd",
		MaxTs: time.Date(2026, 8, 7, 15, 0, 0, 0, shanghai),
	}}, nil, now, policy)
	assertItemState(t, report, "bars", "1d", "fwd", StateComplete)
}

type calendarFunc func(string, time.Time) MarketSession

func (f calendarFunc) Session(symbol string, date time.Time) MarketSession { return f(symbol, date) }

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func assertItemState(t *testing.T, report Report, kind, timeframe, adjust string, want State) {
	t.Helper()
	for _, item := range report.Items {
		if item.Kind == kind && item.Timeframe == timeframe && item.Adjust == adjust {
			if item.State != want {
				t.Fatalf("%s/%s/%s state = %s; want %s", kind, timeframe, adjust, item.State, want)
			}
			return
		}
	}
	t.Fatalf("missing report item %s/%s/%s", kind, timeframe, adjust)
}
