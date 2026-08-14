package backtestexec

// Unit tests for the shared CLI/API run contract: Build validation and
// SaveParams shape. Run's DB paths are covered by the httpapi integration
// tests (real PostgreSQL, WBOT_PG_DSN).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/strategy"
	"github.com/jiayu/wbot/internal/wheel"
)

func TestBuild(t *testing.T) {
	wheelParams := func() map[string]any {
		return map[string]any{
			"full_position_price": 100.0,
			"zero_position_price": 200.0,
			"max_inventory":       1000.0,
		}
	}
	unknown := wheelParams()
	unknown["bogus"] = 1
	wrongType := wheelParams()
	wrongType["max_inventory"] = "1000"
	outOfRange := wheelParams()
	outOfRange["max_inventory"] = -1
	tests := []struct {
		name      string
		strategy  string
		params    map[string]any
		wantTempl bool
		wantErr   string
	}{
		{"hold", "hold", nil, false, ""},
		{"buy-hold", "buy-hold", map[string]any{}, false, ""},
		{"hold rejects params", "hold", map[string]any{"a": 1}, false, "no params"},
		{"buy-hold rejects params", "buy-hold", map[string]any{"a": 1}, false, "no params"},
		{"wheel", "wheel", wheelParams(), true, ""},
		{"wheel defaults", "wheel", wheelParams(), true, ""},
		{"unknown strategy", "nope", nil, false, "unknown template"},
		{"unknown param", "wheel", unknown, false, "unknown param"},
		{"wrong type", "wheel", wrongType, false, "want a number"},
		{"out of range", "wheel", outOfRange, false, "want in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, templ, err := Build(tt.strategy, tt.params)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Build(%q) err = %v; want containing %q", tt.strategy, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Build(%q) err = %v; want nil", tt.strategy, err)
			}
			if s == nil {
				t.Fatalf("Build(%q) strategy = nil; want a strategy", tt.strategy)
			}
			if (templ != nil) != tt.wantTempl {
				t.Fatalf("Build(%q) templ = %v; want templ present = %v", tt.strategy, templ, tt.wantTempl)
			}
			if tt.strategy == "hold" || tt.strategy == "buy-hold" {
				if _, ok := s.(backtest.HoldStrategy); !ok {
					if _, ok := s.(*backtest.BuyHoldStrategy); !ok {
						t.Fatalf("Build(%q) strategy type = %T; want hold/buy-hold", tt.strategy, s)
					}
				}
			}
		})
	}
}

func TestQuoteRangeStartIncludesFreshPreBarSnapshot(t *testing.T) {
	from := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	wheelStrategy := &strategy.WheelStrategy{Config: wheel.Config{MaxQuoteAgeSeconds: 3600}}
	if got, want := quoteRangeStart(from, wheelStrategy), from.Add(-time.Hour); !got.Equal(want) {
		t.Fatalf("quoteRangeStart = %s; want %s", got, want)
	}
	if got := quoteRangeStart(from, backtest.HoldStrategy{}); !got.Equal(from) {
		t.Fatalf("benchmark quoteRangeStart = %s; want unchanged %s", got, from)
	}
}

func TestSaveParams(t *testing.T) {
	strategyParams := map[string]any{"max_inventory": 1200.0}
	got := SaveParams(Options{Cash: 10000, Fee: 1.5, Timeframe: "1d", Adjust: "fwd", Params: strategyParams})
	want := map[string]any{"cash": 10000.0, "fee": 1.5, "timeframe": "1d", "adjust": "fwd"}
	if len(got) != len(want)+1 {
		t.Fatalf("SaveParams = %v; want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("SaveParams[%q] = %v; want %v", k, got[k], v)
		}
	}
	if got["strategy_params"].(map[string]any)["max_inventory"] != 1200.0 {
		t.Fatalf("SaveParams strategy_params = %v; want reproducible Wheel config", got["strategy_params"])
	}
}

func TestSaveParamsIncludesOnlyRealConfigVersion(t *testing.T) {
	version := 2
	got := SaveParams(Options{Cash: 10000, ConfigVersion: &version})
	if got["config_version"] != 2 {
		t.Fatalf("config_version = %#v; want 2", got["config_version"])
	}
	if _, ok := SaveParams(Options{Cash: 10000})["config_version"]; ok {
		t.Fatal("ad-hoc params persisted a fabricated config_version")
	}
}

func TestOptionsDataForRunAllowsZeroSnapshots(t *testing.T) {
	opts, err := optionsDataForRun(nil, 42)
	if err != nil {
		t.Fatal(err)
	}
	if opts == nil || opts.RunSeed != 42 || len(opts.QuoteBatches) != 0 {
		t.Fatalf("zero snapshot options = %+v; want non-nil DATA_BLOCKED input", opts)
	}
}

func TestRunPreparedIsDeterministicAndDoesNotShareRunSeed(t *testing.T) {
	ts := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	theta := -0.1
	prepared := &Prepared{
		bars: []ingest.Bar{{Ts: ts, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1}},
		options: &backtest.OptionsData{RunSeed: 999, Bars: backtest.OptionBars{"P95": {{Ts: ts, Open: 2, High: 2, Low: 2, Close: 2, Volume: 1}}}, QuoteBatches: []backtest.QuoteSnapshotBatch{{
			ObservedAt: ts, SnapshotKey: "batch", Underlying: "TEST.US", UnderlyingPrice: 100,
			Quotes: []wheel.OptionQuote{{Symbol: "P95", Code: "P95", Underlying: "TEST.US", Source: "test", OptionType: wheel.Put,
				Expiry: ts.AddDate(0, 0, 7), Strike: 95, Delta: -0.3, Bid: 2, Ask: 2.1, ImpliedVol: 0.2,
				Theta: &theta, Volume: 1, OpenInterest: 1, LotSize: 100, QuoteTime: ts}}, ExpiryOrder: []int{0},
		}}},
		sourceHash: "sha256-test",
	}
	o := Options{Symbol: "TEST.US", Strategy: "wheel", Params: map[string]any{
		"full_position_price": 90.0, "zero_position_price": 110.0, "max_inventory": 1000.0,
		"min_option_quality": 0.0, "trade_gap": 0.0,
	}, Cash: 20000, Seed: 123}
	first, err := prepared.RunPrepared(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepared.RunPrepared(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("same seed produced different prepared outcomes:\n%s\n%s", firstJSON, secondJSON)
	}
	if prepared.options.RunSeed != 999 {
		t.Fatalf("prepared RunSeed mutated to %d; want immutable seed 999", prepared.options.RunSeed)
	}
	if first.Result.Unfilled.AttemptCount != 1 {
		t.Fatalf("attempt count = %d; want one seed-controlled attempt", first.Result.Unfilled.AttemptCount)
	}
}

func TestRunRejectsNilDB(t *testing.T) {
	if _, err := Run(context.Background(), nil, Options{Symbol: "DEMO.US", Strategy: "hold"}); err == nil {
		t.Fatal("Run(nil db) err = nil; want error")
	}
}

func TestRunRejectsMissingInputs(t *testing.T) {
	for _, o := range []Options{
		{Symbol: "", Strategy: "hold"},
		{Symbol: "DEMO.US", Strategy: ""},
	} {
		if _, err := Run(context.Background(), nil, o); err == nil {
			t.Fatalf("Run(%+v) err = nil; want error", o)
		}
	}
}

// RunMulti's validation surface is unit-testable without a DB; the query/run
// path is covered by the cmd/wbot integration test (real PostgreSQL).
func TestRunMultiRejects(t *testing.T) {
	tests := []struct {
		name    string
		o       Options
		symbols []string
		wantErr string
	}{
		{"empty symbols", Options{Strategy: "hold"}, nil, "empty symbols"},
		{"no strategy", Options{}, []string{"A.US"}, "strategy is required"},
		{"unknown strategy", Options{Strategy: "nope"}, []string{"A.US"}, "unknown template"},
		{"option strategy", Options{Strategy: "wheel", Params: map[string]any{
			"full_position_price": 100.0,
			"zero_position_price": 200.0,
			"max_inventory":       1000.0,
		}}, []string{"A.US"}, "needs option_quotes"},
		{"hold rejects params", Options{Strategy: "hold", Params: map[string]any{"a": 1}}, []string{"A.US"}, "no params"},
		{"nil db", Options{Strategy: "hold"}, []string{"A.US"}, "nil db"},
		{"empty symbol", Options{Strategy: "hold"}, []string{""}, "empty symbol"},
		{"duplicate symbol", Options{Strategy: "hold"}, []string{"A.US", "A.US"}, "duplicate symbol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := RunMulti(context.Background(), nil, tt.o, tt.symbols); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("RunMulti(%+v, %v) err = %v; want containing %q", tt.o, tt.symbols, err, tt.wantErr)
			}
		})
	}
}
