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
