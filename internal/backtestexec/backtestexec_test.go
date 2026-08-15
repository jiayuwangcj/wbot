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
	"github.com/jiayu/wbot/internal/backtestes"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/strategy"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
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

func TestBuildWheelInjectsTradingDayCalendar(t *testing.T) {
	s, _, err := Build("wheel", map[string]any{
		"full_position_price": 100.0,
		"zero_position_price": 200.0,
		"max_inventory":       1000.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	ws, ok := s.(*strategy.WheelStrategy)
	if !ok {
		t.Fatalf("Build(wheel) type = %T; want *strategy.WheelStrategy", s)
	}
	if ws.Config.Calendar == nil {
		t.Fatal("backtest Build must inject a trading-day calendar so a daily-close snapshot stays fresh on the next trading day")
	}
	if ws.Config.Calendar.IsTradingDay("HK.00700", time.Date(2026, 8, 8, 4, 0, 0, 0, time.UTC)) {
		t.Fatal("Saturday must not be a trading day")
	}
	if !ws.Config.Calendar.IsTradingDay("HK.00700", time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)) {
		t.Fatal("Monday must be a trading day")
	}
}

func TestWheelBuildMondayBarAcrossWeekend(t *testing.T) {
	// Friday 2026-08-07 16:00 HKT daily-close snapshot (UTC 08:00); Monday
	// 2026-08-10 09:30 HKT bar (UTC 01:30). 65.5h exceeds the 24h wall-clock
	// max, but the trading-day exemption keeps the close fresh for Monday.
	fridayClose := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	mondayBar := time.Date(2026, 8, 10, 1, 30, 0, 0, time.UTC)

	bid := 2.0
	delta, ask, iv, theta, underlying := -0.30, 2.10, 0.20, -0.10, 100.0
	volume, oi, lot := int64(1000), int64(2000), int64(100)
	row := wheelstore.QuoteSnapshotRecord{
		Symbol: "HK.00700-P95", Underlying: "HK.00700", OptionType: "PUT", Strike: 95,
		Expiry: mondayBar.AddDate(0, 0, 30), Source: "test", SnapshotKey: "batch-1",
		UnderlyingPrice: &underlying, Delta: &delta, Bid: &bid, Ask: &ask, IV: &iv, Theta: &theta,
		Volume: &volume, OpenInterest: &oi, LotSize: &lot, ObservedAt: fridayClose,
	}
	data, err := backtest.OptionsDataFromQuoteSnapshots([]wheelstore.QuoteSnapshotRecord{row})
	if err != nil {
		t.Fatal(err)
	}
	bars := []ingest.Bar{{Ts: mondayBar, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1}}
	params := map[string]any{
		"full_position_price": 90.0,
		"zero_position_price": 110.0,
		"max_inventory":       1000.0,
		"max_dte":             45.0,
	}
	s, _, err := Build("wheel", params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := backtest.RunOptions(context.Background(), bars, 20000, 0, s, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Signals) != 1 || res.Signals[0].CapabilityStatus != wheel.CapabilityReady || res.Signals[0].Action != wheel.ActionAlert {
		t.Fatalf("Monday bar with Friday close snapshot: signals=%+v; want ALERT CapabilityReady (trading-day fresh)", res.Signals)
	}
	// Negative control: the same run built without the injected calendar keeps
	// the strict wall-clock rule, so the Friday close is stale on Monday.
	raw, err := strategy.Factory("wheel", params)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := backtest.RunOptions(context.Background(), bars, 20000, 0, raw, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Signals) != 1 || blocked.Signals[0].CapabilityStatus != wheel.CapabilityDataBlocked {
		t.Fatalf("Monday bar without calendar: signals=%+v; want DATA_BLOCKED (wall-clock stale)", blocked.Signals)
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

func TestSaveParamsIncludesTypedFeeModel(t *testing.T) {
	model := backtest.HKFeeModel(21, 70, 100)
	got := SaveParams(Options{Cash: 1_000_000, Fee: 3, FeeModel: &model})
	for key, want := range map[string]any{
		"fee":                     3.0,
		"fee_option_per_contract": 21.0,
		"fee_stock_per_lot":       70.0,
		"lot_size":                100,
	} {
		if got[key] != want {
			t.Fatalf("SaveParams[%q] = %#v; want %#v (all=%v)", key, got[key], want, got)
		}
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

func TestRunPreparedConcurrentEvaluationsMatchSerial(t *testing.T) {
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
		sourceHash: "sha256-concurrent-test",
	}
	opts := Options{Symbol: "TEST.US", Strategy: "wheel", Params: map[string]any{
		"full_position_price": 90.0, "zero_position_price": 110.0, "max_inventory": 1000.0,
		"min_option_quality": 0.0, "trade_gap": 0.0,
	}, Cash: 20000, Seed: 777}
	tasks := make([]func(context.Context) (*Outcome, error), 16)
	for i := range tasks {
		tasks[i] = func(ctx context.Context) (*Outcome, error) {
			return prepared.RunPrepared(ctx, opts)
		}
	}
	parallel, err := backtestes.ParallelMap(context.Background(), tasks)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := prepared.RunPrepared(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(serial)
	if err != nil {
		t.Fatal(err)
	}
	for i, got := range parallel {
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != string(want) {
			t.Fatalf("parallel result[%d] differs from serial:\nparallel=%s\nserial=%s", i, encoded, want)
		}
	}
	if prepared.options.RunSeed != 999 {
		t.Fatalf("prepared RunSeed mutated to %d; want immutable seed 999", prepared.options.RunSeed)
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

// TestSourceHashStreamMatchesSourceHash proves the streaming digest the chunked
// path feeds is byte-identical to the single-call sourceHash, both in one pass
// and when bars/batches are split into chunks with overlapping lookback regions.
func TestSourceHashStreamMatchesSourceHash(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := start.AddDate(0, 0, 30)
	theta := -0.1
	var bars []ingest.Bar
	var rows []wheelstore.QuoteSnapshotRecord
	for day := 0; day < 12; day++ {
		ts := start.AddDate(0, 0, day)
		bars = append(bars, ingest.Bar{Ts: ts, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1, Source: "test", Adjusted: "none"})
		for _, p := range []struct {
			sym    string
			strike float64
			kind   wheel.OptionType
		}{
			{"P95", 95, wheel.Put}, {"C105", 105, wheel.Call},
		} {
			bid, delta := 2.0, -0.30
			if p.kind == wheel.Call {
				delta = 0.30
			}
			rows = append(rows, wheelstore.QuoteSnapshotRecord{
				Symbol: p.sym, Underlying: "U.US", OptionType: string(p.kind), Strike: p.strike, Expiry: expiry,
				Source: "test", SnapshotKey: "batch-1", UnderlyingPrice: floatPtr(100), Delta: &delta,
				Bid: &bid, Ask: floatPtr(2.1), IV: floatPtr(0.2), Theta: &theta,
				Volume: int64Ptr(1000), OpenInterest: int64Ptr(2000), LotSize: int64Ptr(100), ObservedAt: ts,
			})
		}
	}
	opts, err := backtest.OptionsDataFromQuoteSnapshots(rows)
	if err != nil {
		t.Fatal(err)
	}
	want, err := sourceHash(bars, opts)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("single pass", func(t *testing.T) {
		h := newSourceHashStream()
		h.addBars(bars)
		h.addOwnedBatches(opts, time.Time{}, map[batchKey]struct{}{})
		got, err := h.digest()
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("stream digest = %s; want %s", got, want)
		}
	})

	t.Run("chunked with overlap", func(t *testing.T) {
		h := newSourceHashStream()
		seen := map[batchKey]struct{}{}
		for startIdx := 0; startIdx < len(bars); startIdx += 5 {
			end := startIdx + 5
			if end > len(bars) {
				end = len(bars)
			}
			chunkBars := bars[startIdx:end]
			chunkFrom := chunkBars[0].Ts.Add(-24 * time.Hour)
			chunkTo := chunkBars[len(chunkBars)-1].Ts
			var chunkRows []wheelstore.QuoteSnapshotRecord
			for _, r := range rows {
				if !r.ObservedAt.Before(chunkFrom) && !r.ObservedAt.After(chunkTo) {
					chunkRows = append(chunkRows, r)
				}
			}
			chunkOpts, err := backtest.OptionsDataFromQuoteSnapshots(chunkRows)
			if err != nil {
				t.Fatal(err)
			}
			ownedFrom := chunkBars[0].Ts
			if startIdx == 0 {
				ownedFrom = time.Time{}
			}
			h.addBars(chunkBars)
			h.addOwnedBatches(chunkOpts, ownedFrom, seen)
		}
		got, err := h.digest()
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("chunked stream digest = %s; want %s", got, want)
		}
	})

	t.Run("no option data", func(t *testing.T) {
		want, err := sourceHash(bars, nil)
		if err != nil {
			t.Fatal(err)
		}
		h := newSourceHashStream()
		h.addBars(bars)
		got, err := h.digest()
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("no-option stream digest = %s; want %s", got, want)
		}
	})
}

func floatPtr(v float64) *float64 { return &v }
func int64Ptr(v int64) *int64     { return &v }
