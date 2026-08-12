package wheelrun

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/datacheck"
	"github.com/jiayu/wbot/internal/futu"
	"github.com/jiayu/wbot/internal/watchlist"
	"github.com/jiayu/wbot/internal/wheelstore"
)

func TestATMExpansionLevelsAlternatesAroundNearestStrike(t *testing.T) {
	expiry := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	contracts := []futu.OptionContract{
		{Symbol: "US.X120C", OptionType: "call", Strike: 120, Expiry: expiry},
		{Symbol: "US.X090P", OptionType: "put", Strike: 90, Expiry: expiry},
		{Symbol: "US.X100C", OptionType: "call", Strike: 100, Expiry: expiry},
		{Symbol: "US.X110P", OptionType: "put", Strike: 110, Expiry: expiry},
	}
	levels := atmExpansionLevels(contracts, 105, 10)
	if len(levels) != 4 {
		t.Fatalf("levels = %d; want 4", len(levels))
	}
	want := []float64{100, 90, 110, 120}
	for i, level := range levels {
		if len(level) != 1 || level[0].Strike != want[i] {
			t.Fatalf("level %d = %+v; want strike %.0f", i, level, want[i])
		}
	}
}

func TestATMExpansionLevelsHonorsRadius(t *testing.T) {
	expiry := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	contracts := make([]futu.OptionContract, 0, 31)
	for i := 0; i < 31; i++ {
		strike := float64(100 + i*10)
		contracts = append(contracts, futu.OptionContract{
			Symbol:     fmt.Sprintf("US.X%02dC", i),
			OptionType: "call", Strike: strike, Expiry: expiry,
		})
	}
	levels := atmExpansionLevels(contracts, 250, maxATMExpansionRadius)
	if len(levels) != 21 {
		t.Fatalf("levels = %d; want center plus ten on each side", len(levels))
	}
	if levels[0][0].Strike != 250 || levels[len(levels)-1][0].Strike != 350 {
		t.Fatalf("radius window = %.0f..%.0f; want 250..350", levels[0][0].Strike, levels[len(levels)-1][0].Strike)
	}
}

func TestRunOnceATMQuotesOnlyExpandUntilTwoQualityCandidates(t *testing.T) {
	const symbol = "US.JD"
	now := time.Now()
	expiry := now.AddDate(0, 0, 7)
	strikes := []float64{590, 600, 610, 620}
	contracts := make([]futu.OptionContract, 0, len(strikes)*2)
	options := make(map[string]futu.OptionQuoteEx)
	for _, strike := range strikes {
		call := callContract("US.JD260901C"+formatStrike(strike), symbol, strike, expiry)
		put := call
		put.Symbol = "US.JD260901P" + formatStrike(strike)
		put.OptionType = "put"
		contracts = append(contracts, call, put)
		if strike == 600 || strike == 610 {
			options[call.Symbol] = fullCallQuote(call.Symbol, strike, expiry, now)
		}
	}
	quoter := &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts:   options,
	}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter: quoter, Positions: fakePositions{{Symbol: symbol, Code: "JD", Qty: 500, Side: SideLong}},
		Chain: fakeChain{contracts: contracts}, Store: store,
		Watchlist: &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
	})

	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if got := len(quoter.optionCalls); got != 3 {
		t.Fatalf("OptionQuotes calls = %d; want ATM, lower, upper only", got)
	}
	if got := quoter.optionCalls[0]; len(got) != 2 || got[0] != "US.JD260901C600000" || got[1] != "US.JD260901P600000" {
		t.Fatalf("ATM symbols = %v; want 600 call/put", got)
	}
	if got := quoter.optionCalls[1]; len(got) != 2 || got[0] != "US.JD260901C590000" || got[1] != "US.JD260901P590000" {
		t.Fatalf("lower symbols = %v; want 590 call/put", got)
	}
	if got := quoter.optionCalls[2]; len(got) != 2 || got[0] != "US.JD260901C610000" || got[1] != "US.JD260901P610000" {
		t.Fatalf("upper symbols = %v; want 610 call/put", got)
	}
	if len(store.signals) != 1 || store.signals[0].Action != "ALERT" {
		t.Fatalf("signals = %+v; want one ALERT", store.signals)
	}
}

func formatStrike(strike float64) string {
	return fmt.Sprintf("%06d", int(strike*1000))
}

func TestMarketIsOpenUsesExchangeTimezoneCalendarAndLunchBreak(t *testing.T) {
	hkt, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		t.Fatal(err)
	}
	cal := datacheck.NewExchangeCalendar()
	tests := []struct {
		name   string
		symbol string
		when   time.Time
		want   bool
	}{
		{"hk morning", "HK.00700", time.Date(2026, 8, 12, 10, 0, 0, 0, hkt), true},
		{"hk lunch", "HK.00700", time.Date(2026, 8, 12, 12, 30, 0, 0, hkt), false},
		{"hk afternoon", "HK.00700", time.Date(2026, 8, 12, 14, 0, 0, 0, hkt), true},
		{"hk close", "HK.00700", time.Date(2026, 8, 12, 16, 0, 0, 0, hkt), false},
		{"hk holiday", "HK.00700", time.Date(2026, 10, 1, 10, 0, 0, 0, hkt), false},
		{"us summer before open", "US.JD", time.Date(2026, 8, 12, 21, 29, 0, 0, hkt), false},
		{"us summer open", "US.JD", time.Date(2026, 8, 12, 21, 30, 0, 0, hkt), true},
		{"us summer close", "US.JD", time.Date(2026, 8, 13, 4, 0, 0, 0, hkt), false},
		{"us standard open", "US.JD", time.Date(2026, 1, 12, 22, 30, 0, 0, hkt), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marketIsOpen(tt.symbol, tt.when, cal); got != tt.want {
				t.Fatalf("marketIsOpen(%s, %s) = %t; want %t", tt.symbol, tt.when, got, tt.want)
			}
		})
	}
}

func TestRunOnceClosedMarketSkipsAllGatewayStages(t *testing.T) {
	const symbol = "HK.00700"
	hkt, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		t.Fatal(err)
	}
	quoter := &fakeQuoter{}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	r := NewRunner(Dependencies{
		Quoter: quoter, Positions: fakePositions{}, Chain: fakeChain{}, Store: store,
		Watchlist: &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
		Now:       func() time.Time { return time.Date(2026, 8, 12, 20, 0, 0, 0, hkt) },
	})
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if len(quoter.quoteCalls) != 0 || len(quoter.optionCalls) != 0 || len(store.signals) != 0 {
		t.Fatalf("closed market still touched live path: quote=%v options=%v signals=%v", quoter.quoteCalls, quoter.optionCalls, store.signals)
	}
}

type blockingSnapshotRecorder struct {
	started   chan struct{}
	startOnce sync.Once
	unblock   chan struct{}

	mu      sync.Mutex
	records []wheelstore.QuoteSnapshotRecord
}

func (r *blockingSnapshotRecorder) AppendQuoteSnapshot(_ context.Context, record wheelstore.QuoteSnapshotRecord) (int64, error) {
	r.startOnce.Do(func() { close(r.started) })
	if r.unblock != nil {
		<-r.unblock
	}
	r.mu.Lock()
	r.records = append(r.records, record)
	id := int64(len(r.records))
	r.mu.Unlock()
	return id, nil
}

func TestQuoteSnapshotRecordingIsAsynchronousAndBounded(t *testing.T) {
	const symbol = "HK.00700"
	now := time.Now()
	contract := callContract("HK.TCH260901C335000", symbol, 335, now.AddDate(0, 0, 7))
	quoter := &fakeQuoter{
		prices: map[string]float64{symbol: 600},
		opts:   map[string]futu.OptionQuoteEx{contract.Symbol: fullCallQuote(contract.Symbol, 335, contract.Expiry, now)},
	}
	recorder := &blockingSnapshotRecorder{started: make(chan struct{}), unblock: make(chan struct{})}
	store := &fakeStore{configs: map[string]*wheelstore.ConfigRecord{symbol: configRecord(symbol)}}
	r := testRunner(t, Dependencies{
		Quoter: quoter, Positions: fakePositions{{Symbol: symbol, Code: "00700", Qty: 500, Side: SideLong}},
		Chain: fakeChain{contracts: []futu.OptionContract{contract}}, Store: store,
		SnapshotRecorder: recorder, SnapshotQueueSize: 1,
		Watchlist: &fakeWatchlist{items: []watchlist.Item{wheelItem(symbol)}},
	})

	started := time.Now()
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("RunOnce waited for snapshot recorder: %s", elapsed)
	}
	close(recorder.unblock)
	r.Close()
	recorder.mu.Lock()
	got := len(recorder.records)
	recorder.mu.Unlock()
	if got != 1 {
		t.Fatalf("recorded snapshots = %d; want one selected contract", got)
	}
}
