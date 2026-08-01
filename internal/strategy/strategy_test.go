package strategy

// Template registry + covered-call / cash-secured-put behavior tests; all
// numbers hand-computable (lot 100, daily bars Jan 1+i 2024, UTC midnight).

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
)

func day(i int) time.Time { return time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC) }

func bars(closes ...float64) []ingest.Bar {
	out := make([]ingest.Bar, 0, len(closes))
	for i, c := range closes {
		out = append(out, ingest.Bar{Ts: day(i), Open: c, High: c, Low: c, Close: c, Volume: 100})
	}
	return out
}

// optBars builds per-code price bars across the whole run (day-aligned).
func optBars(closes map[string][]float64) backtest.OptionBars {
	out := backtest.OptionBars{}
	for code, cs := range closes {
		for i, v := range cs {
			out[code] = append(out[code], ingest.Bar{Ts: day(i), Open: v, High: v, Low: v, Close: v, Volume: 10})
		}
	}
	return out
}

// run builds name's strategy with params and replays bars over chain+optBars.
func run(t *testing.T, name string, params map[string]any, closes []float64, chain backtest.OptionChain, ob backtest.OptionBars) *backtest.Result {
	t.Helper()
	s, err := Factory(name, params)
	if err != nil {
		t.Fatalf("Factory(%s) error: %v", name, err)
	}
	res, err := backtest.RunOptions(context.Background(), bars(closes...), 10000, 0, s, &backtest.OptionsData{Chain: chain, Bars: ob})
	if err != nil {
		t.Fatalf("RunOptions(%s) error: %v", name, err)
	}
	return res
}

func assertEq(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v; want %v", label, got, want)
	}
}

func TestTemplates(t *testing.T) {
	ts := Templates()
	if len(ts) != 2 || ts[0].Name != "covered-call" || ts[1].Name != "cash-secured-put" {
		t.Fatalf("Templates() = %+v; want covered-call + cash-secured-put", ts)
	}
	if !ts[0].NeedsOptions || !ts[1].NeedsOptions {
		t.Fatalf("Templates() NeedsOptions = %v %v; want true true", ts[0].NeedsOptions, ts[1].NeedsOptions)
	}
	if _, ok := Lookup("covered-call"); !ok {
		t.Fatal("Lookup(covered-call) = false")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup(nope) = true")
	}
	for _, tt := range ts {
		for _, p := range tt.Params {
			if p.Default == nil {
				t.Fatalf("%s param %s: nil default", tt.Name, p.Name)
			}
			switch p.Type {
			case "number":
				if p.Min >= p.Max {
					t.Fatalf("%s param %s: bad range [%v, %v]", tt.Name, p.Name, p.Min, p.Max)
				}
			case "string":
				if len(p.Allowed) == 0 {
					t.Fatalf("%s param %s: no allowed values", tt.Name, p.Name)
				}
			default:
				t.Fatalf("%s param %s: bad type %q", tt.Name, p.Name, p.Type)
			}
		}
	}
}

func TestFactoryValidation(t *testing.T) {
	tests := []struct {
		name    string
		templ   string
		params  map[string]any
		wantErr string
	}{
		{"unknown template", "nope", map[string]any{}, "unknown template"},
		{"unknown param", "covered-call", map[string]any{"bogus": 1}, "unknown param"},
		{"wrong type", "covered-call", map[string]any{"strike_pct_otm": "high"}, "want a number"},
		{"negative otm", "covered-call", map[string]any{"strike_pct_otm": -0.01}, "want in [0"},
		{"negative fee", "covered-call", map[string]any{"fee_per_contract": -1}, "want in [0"},
		{"zero days", "covered-call", map[string]any{"days_to_expiry": 0}, "want in [1"},
		{"bad expiry rule", "covered-call", map[string]any{"expiry_rule": "monthly"}, "want one of"},
		{"low cash reserve", "cash-secured-put", map[string]any{"cash_reserve": 0.9}, "want in [1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Factory(tt.templ, tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Factory(%s, %v) error = %v; want containing %q", tt.templ, tt.params, err, tt.wantErr)
			}
		})
	}
	// Defaults apply when params are omitted entirely.
	s, err := Factory("covered-call", map[string]any{})
	if err != nil {
		t.Fatalf("Factory(defaults) error: %v", err)
	}
	if cc, ok := s.(*CoveredCall); !ok || cc.lot != 100 || cc.strikePctOTM != 0.03 || cc.expiryRule != "next_expiry" {
		t.Fatalf("defaults = %+v; want lot 100, pct 0.03, next_expiry", s)
	}
}

func TestCoveredCallITM(t *testing.T) {
	chain := backtest.OptionChain{
		"C105": {Code: "C105", Kind: backtest.OptionCall, Strike: 105, Expiry: day(3)},
		"C110": {Code: "C110", Kind: backtest.OptionCall, Strike: 110, Expiry: day(3)},
		"C115": {Code: "C115", Kind: backtest.OptionCall, Strike: 115, Expiry: day(3)},
	}
	ob := optBars(map[string][]float64{
		"C105": {2, 1.5, 1, 0.5}, "C110": {1.5, 1.2, 0.8, 0.4}, "C115": {3, 2.5, 1.5, 0.8},
	})
	res := run(t, "covered-call", map[string]any{"strike_pct_otm": 0.05}, []float64{100, 110, 118, 120}, chain, ob)
	// buy 100 @100, sell C115 for 250, exercised at 115 on close 120: cash 11750.
	assertEq(t, "equity", res.Equity, 11750)
	assertEq(t, "return", res.TotalReturn, 0.175)
	if res.Bars != 4 {
		t.Fatalf("Bars = %d; want 4", res.Bars)
	}
}

func TestCoveredCallOTMRoll(t *testing.T) {
	chain := backtest.OptionChain{
		"C105": {Code: "C105", Kind: backtest.OptionCall, Strike: 105, Expiry: day(3)},
		"C110": {Code: "C110", Kind: backtest.OptionCall, Strike: 110, Expiry: day(3)},
		"C115": {Code: "C115", Kind: backtest.OptionCall, Strike: 115, Expiry: day(3)},
	}
	ob := optBars(map[string][]float64{
		"C105": {2, 1.5, 1, 0.5}, "C110": {1.5, 1.2, 0.8, 0.4}, "C115": {3, 2.5, 1.5, 0.8},
	})
	res := run(t, "covered-call", map[string]any{"strike_pct_otm": 0.05}, []float64{100, 110, 105, 108}, chain, ob)
	// C115 OTM on day 2 (105 < 115): voided, premium kept; day 3 rolls into a
	// new C115 at 0.8: cash 250+80, stock 100 @108, leg -100*0.8.
	assertEq(t, "equity", res.Equity, 11050)
	assertEq(t, "return", res.TotalReturn, 0.105)
}

func TestCoveredCallFee(t *testing.T) {
	chain := backtest.OptionChain{
		"C105": {Code: "C105", Kind: backtest.OptionCall, Strike: 105, Expiry: day(3)},
		"C110": {Code: "C110", Kind: backtest.OptionCall, Strike: 110, Expiry: day(3)},
		"C115": {Code: "C115", Kind: backtest.OptionCall, Strike: 115, Expiry: day(3)},
	}
	ob := optBars(map[string][]float64{
		"C105": {2, 1.5, 1, 0.5}, "C110": {1.5, 1.2, 0.8, 0.4}, "C115": {3, 2.5, 1.5, 0.8},
	})
	res := run(t, "covered-call", map[string]any{"strike_pct_otm": 0.05, "fee_per_contract": 10}, []float64{100, 110, 118, 120}, chain, ob)
	// Same as ITM but the 250 premium is 240 after the 10 fee.
	assertEq(t, "equity", res.Equity, 11740)
}

func TestCoveredCallNoEligibleExpiry(t *testing.T) {
	// Expiry only on day 0 (never after a bar): the lot is still bought at 100
	// on day 0, then no call is ever sold (hold).
	chain := backtest.OptionChain{"C105": {Code: "C105", Kind: backtest.OptionCall, Strike: 105, Expiry: day(0)}}
	ob := optBars(map[string][]float64{"C105": {2}})
	res := run(t, "covered-call", map[string]any{}, []float64{100, 101, 102}, chain, ob)
	assertEq(t, "equity", res.Equity, 10200)
	assertEq(t, "return", res.TotalReturn, 0.02)
}

func TestCashSecuredPutITM(t *testing.T) {
	chain := backtest.OptionChain{
		"P95": {Code: "P95", Kind: backtest.OptionPut, Strike: 95, Expiry: day(2)},
		"P97": {Code: "P97", Kind: backtest.OptionPut, Strike: 97, Expiry: day(2)},
	}
	ob := optBars(map[string][]float64{"P95": {3, 2, 1}, "P97": {3.5, 2.5, 1.5}})
	res := run(t, "cash-secured-put", map[string]any{"strike_pct_otm": 0.05}, []float64{100, 90, 85}, chain, ob)
	// sell P95 for 300, assigned at 95 on close 85: cash 10000+300-9500, stock 100.
	assertEq(t, "equity", res.Equity, 9300)
	assertEq(t, "return", res.TotalReturn, -0.07)
}

func TestCashSecuredPutOTMRoll(t *testing.T) {
	chain := backtest.OptionChain{
		"P95": {Code: "P95", Kind: backtest.OptionPut, Strike: 95, Expiry: day(2)},
		"P97": {Code: "P97", Kind: backtest.OptionPut, Strike: 97, Expiry: day(2)},
	}
	ob := optBars(map[string][]float64{"P95": {3, 2, 1}, "P97": {3.5, 2.5, 1.5}})
	res := run(t, "cash-secured-put", map[string]any{"strike_pct_otm": 0.05}, []float64{100, 103, 102}, chain, ob)
	// P95 OTM on day 1 (103 > 95): voided, premium kept; day 2 rolls into P97
	// at 1.5: cash 10300+150, leg -100*1.5.
	assertEq(t, "equity", res.Equity, 10300)
	assertEq(t, "return", res.TotalReturn, 0.03)
}

func TestCashSecuredPutFee(t *testing.T) {
	chain := backtest.OptionChain{"P95": {Code: "P95", Kind: backtest.OptionPut, Strike: 95, Expiry: day(2)}}
	ob := optBars(map[string][]float64{"P95": {3, 2, 1}})
	res := run(t, "cash-secured-put", map[string]any{"fee_per_contract": 10}, []float64{100, 90, 85}, chain, ob)
	// ITM path with a 10 fee: premium 290, assigned at 95 on close 85.
	assertEq(t, "equity", res.Equity, 9290)
}

func TestCashSecuredPutAssignedUnwind(t *testing.T) {
	chain := backtest.OptionChain{"P95": {Code: "P95", Kind: backtest.OptionPut, Strike: 95, Expiry: day(2)}}
	ob := optBars(map[string][]float64{"P95": {3, 2, 1, 1}})
	res := run(t, "cash-secured-put", map[string]any{}, []float64{100, 90, 85, 88}, chain, ob)
	// assigned 100 @95 on day 2 (cash 800), unwound at 88 on day 3.
	assertEq(t, "equity", res.Equity, 9600)
	assertEq(t, "return", res.TotalReturn, -0.04)
}

// captureStrategy records the last State passed to the wrapped strategy.
type captureStrategy struct {
	backtest.Strategy
	st *backtest.State
}

func (c *captureStrategy) OnBar(ctx context.Context, bar ingest.Bar, st *backtest.State) (backtest.Action, float64, error) {
	c.st = st
	return c.Strategy.OnBar(ctx, bar, st)
}

func TestCashSecuredPutReserveMultiplier(t *testing.T) {
	chain := backtest.OptionChain{"P95": {Code: "P95", Kind: backtest.OptionPut, Strike: 95, Expiry: day(2)}}
	ob := optBars(map[string][]float64{"P95": {3, 2}})
	// cash 30000 secures 3 contracts at collateral 9500, 2 at 11400 (x1.2).
	capture := func(cashReserve float64) *backtest.State {
		s, err := Factory("cash-secured-put", map[string]any{"cash_reserve": cashReserve})
		if err != nil {
			t.Fatalf("Factory() error: %v", err)
		}
		rec := &captureStrategy{Strategy: s}
		if _, err := backtest.RunOptions(context.Background(), bars(100, 101), 30000, 0, rec, &backtest.OptionsData{Chain: chain, Bars: ob}); err != nil {
			t.Fatalf("RunOptions() error: %v", err)
		}
		return rec.st
	}
	if got := capture(1.0).Options["P95"].Contracts; got != -3 {
		t.Fatalf("reserve 1.0 contracts = %v; want -3", got)
	}
	if got := capture(1.2).Options["P95"].Contracts; got != -2 {
		t.Fatalf("reserve 1.2 contracts = %v; want -2", got)
	}
}

func TestCashSecuredPutInsufficientCash(t *testing.T) {
	chain := backtest.OptionChain{"P95": {Code: "P95", Kind: backtest.OptionPut, Strike: 95, Expiry: day(2)}}
	ob := optBars(map[string][]float64{"P95": {3, 2}})
	s, err := Factory("cash-secured-put", map[string]any{"cash_reserve": 1.5})
	if err != nil {
		t.Fatalf("Factory() error: %v", err)
	}
	// 10000 / (1.5*95*100) < 1 contract: the strategy must reject the open.
	_, err = backtest.RunOptions(context.Background(), bars(100, 101), 10000, 0, s, &backtest.OptionsData{Chain: chain, Bars: ob})
	if err == nil || !strings.Contains(err.Error(), "cannot secure 1 contract") {
		t.Fatalf("RunOptions() error = %v; want cash error", err)
	}
}

func TestDaysRule(t *testing.T) {
	chain := backtest.OptionChain{
		"C105a": {Code: "C105a", Kind: backtest.OptionCall, Strike: 105, Expiry: day(2)},
		"C105b": {Code: "C105b", Kind: backtest.OptionCall, Strike: 105, Expiry: day(4)},
	}
	ob := optBars(map[string][]float64{"C105a": {2, 2, 2, 2}, "C105b": {2, 2, 2, 2}})
	// days rule: on day 1, day-2 expiry (|1-2|=1) ties day-4 (|3-2|=1) -> earlier
	// wins; day 2 OTM (104 < 105) voids; day 3 rolls into day-4 (|1-2|=1).
	res := run(t, "covered-call", map[string]any{"expiry_rule": "days", "days_to_expiry": 2}, []float64{100, 102, 104, 106}, chain, ob)
	// cash 200+200, stock 100 @106, leg -100*2.0.
	assertEq(t, "equity", res.Equity, 10800)
}
