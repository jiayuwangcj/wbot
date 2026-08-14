package backtest

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
	"github.com/jiayu/wbot/internal/wheelstore"
)

// mkBars builds valid bars with equal OHLC and daily timestamps from closes.
func mkBars(closes ...float64) []ingest.Bar {
	bars := make([]ingest.Bar, 0, len(closes))
	for i, c := range closes {
		bars = append(bars, ingest.Bar{
			Ts:     time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC),
			Open:   c,
			High:   c,
			Low:    c,
			Close:  c,
			Volume: 100,
		})
	}
	return bars
}

// stubStrategy returns fixed action/size/err for every bar.
type stubStrategy struct {
	action Action
	size   float64
	err    error
}

type signalStubStrategy struct{ signal wheel.Signal }

func (s signalStubStrategy) OnBar(_ context.Context, _ ingest.Bar, _ *State) (Action, float64, error) {
	return ActionHold, 0, nil
}

func (s signalStubStrategy) Signal() wheel.Signal { return s.signal }

func (s stubStrategy) OnBar(_ context.Context, _ ingest.Bar, _ *State) (Action, float64, error) {
	return s.action, s.size, s.err
}

func TestRunHold(t *testing.T) {
	bars := mkBars(100, 110, 90, 105)
	res, err := Run(context.Background(), bars, 10000, 0, HoldStrategy{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if res.Equity != 10000 || res.TotalReturn != 0 || res.MaxDrawdown != 0 || res.Bars != 4 {
		t.Fatalf("Run() = %+v; want Equity 10000, TotalReturn 0, MaxDrawdown 0, Bars 4", res)
	}
}

func TestRunBuyHold(t *testing.T) {
	bars := mkBars(100, 110, 121)
	res, err := Run(context.Background(), bars, 10000, 0, &BuyHoldStrategy{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	// All-in 100 shares at close 100, cash ~0: final equity = 100 * 121.
	if math.Abs(res.Equity-12100) > 1e-9 {
		t.Fatalf("Run().Equity = %v; want ~12100", res.Equity)
	}
	// TotalReturn = close_last/close_first - 1 = 0.21.
	if math.Abs(res.TotalReturn-0.21) > 1e-9 {
		t.Fatalf("Run().TotalReturn = %v; want ~0.21", res.TotalReturn)
	}
	if res.MaxDrawdown != 0 || res.Bars != 3 {
		t.Fatalf("Run() = %+v; want MaxDrawdown 0, Bars 3", res)
	}
}

func TestRunFee(t *testing.T) {
	// Buy-hold with fee=1: buy 100 at close 100 -> cash -1, final equity 12099 (hold pays no fee).
	bars := mkBars(100, 110, 121)
	res, err := Run(context.Background(), bars, 10000, 1, &BuyHoldStrategy{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if math.Abs(res.Equity-12099) > 1e-9 {
		t.Fatalf("Run().Equity = %v; want ~12099", res.Equity)
	}
	if !res.Fees.Included || res.Fees.PerTrade != 1 || res.Fees.TotalAmount != 1 || res.Fees.StockAmount != 1 || res.Fees.OptionAmount != 0 || res.Fees.ChargedTradeCount != 1 {
		t.Fatalf("Run().Fees = %+v; want one charged stock fill", res.Fees)
	}
	tm := res.Terminal
	if tm.ValuationStatus != ValuationComplete || tm.FinalEquityAmount == nil || *tm.FinalEquityAmount != 12099 ||
		tm.HoldingsMarketValueAmount == nil || *tm.HoldingsMarketValueAmount != 12100 || tm.StockAverageCost == nil || *tm.StockAverageCost != 100 ||
		tm.RealizedPnLAmount == nil || *tm.RealizedPnLAmount != -1 || tm.UnrealizedPnLAmount == nil || *tm.UnrealizedPnLAmount != 2100 {
		t.Fatalf("Run().Terminal = %+v; want complete stock mark with fee realized and price move unrealized", tm)
	}
	if res.Bars != 3 {
		t.Fatalf("Run() = %+v; want Bars 3", res)
	}

	resHold, err := Run(context.Background(), bars, 10000, 1, HoldStrategy{})
	if err != nil {
		t.Fatalf("Run() hold error: %v", err)
	}
	if resHold.Equity != 10000 || resHold.TotalReturn != 0 {
		t.Fatalf("Run() hold = %+v; want Equity 10000, TotalReturn 0 (hold settles charge no fee)", resHold)
	}
	if !resHold.Fees.Included || resHold.Fees.TotalAmount != 0 || resHold.Fees.ChargedTradeCount != 0 {
		t.Fatalf("Run() hold fees = %+v; want an enabled zero-charge fee model", resHold.Fees)
	}
}

func TestRunDataQualityZeroAndMissingSnapshots(t *testing.T) {
	blocked := signalStubStrategy{signal: wheel.Signal{
		Action: wheel.ActionHold, Direction: wheel.DirectionHold,
		CapabilityStatus: wheel.CapabilityDataBlocked, BlockedBy: []string{"option_quote_snapshots"},
	}}
	res, err := RunOptions(context.Background(), mkBars(100, 101), 10000, 0, blocked, &OptionsData{})
	if err != nil {
		t.Fatal(err)
	}
	q := res.DataQuality
	if q.Status != wheel.CapabilityDataBlocked || q.SnapshotBatchCount != 0 || q.SnapshotContractRowCount != 0 ||
		q.BlockedBarCount != 2 || q.ReadyBarCount != 0 || q.ValidCoverageRatio == nil || *q.ValidCoverageRatio != 0 ||
		q.HistoricalOptionCycleComplete == nil || *q.HistoricalOptionCycleComplete {
		t.Fatalf("zero snapshot quality = %+v", q)
	}
	if len(q.MissingRequiredFieldCounts) == 0 || q.MissingRequiredFieldCounts["bid"] != 0 {
		t.Fatalf("zero-row missing field counts = %+v; want explicit zero-valued dictionary", q.MissingRequiredFieldCounts)
	}

	missing := &OptionsData{QuoteBatches: []QuoteSnapshotBatch{{Quotes: []wheel.OptionQuote{{}}}}}
	res, err = RunOptions(context.Background(), mkBars(100), 10000, 0, blocked, missing)
	if err != nil {
		t.Fatal(err)
	}
	q = res.DataQuality
	if q.SnapshotBatchCount != 1 || q.SnapshotContractRowCount != 1 {
		t.Fatalf("missing snapshot counts = %+v", q)
	}
	for _, field := range []string{"bid", "ask", "delta", "theta", "source", "symbol", "snapshot_key", "underlying_price"} {
		if q.MissingRequiredFieldCounts[field] != 1 {
			t.Fatalf("missing %s count = %d; all counts %+v", field, q.MissingRequiredFieldCounts[field], q.MissingRequiredFieldCounts)
		}
	}
}

func TestLatestQuoteBatchSourcePriority(t *testing.T) {
	at := time.Date(2025, 7, 2, 8, 0, 0, 0, time.UTC)
	batch := func(source, key string, observed time.Time) QuoteSnapshotBatch {
		return QuoteSnapshotBatch{ObservedAt: observed, SnapshotKey: key, Quotes: []wheel.OptionQuote{{Source: source}}}
	}
	opts := &OptionsData{QuoteBatches: []QuoteSnapshotBatch{
		batch("alpha", "z", at),
		batch("hkex", "a", at),
		batch("futu", "a", at),
	}}
	if got := latestQuoteBatch(opts, at); got == nil || got.Quotes[0].Source != "futu" {
		t.Fatalf("same-time source = %+v; want futu", got)
	}
	opts.QuoteBatches = opts.QuoteBatches[:2]
	if got := latestQuoteBatch(opts, at); got == nil || got.Quotes[0].Source != "hkex" {
		t.Fatalf("same-time source without futu = %+v; want hkex", got)
	}
	opts.QuoteBatches = append(opts.QuoteBatches, batch("alpha", "old-source-newer-time", at.Add(time.Second)))
	if got := latestQuoteBatch(opts, at.Add(time.Second)); got == nil || got.ObservedAt != at.Add(time.Second) {
		t.Fatalf("newer observation = %+v; want timestamp to outrank provider", got)
	}
}

func TestOptionsDataRejectsMixedSourcesInAtomicBatch(t *testing.T) {
	at := time.Date(2025, 7, 2, 8, 0, 0, 0, time.UTC)
	rows := []wheelstore.QuoteSnapshotRecord{
		{Symbol: "P1", Underlying: "HK.00700", Source: "futu", SnapshotKey: "same", ObservedAt: at},
		{Symbol: "P2", Underlying: "HK.00700", Source: "hkex", SnapshotKey: "same", ObservedAt: at},
	}
	if _, err := OptionsDataFromQuoteSnapshots(rows); err == nil || !strings.Contains(err.Error(), "mixed sources") {
		t.Fatalf("mixed source error = %v", err)
	}
}

func TestHKEXHistoricalCycleUnlocksResearchOnly(t *testing.T) {
	theta := -0.1
	expiry := time.Date(2025, 7, 12, 0, 0, 0, 0, time.UTC)
	quote := func(observed time.Time) wheel.OptionQuote {
		return wheel.OptionQuote{
			Symbol: "HK.TST250712P500000", Source: "hkex", OptionType: wheel.Put,
			Expiry: expiry, Strike: 500, Delta: -0.4, Bid: 10, Ask: 10,
			ImpliedVol: 0.2, Theta: &theta, Volume: 1000, OpenInterest: 5000,
			LotSize: 100, QuoteTime: observed,
		}
	}
	first := time.Date(2025, 7, 2, 8, 0, 0, 0, time.UTC)
	last := time.Date(2025, 7, 11, 8, 0, 0, 0, time.UTC)
	opts := &OptionsData{QuoteBatches: []QuoteSnapshotBatch{
		{ObservedAt: first, SnapshotKey: "hkex-eod-20250702-bs-r0", Underlying: "HK.TEST", UnderlyingPrice: 500, Quotes: []wheel.OptionQuote{quote(first)}},
		{ObservedAt: last, SnapshotKey: "hkex-eod-20250711-bs-r0", Underlying: "HK.TEST", UnderlyingPrice: 500, Quotes: []wheel.OptionQuote{quote(last)}},
	}}
	signals := []SignalTrace{{CapabilityStatus: wheel.CapabilityReady}}
	quality := summarizeDataQuality([]ingest.Bar{{Ts: last, Open: 500, High: 500, Low: 500, Close: 500, Volume: 1}}, opts, signals)
	if quality.Status != "RESEARCH_ONLY" || quality.HistoricalOptionCycleComplete == nil || !*quality.HistoricalOptionCycleComplete || quality.ReadyBarCount != 1 || quality.SnapshotBatchCount != 2 {
		t.Fatalf("HKEX cycle quality = %+v", quality)
	}
	if len(quality.OptionSnapshotSources) != 1 || quality.OptionSnapshotSources[0] != "hkex" {
		t.Fatalf("sources = %v; want hkex", quality.OptionSnapshotSources)
	}
	for field, count := range quality.MissingRequiredFieldCounts {
		if count != 0 {
			t.Fatalf("missing %s = %d; want zero", field, count)
		}
	}
	for _, blocker := range quality.BlockedBy {
		if blocker == "historical_option_cycle" {
			t.Fatalf("complete cycle still blocked: %v", quality.BlockedBy)
		}
	}

	incomplete := &OptionsData{QuoteBatches: opts.QuoteBatches[:1]}
	quality = summarizeDataQuality(nil, incomplete, signals)
	if quality.Status != wheel.CapabilityDataBlocked || quality.HistoricalOptionCycleComplete == nil || *quality.HistoricalOptionCycleComplete {
		t.Fatalf("incomplete cycle quality = %+v", quality)
	}
}

func TestRunDataQualityRecordsUnderlyingAndOptionSources(t *testing.T) {
	bars := mkBars(100, 101, 102)
	bars[0].Source, bars[0].Adjusted = "futu", "fwd"
	for i := 1; i < len(bars); i++ {
		bars[i].Source, bars[i].Adjusted = "tencent", "qfq"
	}
	blocked := signalStubStrategy{signal: wheel.Signal{
		Action: wheel.ActionHold, Direction: wheel.DirectionHold,
		CapabilityStatus: wheel.CapabilityDataBlocked, BlockedBy: []string{"historical_option_snapshots"},
	}}
	opts := &OptionsData{QuoteBatches: []QuoteSnapshotBatch{{Quotes: []wheel.OptionQuote{
		{Source: "futu"}, {Source: "futu"},
	}}}}
	res, err := RunOptions(context.Background(), bars, 10000, 0, blocked, opts)
	if err != nil {
		t.Fatal(err)
	}
	got := res.DataQuality
	wantBars := []BarProvenance{{Source: "futu", Adjusted: "fwd", BarCount: 1}, {Source: "tencent", Adjusted: "qfq", BarCount: 2}}
	if len(got.UnderlyingBars) != len(wantBars) {
		t.Fatalf("underlying bars = %+v; want %+v", got.UnderlyingBars, wantBars)
	}
	for i := range wantBars {
		if got.UnderlyingBars[i] != wantBars[i] {
			t.Fatalf("underlying bars = %+v; want %+v", got.UnderlyingBars, wantBars)
		}
	}
	if len(got.OptionSnapshotSources) != 1 || got.OptionSnapshotSources[0] != "futu" {
		t.Fatalf("option snapshot sources = %v; want [futu]", got.OptionSnapshotSources)
	}
}

func TestRunMaxDrawdown(t *testing.T) {
	// V-shaped closes 100/50/100/90: equity 10000->5000->10000->9000, drawdown 0.5.
	bars := mkBars(100, 50, 100, 90)
	res, err := Run(context.Background(), bars, 10000, 0, &BuyHoldStrategy{})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if math.Abs(res.MaxDrawdown-0.5) > 1e-9 {
		t.Fatalf("Run().MaxDrawdown = %v; want ~0.5", res.MaxDrawdown)
	}
	if math.Abs(res.Equity-9000) > 1e-9 || math.Abs(res.TotalReturn+0.1) > 1e-9 {
		t.Fatalf("Run() = %+v; want Equity ~9000, TotalReturn ~-0.1", res)
	}
}

func TestRunValidation(t *testing.T) {
	badBars := []ingest.Bar{{Ts: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Open: 1, High: 1, Low: 2, Close: 1}}
	tests := []struct {
		name    string
		bars    []ingest.Bar
		cash    float64
		fee     float64
		s       Strategy
		wantErr string
	}{
		{"empty bars", nil, 10000, 0, HoldStrategy{}, "backtest: empty bars"},
		{"zero cash", mkBars(100), 0, 0, HoldStrategy{}, "initial cash must be > 0"},
		{"negative cash", mkBars(100), -1, 0, HoldStrategy{}, "initial cash must be > 0"},
		{"negative fee", mkBars(100), 10000, -1, HoldStrategy{}, "backtest: negative fee"},
		{"nil strategy", mkBars(100), 10000, 0, nil, "backtest: nil strategy"},
		{"invalid bars passthrough", badBars, 10000, 0, HoldStrategy{}, "ingest: validate bars"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(context.Background(), tt.bars, tt.cash, tt.fee, tt.s)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run() error = %v; want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunTradeValidation(t *testing.T) {
	tests := []struct {
		name    string
		s       Strategy
		wantErr string
	}{
		{"over-buy", stubStrategy{action: ActionBuy, size: 200}, "exceeds cash"},
		{"negative buy size", stubStrategy{action: ActionBuy, size: -1}, "exceeds cash"},
		{"over-sell", stubStrategy{action: ActionSell, size: 50}, "exceeds position"},
		{"unknown action", stubStrategy{action: Action(99)}, "unknown action"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(context.Background(), mkBars(100), 10000, 0, tt.s)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run() error = %v; want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunStrategyError(t *testing.T) {
	want := errors.New("boom")
	_, err := Run(context.Background(), mkBars(100), 10000, 0, stubStrategy{err: want})
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("Run() error = %v; want wrapping %v", err, want)
	}
}

func TestRunContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, mkBars(100, 110), 10000, 0, HoldStrategy{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v; want context.Canceled", err)
	}
}

func TestBuyHoldStrategy(t *testing.T) {
	st := &State{Cash: 10000}
	bar := ingest.Bar{Close: 100}
	s := &BuyHoldStrategy{}

	act, size, err := s.OnBar(context.Background(), bar, st)
	if err != nil {
		t.Fatalf("first OnBar() error: %v", err)
	}
	if act != ActionBuy || size != 100 {
		t.Fatalf("first OnBar() = (%v, %v); want (ActionBuy, 100)", act, size)
	}
	// All-in: cost equals the available cash.
	if math.Abs(size*bar.Close-st.Cash) > 1e-9 {
		t.Fatalf("buy size %v at close %v does not exhaust cash %v", size, bar.Close, st.Cash)
	}

	act2, size2, err := s.OnBar(context.Background(), bar, st)
	if err != nil {
		t.Fatalf("second OnBar() error: %v", err)
	}
	if act2 != ActionHold || size2 != 0 {
		t.Fatalf("second OnBar() = (%v, %v); want (ActionHold, 0)", act2, size2)
	}
}

func TestParseBars(t *testing.T) {
	data := []byte(`[
		{"ts":"2024-01-01T00:00:00Z","open":100,"high":102,"low":99,"close":101,"volume":10},
		{"ts":"2024-01-02T01:00:00+01:00","open":101,"high":105,"low":100,"close":104,"volume":20}
	]`)
	bars, err := ParseBars(data)
	if err != nil {
		t.Fatalf("ParseBars() error: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("ParseBars() len = %d; want 2", len(bars))
	}
	b0, b1 := bars[0], bars[1]
	if !b0.Ts.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) || b0.Close != 101 || b0.Volume != 10 {
		t.Fatalf("ParseBars()[0] = %+v", b0)
	}
	// Offset timestamps are normalized to UTC.
	if !b1.Ts.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)) || b1.Open != 101 {
		t.Fatalf("ParseBars()[1] = %+v", b1)
	}
}

func TestParseBarsErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{"bad json", `[{"ts":`, "json"},
		{"bad ts", `[{"ts":"2024-01-01"}]`, "ts"},
		{"empty ts", `[{"ts":""}]`, "ts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseBars([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseBars() error = %v; want containing %q", err, tt.wantErr)
			}
		})
	}
}
