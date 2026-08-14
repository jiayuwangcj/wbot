package wheel

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func testConfig(state string) Config {
	return Config{Strategy: "wheel", FullPositionPrice: 400, ZeroPositionPrice: 550, MaxInventory: 1200, MinDTE: 5, MaxDTE: 10, MinOptionQuality: 0, TradeGap: 50, StrategicState: state}
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
	}{{"below", 300, 1200}, {"full anchor", 400, 1200}, {"middle", 475, 600}, {"above", 600, 0}}
	cfg := testConfig(StateNormal)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cfg.TargetInventory(tc.price)
			if err != nil || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestConfigTargetInventoryUsesCompletePriceCurve(t *testing.T) {
	cfg := testConfig(StateNormal)
	cfg.PricePositionCurve = []PricePoint{
		{Price: 30.25, TargetInventory: 1100},
		{Price: 31.25, TargetInventory: 1000},
		{Price: 33.25, TargetInventory: 800},
		{Price: 35.25, TargetInventory: 600},
	}
	cfg.MaxInventory = 1100
	for _, tc := range []struct {
		price float64
		want  float64
	}{{29, 1100}, {32.25, 900}, {35.25, 600}, {40, 600}} {
		got, err := cfg.TargetInventory(tc.price)
		if err != nil || got != tc.want {
			t.Fatalf("TargetInventory(%v) = %v, %v; want %v from complete curve", tc.price, got, err, tc.want)
		}
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
		{"full price non-positive", func(c *Config) { c.FullPositionPrice = 0 }, true},
		{"zero not above full", func(c *Config) { c.ZeroPositionPrice = c.FullPositionPrice }, true},
		{"fractional max inventory", func(c *Config) { c.MaxInventory = 1200.5 }, true},
		{"DTE outside wheel window", func(c *Config) { c.MinDTE = 4 }, true},
		{"quality outside bounds", func(c *Config) { c.MinOptionQuality = 1.1 }, true},
		{"negative move interval", func(c *Config) { c.MoveIntervalPct = -0.01 }, true},
		{"curve target above max inventory", func(c *Config) {
			c.PricePositionCurve = []PricePoint{{Price: 100, TargetInventory: 1201}, {Price: 110, TargetInventory: 0}}
		}, true},
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
		{"bad lot", func(q *OptionQuote) { q.LotSize = 0 }, true},
		// Lot size comes from the live quote now; any positive value is accepted
		// (config no longer carries one, 2026-08-13).
		{"mismatched lot accepted", func(q *OptionQuote) { q.LotSize = 10 }, false},
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

func TestQuoteValidationUsesDTESentinels(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(StateNormal)
	tooSoon := testQuote(string(Put), 400, asOf.AddDate(0, 0, 4))
	tooSoon.QuoteTime = asOf
	if err := tooSoon.Validate(asOf, cfg); !errors.Is(err, ErrDTEBelowMinimum) {
		t.Fatalf("too-soon DTE error = %v; want ErrDTEBelowMinimum", err)
	}
	tooLate := testQuote(string(Put), 400, asOf.AddDate(0, 0, 11))
	tooLate.QuoteTime = asOf
	if err := tooLate.Validate(asOf, cfg); !errors.Is(err, ErrDTEAboveMaximum) {
		t.Fatalf("too-late DTE error = %v; want ErrDTEAboveMaximum", err)
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

// TestEvaluatePendingOrderExclusion: 未成交挂单必须排除同合约同方向的
// 候选——否则每次评估都重新 alert 同一合约,LLM 审核反复拒绝重复敞口
// (2026-08-13: US.JD P29000 挂单 206158430256 未成交,747/749/750/751
// 连续被 REJECT)。
func TestEvaluatePendingOrderExclusion(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))  // Symbol P-4
	put2 := testQuote(string(Put), 500, asOf.AddDate(0, 0, 7)) // Symbol P-5
	cfg := testConfig(StateNormal)
	base := DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put, put2}, HasCashAvailable: true, CashAvailable: 1_000_000}

	// 首选(P-4)在挂单中:该候选被排除,信号改选 P-5。
	sig, err := Evaluate(cfg, base)
	if err != nil || sig.Action != ActionAlert {
		t.Fatalf("baseline signal = %+v err=%v; want ALERT", sig, err)
	}
	withPending := base
	withPending.Pending = []PendingOrder{{Contract: put.Symbol, Direction: string(Put)}}
	sig, err = Evaluate(cfg, withPending)
	if err != nil || sig.Action != ActionAlert {
		t.Fatalf("pending signal = %+v err=%v; want ALERT on next contract", sig, err)
	}
	if sig.Quote == nil || sig.Quote.Symbol != put2.Symbol {
		t.Fatalf("chosen quote = %+v; want %s (pending %s must be skipped)", sig.Quote, put2.Symbol, put.Symbol)
	}
	for _, c := range sig.Candidates {
		if !c.Accepted && strings.Contains(strings.Join(c.Reasons, " "), "pending order") {
			continue
		}
		if c.Accepted && c.Quote.Symbol == put.Symbol {
			t.Fatalf("candidate %s accepted despite pending order", put.Symbol)
		}
	}

	// 全部候选被挂单覆盖:HOLD,且不得误报 DATA_BLOCKED(行情是好的)。
	allPending := base
	allPending.Pending = []PendingOrder{{Contract: put.Symbol, Direction: string(Put)}, {Contract: put2.Symbol, Direction: string(Put)}}
	sig, err = Evaluate(cfg, allPending)
	if err != nil || sig.Action != ActionHold {
		t.Fatalf("all-pending signal = %+v err=%v; want HOLD", sig, err)
	}
	if sig.CapabilityStatus != CapabilityReady {
		t.Fatalf("all-pending capability = %s; want READY (book full, not data blocked)", sig.CapabilityStatus)
	}
	if !strings.Contains(sig.Reason, "pending orders") {
		t.Fatalf("all-pending reason = %q; want explicit pending mention", sig.Reason)
	}

	// 方向不匹配的挂单不排除候选(反向挂单是另一笔敞口)。
	callPending := base
	callPending.Pending = []PendingOrder{{Contract: put.Symbol, Direction: string(Call)}}
	sig, err = Evaluate(cfg, callPending)
	if err != nil || sig.Action != ActionAlert || sig.Quote == nil || sig.Quote.Symbol != put.Symbol {
		t.Fatalf("wrong-direction pending signal = %+v err=%v; want ALERT on %s", sig, err, put.Symbol)
	}
}

// TestEvaluateReplaceOrder: 改单语义——同方向存在恰一张未成交挂单且首选
// 候选是不同合约时,信号携带 replace(撤旧挂新)。同样需要 LLM 审核(审核
// 规则 7 把关),改单不绕过任何闸门(老板指令 2026-08-13)。
func TestEvaluateReplaceOrder(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))  // P-4,首选
	put2 := testQuote(string(Put), 500, asOf.AddDate(0, 0, 7)) // P-5
	cfg := testConfig(StateNormal)
	base := DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put, put2}, HasCashAvailable: true, CashAvailable: 1_000_000}

	// 首选 P-4 与挂单合约 P-5 不同 → 改单信号。
	withPending := base
	withPending.Pending = []PendingOrder{{Contract: put2.Symbol, Direction: string(Put), OrderID: "206158430256"}}
	sig, err := Evaluate(cfg, withPending)
	if err != nil || sig.Action != ActionAlert {
		t.Fatalf("replace signal = %+v err=%v; want ALERT", sig, err)
	}
	if sig.Replace == nil {
		t.Fatalf("replace signal missing Replace; want 撤 %s 换 %s", put2.Symbol, put.Symbol)
	}
	if sig.Replace.OrderID != "206158430256" || sig.Replace.Contract != put2.Symbol {
		t.Fatalf("Replace = %+v; want order 206158430256 on %s", sig.Replace, put2.Symbol)
	}
	if sig.Quote == nil || sig.Quote.Symbol != put.Symbol {
		t.Fatalf("chosen = %+v; want %s", sig.Quote, put.Symbol)
	}
	if !strings.Contains(sig.Reason, "replace pending") {
		t.Fatalf("reason = %q; want replace mention", sig.Reason)
	}

	// 挂单合约 = 首选 → 排除,无 replace(不重复不下改单)。
	sameContract := base
	sameContract.Pending = []PendingOrder{{Contract: put.Symbol, Direction: string(Put), OrderID: "1"}}
	sig, err = Evaluate(cfg, sameContract)
	if err != nil || sig.Action != ActionAlert {
		t.Fatalf("same-contract signal = %+v err=%v; want ALERT on next contract", sig, err)
	}
	if sig.Replace != nil {
		t.Fatalf("same-contract signal must not carry Replace: %+v", sig.Replace)
	}
	if sig.Quote == nil || sig.Quote.Symbol != put2.Symbol {
		t.Fatalf("same-contract chosen = %+v; want %s", sig.Quote, put2.Symbol)
	}

	// 多张同方向挂单 → 不触发改单(不确定替换哪张,留给人工)。
	multi := base
	multi.Pending = []PendingOrder{{Contract: put2.Symbol, Direction: string(Put), OrderID: "1"}, {Contract: "P-6", Direction: string(Put), OrderID: "2"}}
	sig, err = Evaluate(cfg, multi)
	if err != nil || sig.Action != ActionAlert {
		t.Fatalf("multi-pending signal = %+v err=%v; want ALERT", sig, err)
	}
	if sig.Replace != nil {
		t.Fatalf("multi-pending must not carry Replace: %+v", sig.Replace)
	}

	// rest 单合约已不在报价(过期/失效)→ 视为陈旧挂单,允许改单换新候选。
	stale := base
	stale.Pending = []PendingOrder{{Contract: "P-9-expired", Direction: string(Put), OrderID: "999"}}
	sig, err = Evaluate(cfg, stale)
	if err != nil || sig.Action != ActionAlert {
		t.Fatalf("stale-pending signal = %+v err=%v; want ALERT", sig, err)
	}
	if sig.Replace == nil || sig.Replace.OrderID != "999" || sig.Replace.Contract != "P-9-expired" {
		t.Fatalf("stale-pending Replace = %+v; want 撤 P-9-expired(999) 换 %s", sig.Replace, put.Symbol)
	}

	// 挂单候选失效(DTE 3 < 5,仍被报价但过不了结构校验)→ 允许改单。
	soon := testQuote(string(Put), 400, asOf.AddDate(0, 0, 3))
	bad := DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{soon, put2}, HasCashAvailable: true, CashAvailable: 1_000_000}
	bad.Pending = []PendingOrder{{Contract: soon.Symbol, Direction: string(Put), OrderID: "100"}}
	sig, err = Evaluate(cfg, bad)
	if err != nil || sig.Action != ActionAlert {
		t.Fatalf("invalid-pending signal = %+v err=%v; want ALERT", sig, err)
	}
	if sig.Replace == nil || sig.Replace.Contract != soon.Symbol {
		t.Fatalf("invalid-pending Replace = %+v; want 撤失效的 %s", sig.Replace, soon.Symbol)
	}
	if sig.Quote == nil || sig.Quote.Symbol != put2.Symbol {
		t.Fatalf("invalid-pending chosen = %+v; want %s", sig.Quote, put2.Symbol)
	}
}

func TestEvaluateTacticalGates(t *testing.T) {
	asOf := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	put := testQuote(string(Put), 400, asOf.AddDate(0, 0, 7))
	baseInput := DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put}, HasCashAvailable: true, CashAvailable: 1_000_000}

	cfg := testConfig(StateNormal)
	cfg.MoveIntervalPct = 0.02
	in := baseInput
	in.LastEffectiveFillPrice = 405
	if signal, err := Evaluate(cfg, in); err != nil || signal.Action != ActionHold || !strings.Contains(signal.Reason, "move_interval_pct") {
		t.Fatalf("move gate signal = %+v err=%v", signal, err)
	}

	cfg = testConfig(StateNormal)
	cfg.MinPremiumPerShare = put.Bid + 0.01
	if signal, err := Evaluate(cfg, baseInput); err != nil || signal.Action != ActionHold || !slices.Contains(signal.RejectReasons, "wheel: premium per share 4.0000 below minimum 4.0100") {
		t.Fatalf("premium gate signal = %+v err=%v", signal, err)
	}

	cfg = testConfig(StateNormal)
	cfg.StockSwitchPct = 0.05
	in = baseInput
	in.CurrentPrice = 400
	in.LastEffectiveFillPrice = 450
	if signal, err := Evaluate(cfg, in); err != nil || signal.Action != ActionHold || signal.StockSuggestion == nil || signal.StockSuggestion.Side != "BUY" || signal.StockSuggestion.Shares <= 0 {
		t.Fatalf("stock switch signal = %+v err=%v", signal, err)
	}
}

func TestEvaluateRiskAndNoDailyLimitTable(t *testing.T) {
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
		{"repeated decisions remain unlimited", DecisionInput{CurrentPrice: 400, AsOf: asOf, Quotes: []OptionQuote{put}, HasCashAvailable: true, CashAvailable: 100000}, "ALERT"},
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
