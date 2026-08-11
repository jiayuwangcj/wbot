package wheel

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func testConfig(state string) Config {
	return Config{Strategy: "wheel", PricePositionCurve: []PricePoint{{Price: 400, TargetInventory: 1200}, {Price: 480, TargetInventory: 600}, {Price: 550, TargetInventory: 0}}, MaxInventory: 1200, LotSize: 100, MinDTE: 5, MaxDTE: 10, MinOptionQuality: 0, MaxDailyOrders: 1, ExtremeMaxDailyOrders: 2, NoTradeGap: 50, StrategicState: state}
}

func testQuote(kind string, strike float64, expiry time.Time) OptionQuote {
	delta := -0.30
	theta := -0.10
	if kind == string(Call) {
		delta = 0.30
	}
	return OptionQuote{Symbol: kind + "-" + string(rune('0'+int(strike/100))), Source: "test", OptionType: kind, Expiry: expiry, Strike: strike, Delta: delta, Bid: 4, Ask: 4.10, ImpliedVol: .3, Theta: &theta, Volume: 1000, OpenInterest: 10000, LotSize: 100, QuoteTime: expiry.Add(-8 * 24 * time.Hour)}
}

func TestInterpolateTargetInventoryTable(t *testing.T) {
	cases := []struct {
		name        string
		price, want float64
	}{{"below", 300, 1200}, {"anchor", 480, 600}, {"middle", 440, 900}, {"above", 600, 0}}
	curve := testConfig(StateNormal).PricePositionCurve
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InterpolateTargetInventory(curve, tc.price)
			if err != nil || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestConfigValidateTable(t *testing.T) {
	base := testConfig(StateNormal)
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(c *Config) {}, false},
		{"wrong strategy", func(c *Config) { c.Strategy = "covered-call" }, true},
		{"price not increasing", func(c *Config) { c.PricePositionCurve[1].Price = 400 }, true},
		{"target increasing", func(c *Config) { c.PricePositionCurve[1].TargetInventory = 1300 }, true},
		{"target over max", func(c *Config) { c.PricePositionCurve[0].TargetInventory = 1301 }, true},
		{"DTE outside wheel window", func(c *Config) { c.MinDTE = 4 }, true},
		{"quality outside bounds", func(c *Config) { c.MinOptionQuality = 1.1 }, true},
		{"daily hard cap", func(c *Config) { c.ExtremeMaxDailyOrders = 3 }, true},
		{"unknown state", func(c *Config) { c.StrategicState = "RISK_ON" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if got := cfg.Validate() != nil; got != tc.wantErr {
				t.Fatalf("Validate error=%v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestInventorySignedDeltaTable(t *testing.T) {
	positions := []OptionPosition{{SignedContracts: -2, MarketDelta: -.25}, {SignedContracts: -1, MarketDelta: .5}}
	if got, want := ActualInventory(500, 100), 600.0; got != want {
		t.Fatalf("actual=%v want %v", got, want)
	}
	if got, want := OptionDeltaStock(positions, 100), 0.0; got != want {
		t.Fatalf("option delta=%v want %v", got, want)
	}
	if got := EffectiveInventory(600, 0); got != 600 {
		t.Fatalf("effective=%v", got)
	}
}

func TestQuoteValidationTable(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	base := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))
	cases := []struct {
		name    string
		mutate  func(*OptionQuote)
		wantErr bool
	}{
		{"complete", func(q *OptionQuote) {}, false},
		{"missing source", func(q *OptionQuote) { q.Source = "" }, true},
		{"missing delta", func(q *OptionQuote) { q.Delta = 0 }, true},
		{"inverted market", func(q *OptionQuote) { q.Ask = 3 }, true},
		{"missing IV", func(q *OptionQuote) { q.ImpliedVol = 0 }, true},
		{"missing Theta", func(q *OptionQuote) { q.Theta = nil }, true},
		{"zero liquidity", func(q *OptionQuote) { q.Volume = 0 }, true},
		{"wrong DTE", func(q *OptionQuote) { q.Expiry = asOf.AddDate(0, 0, 11) }, true},
		{"stale", func(q *OptionQuote) { q.QuoteTime = asOf.Add(-25 * time.Hour) }, true},
		{"missing timestamp", func(q *OptionQuote) { q.QuoteTime = time.Time{} }, true},
		{"bad lot", func(q *OptionQuote) { q.LotSize = 10 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := base
			tc.mutate(&q)
			if got := q.Validate(asOf, cfg) != nil; got != tc.wantErr {
				t.Fatalf("error=%v want %v", got, tc.wantErr)
			}
		})
	}
}

func TestEvaluateStateAndDirectionTable(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))
	call := testQuote(string(Call), 500, asOf.AddDate(0, 0, 7))
	cases := []struct {
		name, state, wantAction, wantDirection string
		price, stock                           float64
		quotes                                 []OptionQuote
	}{
		{"normal put", StateNormal, ActionAlert, DirectionPut, 400, 0, []OptionQuote{put}},
		{"normal call", StateNormal, ActionAlert, DirectionCall, 550, 1000, []OptionQuote{call}},
		{"pause put", StatePauseBuy, ActionHold, DirectionHold, 400, 0, []OptionQuote{put}},
		{"exit call", StateExit, ActionAlert, DirectionCall, 550, 1000, []OptionQuote{call}},
		{"exit put", StateExit, ActionHold, DirectionHold, 400, 0, []OptionQuote{put}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(tc.state)
			s, err := Evaluate(cfg, DecisionInput{CurrentPrice: tc.price, AsOf: asOf, StockShares: tc.stock, Quotes: tc.quotes, CashAvailable: 100000, HasCashAvailable: true})
			if err != nil || s.Action != tc.wantAction || s.Direction != tc.wantDirection {
				t.Fatalf("signal=%+v err=%v", s, err)
			}
			if s.Action == ActionAlert && s.SignedContracts != -1 {
				t.Fatalf("signed contracts=%d", s.SignedContracts)
			}
		})
	}
}

func TestExpectedGainEstimateAndMissingData(t *testing.T) {
	quote := OptionQuote{Bid: 4, LotSize: 100}
	if got, want := expectedGain(quote, 2), 800.0; got != want {
		t.Fatalf("expectedGain() = %v; want %v", got, want)
	}
	for _, tc := range []struct {
		name     string
		quote    OptionQuote
		quantity int
	}{
		{name: "missing bid", quote: OptionQuote{LotSize: 100}, quantity: 1},
		{name: "missing lot size", quote: OptionQuote{Bid: 4}, quantity: 1},
		{name: "missing quantity", quote: quote},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expectedGain(tc.quote, tc.quantity); got != 0 {
				t.Fatalf("expectedGain() = %v; want 0", got)
			}
		})
	}

	b, err := json.Marshal(Signal{Action: ActionHold})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "expected_gain") {
		t.Fatalf("zero expected gain must be omitted: %s", b)
	}
}

func TestEvaluateAlertIncludesExpectedGain(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))
	signal, err := Evaluate(testConfig(StateNormal), DecisionInput{
		CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put},
		HasCashAvailable: true, CashAvailable: 1_000_000,
	})
	if err != nil || signal.Action != ActionAlert {
		t.Fatalf("signal = %+v err=%v; want ALERT", signal, err)
	}
	if got, want := signal.ExpectedGain, 400.0; got != want {
		t.Fatalf("ExpectedGain = %v; want %v", got, want)
	}
}

func TestEvaluateRiskAndDailyLimitTable(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))
	cases := []struct {
		name  string
		input DecisionInput
		want  string
	}{
		{"no cash context", DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put}}, "HOLD"},
		{"insufficient cash", DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put}, HasCashAvailable: true, CashAvailable: 1}, "HOLD"},
		{"daily cap", DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put}, DailyOrders: 1, HasCashAvailable: true, CashAvailable: 100000}, "HOLD"},
		{"assignment max", DecisionInput{CurrentPrice: 400, AsOf: asOf, StockShares: 1200, Quotes: []OptionQuote{put}, HasCashAvailable: true, CashAvailable: 100000}, "HOLD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := Evaluate(cfg, tc.input)
			if string(s.Action) != tc.want {
				t.Fatalf("action=%s reason=%s", s.Action, s.Reason)
			}
		})
	}
}

func TestCapabilityStatusDistinguishesDataBlockFromRiskHold(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))

	alert, err := Evaluate(cfg, DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put}, HasCashAvailable: true, CashAvailable: 1_000_000})
	if err != nil || alert.Action != ActionAlert || alert.CapabilityStatus != CapabilityReady || len(alert.BlockedBy) != 0 {
		t.Fatalf("alert = %+v err=%v; want READY ALERT", alert, err)
	}

	missingTheta := put
	missingTheta.Theta = nil
	blocked, err := Evaluate(cfg, DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{missingTheta}, HasCashAvailable: true, CashAvailable: 1_000_000})
	if err != nil || blocked.Action != ActionHold || blocked.CapabilityStatus != CapabilityDataBlocked || len(blocked.BlockedBy) != 1 || blocked.BlockedBy[0] != "option_quote_snapshot" {
		t.Fatalf("blocked = %+v err=%v; want DATA_BLOCKED quote dependency", blocked, err)
	}

	riskHold, err := Evaluate(cfg, DecisionInput{
		CurrentPrice: 400, AsOf: asOf, StockShares: 1100,
		Positions:        []OptionPosition{{SignedContracts: -1, MarketDelta: -.25, LotSize: 100, OptionType: Put}},
		Quotes:           []OptionQuote{put},
		HasCashAvailable: true, CashAvailable: 1_000_000,
	})
	if err != nil || riskHold.Action != ActionHold || riskHold.CapabilityStatus != CapabilityReady || len(riskHold.BlockedBy) != 0 {
		t.Fatalf("risk hold = %+v err=%v; want READY risk-policy HOLD", riskHold, err)
	}
}

func TestCapabilityStatusBlocksWhenRequiredQuoteDirectionIsMissing(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	call := testQuote(string(Call), 500, asOf.AddDate(0, 0, 7))
	signal, err := Evaluate(cfg, DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{call}, HasCashAvailable: true, CashAvailable: 1_000_000})
	if err != nil || signal.Action != ActionHold || signal.CapabilityStatus != CapabilityDataBlocked || !slices.Contains(signal.BlockedBy, "option_quote_snapshot") {
		t.Fatalf("signal = %+v err=%v; want DATA_BLOCKED because Put direction has no quote", signal, err)
	}
}

func TestExistingShortPutCommitmentCountsTowardCashReserve(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	put := testQuote(string(Put), 100, asOf.AddDate(0, 0, 7))
	signal, err := Evaluate(cfg, DecisionInput{
		CurrentPrice: 400, AsOf: asOf,
		Positions: []OptionPosition{{SignedContracts: -1, Strike: 95, MarketDelta: -.25, LotSize: 100, OptionType: Put}},
		Quotes:    []OptionQuote{put}, HasCashAvailable: true, CashAvailable: 15_000,
	})
	if err != nil || signal.Action != ActionHold || signal.CapabilityStatus != CapabilityReady {
		t.Fatalf("signal = %+v err=%v; want READY risk HOLD for cumulative reserve 19,500 > cash", signal, err)
	}
}

func TestExistingShortPutCommitmentCountsTowardMaxInventory(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))
	signal, err := Evaluate(cfg, DecisionInput{
		CurrentPrice: 400,
		AsOf:         asOf,
		Positions: []OptionPosition{{
			SignedContracts: -12,
			MarketDelta:     -0.25,
			LotSize:         100,
			OptionType:      Put,
		}},
		Quotes:           []OptionQuote{put},
		HasCashAvailable: true,
		CashAvailable:    1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if signal.Action != ActionHold || signal.Reason != "no quote passed validation and risk checks" {
		t.Fatalf("signal = %+v; want HOLD because existing put commitments consume max inventory", signal)
	}
}

func TestEvaluateStableCandidateOrderingTable(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	far := testQuote(string(Put), 300, asOf.AddDate(0, 0, 7))
	far.Symbol = "far"
	far.Delta = -.05
	near := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))
	near.Symbol = "near"
	near.Delta = -.30
	s, err := Evaluate(cfg, DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{far, near}, HasCashAvailable: true, CashAvailable: 100000})
	if err != nil || s.Action != ActionAlert || s.Quote == nil || s.Quote.Symbol != "near" {
		t.Fatalf("got %+v err=%v", s, err)
	}

	// CAUTION applies the lower-strike preference when post-trade risk is tied.
	cfg.StrategicState = StateCaution
	low := testQuote(string(Put), 380, asOf.AddDate(0, 0, 7))
	low.Symbol = "low"
	low.Delta = -.30
	high := testQuote(string(Put), 420, asOf.AddDate(0, 0, 7))
	high.Symbol = "high"
	high.Delta = -.30
	s, err = Evaluate(cfg, DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{high, low}, HasCashAvailable: true, CashAvailable: 100000})
	if err != nil || s.Action != ActionAlert || s.Quote == nil || s.Quote.Symbol != "low" {
		t.Fatalf("caution got %+v err=%v", s, err)
	}
}

func TestCoveredCallAndQualityTable(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	call := testQuote(string(Call), 500, asOf.AddDate(0, 0, 7))
	cases := []struct {
		name      string
		positions []OptionPosition
		stock     float64
		want      string
	}{
		{"covered", nil, 100, ActionAlert},
		{"already committed call", []OptionPosition{{SignedContracts: -1, MarketDelta: .3, OptionType: Call}}, 100, ActionHold},
		{"naked assignment", nil, 0, ActionHold},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := Evaluate(cfg, DecisionInput{CurrentPrice: 550, AsOf: asOf, StockShares: tc.stock, Positions: tc.positions, Quotes: []OptionQuote{call}, CashAvailable: 100000, HasCashAvailable: true})
			if string(s.Action) != tc.want {
				t.Fatalf("action=%s reason=%s", s.Action, s.Reason)
			}
		})
	}
	better := call
	better.Bid, better.Ask, better.Volume, better.OpenInterest = 4, 4.01, 10000, 100000
	if QualityScore(better) <= QualityScore(call) || QualityScore(better) > 1 {
		t.Fatalf("quality not monotonic: %v <= %v", QualityScore(better), QualityScore(call))
	}
}
