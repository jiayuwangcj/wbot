package wheel

import (
	"strings"
	"testing"
	"time"
)

func heldShortPut(symbol string, avgPremium float64, asOf time.Time) OptionPosition {
	return OptionPosition{Symbol: symbol, SignedContracts: -1, Strike: 390, MarketDelta: -0.30, Delta: -0.30, LotSize: 100, OptionType: Put, AvgPremium: avgPremium}
}

func TestTakeProfitTriggersAtThresholdBoundary(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	cfg.ProfitTakePct = 0.5
	for name, tc := range map[string]struct {
		ask         float64
		wantTrigger bool
	}{
		"exact boundary": {ask: 2.00, wantTrigger: true},  // ratio == 0.5
		"just below":     {ask: 2.01, wantTrigger: false}, // ratio 0.4975
		"deep decay":     {ask: 1.50, wantTrigger: true},  // ratio 0.625
	} {
		t.Run(name, func(t *testing.T) {
			q := testQuote(string(Put), 390, asOf.AddDate(0, 0, 7))
			q.Symbol = "P390"
			q.Ask = tc.ask
			s, err := Evaluate(cfg, DecisionInput{
				CurrentPrice: 400, AsOf: asOf,
				Positions: []OptionPosition{heldShortPut("P390", 4.00, asOf)},
				Quotes:    []OptionQuote{q},
			})
			if err != nil {
				t.Fatal(err)
			}
			if s.ClosePosition != tc.wantTrigger {
				t.Fatalf("ClosePosition = %v; want %v (signal %+v)", s.ClosePosition, tc.wantTrigger, s)
			}
			if tc.wantTrigger {
				if s.Action != ActionAlert || s.Direction != DirectionPut || s.Quantity != 1 || s.SignedContracts != 1 {
					t.Fatalf("signal = %+v; want ALERT PUT qty 1", s)
				}
				if !strings.Contains(s.Reason, "profit_take_pct") || s.Quote == nil || s.Quote.Symbol != "P390" {
					t.Fatalf("reason=%q quote=%+v; want profit_take_pct reason on P390", s.Reason, s.Quote)
				}
			}
		})
	}
}

func TestTakeProfitPicksHighestCapturedLeg(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	cfg.ProfitTakePct = 0.4
	q1 := testQuote(string(Put), 390, asOf.AddDate(0, 0, 7))
	q1.Symbol, q1.Ask = "P390", 1.00 // received 4.00 → ratio 0.75
	q2 := testQuote(string(Put), 380, asOf.AddDate(0, 0, 7))
	q2.Symbol, q2.Ask = "P380", 1.00 // received 2.00 → ratio 0.50
	s, err := Evaluate(cfg, DecisionInput{
		CurrentPrice: 400, AsOf: asOf,
		Positions: []OptionPosition{
			heldShortPut("P390", 4.00, asOf),
			{Symbol: "P380", SignedContracts: -2, Strike: 380, MarketDelta: -0.30, LotSize: 100, OptionType: Put, AvgPremium: 2.00},
		},
		Quotes: []OptionQuote{q1, q2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.ClosePosition || s.Quote == nil || s.Quote.Symbol != "P390" || s.Quantity != 1 {
		t.Fatalf("signal = %+v; want close of P390 qty 1 (highest captured ratio)", s)
	}
}

func TestTakeProfitSkipsLongLegsWithoutBasisAndExpired(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	cfg.ProfitTakePct = 0.5
	long := heldShortPut("P-LONG", 4.00, asOf)
	long.SignedContracts = 1 // long leg is never bought back by profit_take
	noBasis := heldShortPut("P-NOBASIS", 0, asOf)
	expiredQuote := testQuote(string(Put), 390, asOf.AddDate(0, 0, 7))
	expiredQuote.Symbol = "P-EXP"
	expiredQuote.Ask = 1.00
	expiredQuote.Expiry = asOf // settling bar: close would race the expiry settlement
	expired := heldShortPut("P-EXP", 4.00, asOf)
	s, err := Evaluate(cfg, DecisionInput{
		CurrentPrice: 400, AsOf: asOf,
		Positions: []OptionPosition{long, noBasis, expired},
		Quotes:    []OptionQuote{expiredQuote},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ClosePosition {
		t.Fatalf("signal = %+v; want no close: long leg, no fill basis and settling leg are all skipped", s)
	}
}

func TestTakeProfitClosePositionsOutsideInventory(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	cfg.ProfitTakePct = 0.5
	// The held leg sits outside the DTE window (final min_dte days before
	// expiry): it appears only in ClosePositions, never in Positions, so the
	// effective inventory must stay unchanged while the exit still triggers.
	outside := heldShortPut("P-EXP", 4.00, asOf)
	outside.Symbol = "P-OUT"
	q := testQuote(string(Put), 390, asOf.AddDate(0, 0, 7))
	q.Symbol, q.Ask = "P-OUT", 1.50
	s, err := Evaluate(cfg, DecisionInput{
		CurrentPrice: 400, AsOf: asOf,
		ClosePositions: []OptionPosition{outside},
		Quotes:         []OptionQuote{q},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.ClosePosition || s.Quote == nil || s.Quote.Symbol != "P-OUT" || s.Quantity != 1 {
		t.Fatalf("signal = %+v; want close of P-OUT qty 1", s)
	}
	if s.EffectiveInventory != 0 || s.ActualInventory != 0 {
		t.Fatalf("inventory = %v/%v; want 0/0 (ClosePositions must not enter inventory)", s.EffectiveInventory, s.ActualInventory)
	}
	// Without ClosePositions the same leg is invisible: no close signal.
	s2, err := Evaluate(cfg, DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{q}})
	if err != nil {
		t.Fatal(err)
	}
	if s2.ClosePosition {
		t.Fatalf("signal = %+v; want no close without ClosePositions", s2)
	}
}

func TestTakeProfitRunsBeforeEntryGatesAndIVRank(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	cfg.ProfitTakePct = 0.5
	cfg.MinIVRank = 0.8 // unknown rank would block entries; exits must not care
	q := testQuote(string(Put), 390, asOf.AddDate(0, 0, 7))
	q.Symbol, q.Ask = "P390", 1.50
	s, err := Evaluate(cfg, DecisionInput{
		CurrentPrice: 400, AsOf: asOf,
		Positions: []OptionPosition{heldShortPut("P390", 4.00, asOf)},
		Quotes:    []OptionQuote{q},
		// IVRank 0 = unknown: entry path would HOLD fail-closed.
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.ClosePosition || s.Quote == nil || s.Quote.Symbol != "P390" {
		t.Fatalf("signal = %+v; want close despite unknown IV rank", s)
	}
}

func TestDeltaCapRejectsBeyondLimit(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		cfgMutate func(*Config)
		quote     func(time.Time) OptionQuote
		wantAlert bool
		stock     bool // stock-heavy inventory → call direction
	}{
		"put under cap": {cfgMutate: func(c *Config) { c.PutDeltaMax = 0.25 },
			quote: func(t time.Time) OptionQuote {
				q := testQuote(string(Put), 390, t.AddDate(0, 0, 7))
				q.Delta = -0.25
				return q
			}, wantAlert: true},
		"put over cap": {cfgMutate: func(c *Config) { c.PutDeltaMax = 0.25 },
			quote: func(t time.Time) OptionQuote {
				q := testQuote(string(Put), 390, t.AddDate(0, 0, 7))
				q.Delta = -0.45
				return q
			}, wantAlert: false},
		"call under cap": {cfgMutate: func(c *Config) { c.CallDeltaMax = 0.25 },
			quote: func(t time.Time) OptionQuote {
				q := testQuote(string(Call), 600, t.AddDate(0, 0, 7))
				q.Delta = 0.25
				return q
			}, wantAlert: true, stock: true},
		"call over cap": {cfgMutate: func(c *Config) { c.CallDeltaMax = 0.25 },
			quote: func(t time.Time) OptionQuote {
				q := testQuote(string(Call), 600, t.AddDate(0, 0, 7))
				q.Delta = 0.45
				return q
			}, wantAlert: false, stock: true},
		"cap disabled accepts deep": {cfgMutate: func(c *Config) {},
			quote: func(t time.Time) OptionQuote {
				q := testQuote(string(Put), 390, t.AddDate(0, 0, 7))
				q.Delta = -0.45
				return q
			}, wantAlert: true},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(StateNormal)
			tc.cfgMutate(&cfg)
			q := tc.quote(asOf)
			in := DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{q}, CashAvailable: 1_000_000, HasCashAvailable: true}
			if tc.stock {
				in.StockShares, in.StockAverageCost = 1500, 500 // above target → sell call
			}
			s, err := Evaluate(cfg, in)
			if err != nil {
				t.Fatal(err)
			}
			if s.Action == ActionAlert != tc.wantAlert {
				t.Fatalf("signal = %+v; want alert=%v", s, tc.wantAlert)
			}
			if !tc.wantAlert && !strings.Contains(strings.Join(s.RejectReasons, " "), "delta") {
				t.Fatalf("reject reasons = %v; want delta cap reason", s.RejectReasons)
			}
		})
	}
}

func TestIVRankGateHighLowUnknown(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	q := testQuote(string(Put), 390, asOf.AddDate(0, 0, 7))
	for name, tc := range map[string]struct {
		ivRank    float64
		minIVRank float64
		wantAlert bool
	}{
		"high rank passes":   {ivRank: 0.8, minIVRank: 0.5, wantAlert: true},
		"low rank holds":     {ivRank: 0.2, minIVRank: 0.5, wantAlert: false},
		"unknown rank holds": {ivRank: 0, minIVRank: 0.5, wantAlert: false},
		"gate disabled":      {ivRank: 0, minIVRank: 0, wantAlert: true},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(StateNormal)
			cfg.MinIVRank = tc.minIVRank
			s, err := Evaluate(cfg, DecisionInput{
				CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{q},
				CashAvailable: 1_000_000, HasCashAvailable: true, IVRank: tc.ivRank,
			})
			if err != nil {
				t.Fatal(err)
			}
			if s.Action == ActionAlert != tc.wantAlert {
				t.Fatalf("signal = %+v; want alert=%v", s, tc.wantAlert)
			}
			if !tc.wantAlert && tc.minIVRank > 0 && !strings.Contains(s.Reason, "IV rank") {
				t.Fatalf("reason = %q; want IV rank gate reason", s.Reason)
			}
		})
	}
}

func TestNewParamsValidateRanges(t *testing.T) {
	base := testConfig(StateNormal)
	for name, tc := range map[string]struct {
		mutate  func(*Config)
		wantErr bool
	}{
		"profit_take_pct high ok":  {mutate: func(c *Config) { c.ProfitTakePct = 0.8 }, wantErr: false},
		"profit_take_pct too high": {mutate: func(c *Config) { c.ProfitTakePct = 0.81 }, wantErr: true},
		"profit_take_pct negative": {mutate: func(c *Config) { c.ProfitTakePct = -0.1 }, wantErr: true},
		"put_delta_max high ok":    {mutate: func(c *Config) { c.PutDeltaMax = 1 }, wantErr: false},
		"put_delta_max too high":   {mutate: func(c *Config) { c.PutDeltaMax = 1.1 }, wantErr: true},
		"call_delta_max negative":  {mutate: func(c *Config) { c.CallDeltaMax = -0.1 }, wantErr: true},
		"min_iv_rank high ok":      {mutate: func(c *Config) { c.MinIVRank = 1 }, wantErr: false},
		"min_iv_rank too high":     {mutate: func(c *Config) { c.MinIVRank = 1.01 }, wantErr: true},
		"min_iv_rank negative":     {mutate: func(c *Config) { c.MinIVRank = -0.01 }, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if got := cfg.Validate() != nil; got != tc.wantErr {
				t.Fatalf("Validate error=%v want %v", got, tc.wantErr)
			}
		})
	}
}
