// Package strategy exposes the one registered strategy template: wheel.
//
// The wheel template deliberately keeps strategic inputs explicit. In
// particular, there is no default price curve or maximum inventory: those
// values describe the user's risk policy and cannot safely be inferred.
package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
)

// Param is a structured configuration field in the wheel schema.
// Type is one of curve, number, or choice. Required fields have no default.
type Param struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Default  any      `json:"default,omitempty"`
	Required bool     `json:"required,omitempty"`
	Min      float64  `json:"min,omitempty"`
	Max      float64  `json:"max,omitempty"`
	Allowed  []string `json:"choices,omitempty"`
	Help     string   `json:"description"`
}

// Template describes one registered strategy template.
type Template struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	NeedsOptions bool    `json:"-"` // complete option snapshots are required
	Params       []Param `json:"params"`
}

var templates = []Template{
	{
		Name:         "llm",
		Description:  "每 15 分钟由 LLM 基于实时行情与账户上下文生成候选，经确定性风控和 LLM 审核后人工确认",
		NeedsOptions: true,
		Params: []Param{
			{Name: "option_max_quantity", Type: "number", Default: 5.0, Min: 1, Max: 5, Help: "单次期权决策最大合约数"},
			{Name: "stock_max_quantity", Type: "number", Default: 1000.0, Min: 1, Max: 1000, Help: "单次正股决策最大股数"},
			{Name: "max_daily_signals", Type: "number", Default: 5.0, Min: 1, Max: 20, Help: "每日最多可操作信号数"},
			{Name: "min_dte", Type: "number", Default: 5.0, Min: 1, Max: 30, Help: "期权最小到期天数"},
			{Name: "max_dte", Type: "number", Default: 10.0, Min: 1, Max: 60, Help: "期权最大到期天数"},
		},
	},
	{
		Name:         "wheel",
		Description:  "按满仓价—清仓价区间管理库存，只生成人工提醒，不自动下单",
		NeedsOptions: true,
		Params: []Param{
			{Name: "full_position_price", Type: "number", Required: true, Min: 0, Max: math.MaxFloat64, Help: "达到或低于此价格时的目标库存为满仓"},
			{Name: "zero_position_price", Type: "number", Required: true, Min: 0, Max: math.MaxFloat64, Help: "达到或高于此价格时的目标库存为零"},
			{Name: "max_inventory", Type: "number", Required: true, Min: 0, Max: math.MaxFloat64, Help: "允许的最大实际库存"},
			{Name: "move_interval_pct", Type: "number", Default: 0.0, Min: 0, Max: math.MaxFloat64, Help: "距上次有效成交价的最小绝对变动比例（小数）"},
			{Name: "min_premium_per_share", Type: "number", Default: 0.0, Min: 0, Max: math.MaxFloat64, Help: "最低每股权利金"},
			{Name: "min_option_profit", Type: "number", Default: wheel.DefaultMinOptionProfit, Min: 0, Max: math.MaxFloat64, Help: "单笔候选期权预期收益总额最低门槛（权利金×张数）"},
			{Name: "stock_switch_pct", Type: "number", Default: 0.0, Min: 0, Max: math.MaxFloat64, Help: "切换为正股建议的价格变动比例（小数）"},
			{Name: "trade_gap", Type: "number", Default: 50.0, Min: 0, Max: math.MaxFloat64, Help: "库存缺口不超过此值时不交易"},
			{Name: "min_dte", Type: "number", Default: 5.0, Min: wheel.MinWheelDTE, Max: wheel.MaxWheelDTE, Help: "最小到期天数（DTE）"},
			{Name: "max_dte", Type: "number", Default: 10.0, Min: wheel.MinWheelDTE, Max: wheel.MaxWheelDTE, Help: "最大到期天数（DTE）"},
			{Name: "min_option_quality", Type: "number", Default: 0.6, Min: 0, Max: 1, Help: "候选期权质量最低门槛"},
			{Name: "max_quote_age_seconds", Type: "number", Default: 86400.0, Min: 1, Max: math.MaxFloat64, Help: "候选期权报价最大可接受年龄（秒），超出视为陈旧"},
			{Name: "strategic_state", Type: "choice", Default: wheel.StateNormal, Allowed: []string{wheel.StateNormal, wheel.StateCaution, wheel.StatePauseBuy, wheel.StateExit}, Help: "战略状态"},
		},
	},
}

// Templates returns a copy of the registered strategy templates.
func Templates() []Template {
	out := slices.Clone(templates)
	for i := range out {
		out[i].Params = slices.Clone(out[i].Params)
		for j := range out[i].Params {
			out[i].Params[j].Allowed = slices.Clone(out[i].Params[j].Allowed)
		}
	}
	return out
}

// Lookup returns the registered template. Old covered-call and
// cash-secured-put names intentionally return false and have no compatibility
// path.
func Lookup(name string) (*Template, bool) {
	for i := range templates {
		if templates[i].Name == name {
			return &templates[i], true
		}
	}
	return nil, false
}

// Config is an alias for the domain wheel configuration used by the adapter.
type Config = wheel.Config

// ParseConfig parses and validates a wheel parameter object. It accepts the
// direct params map used by Factory and, for JSON round-trips, an optional
// {"strategy":"wheel","params":{...}} envelope.
func ParseConfig(params map[string]any) (wheel.Config, error) {
	if params == nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: full_position_price, zero_position_price and max_inventory are required")
	}
	params, err := unwrapParams(params)
	if err != nil {
		return wheel.Config{}, err
	}
	params, audit, err := normalizeLegacyParams(params)
	if err != nil {
		return wheel.Config{}, err
	}
	t, _ := Lookup("wheel")
	values, err := buildParams(t, params)
	if err != nil {
		return wheel.Config{}, err
	}
	fullPrice, err := asNumber(values["full_position_price"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param full_position_price: %w", err)
	}
	zeroPrice, err := asNumber(values["zero_position_price"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param zero_position_price: %w", err)
	}
	maxInventory, err := asNumber(values["max_inventory"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param max_inventory: %w", err)
	}
	moveInterval, err := asNumber(values["move_interval_pct"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param move_interval_pct: %w", err)
	}
	minPremium, err := asNumber(values["min_premium_per_share"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param min_premium_per_share: %w", err)
	}
	minOptionProfit, err := asNumber(values["min_option_profit"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param min_option_profit: %w", err)
	}
	stockSwitch, err := asNumber(values["stock_switch_pct"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param stock_switch_pct: %w", err)
	}
	tradeGap, err := asNumber(values["trade_gap"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param trade_gap: %w", err)
	}
	minDTE, err := asInt(values["min_dte"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param min_dte: %w", err)
	}
	maxDTE, err := asInt(values["max_dte"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param max_dte: %w", err)
	}
	minQuality, err := asNumber(values["min_option_quality"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param min_option_quality: %w", err)
	}
	maxQuoteAge, err := asInt(values["max_quote_age_seconds"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param max_quote_age_seconds: %w", err)
	}
	state, ok := values["strategic_state"].(string)
	if !ok {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param strategic_state: want a choice")
	}

	cfg := wheel.Config{
		Strategy:               "wheel",
		PricePositionCurve:     slices.Clone(audit.priceCurve),
		FullPositionPrice:      fullPrice,
		ZeroPositionPrice:      zeroPrice,
		MaxInventory:           maxInventory,
		MoveIntervalPct:        moveInterval,
		MinPremiumPerShare:     minPremium,
		MinOptionProfit:        minOptionProfit,
		StockSwitchPct:         stockSwitch,
		TradeGap:               tradeGap,
		MinDTE:                 minDTE,
		MaxDTE:                 maxDTE,
		MinOptionQuality:       minQuality,
		StrategicState:         state,
		MaxQuoteAgeSeconds:     maxQuoteAge,
		MigrationLossy:         audit.lossy,
		MigrationWarningCount:  len(audit.warnings),
		MigrationWarnings:      audit.warnings,
		MigrationOriginalCurve: audit.originalCurve,
	}
	if err := cfg.Validate(); err != nil {
		return wheel.Config{}, err
	}
	return cfg, nil
}

// ParseConfigJSON is a convenience for callers holding the structured JSON
// contract rather than a decoded map.
func ParseConfigJSON(data []byte) (wheel.Config, error) {
	var params map[string]any
	if err := json.Unmarshal(data, &params); err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: invalid JSON: %w", err)
	}
	return ParseConfig(params)
}

// CanonicalParams validates params and returns the write-side representation.
// It is the persistence boundary for the read-old/write-new migration policy.
func CanonicalParams(params map[string]any) (map[string]any, error) {
	cfg, err := ParseConfig(params)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("strategy wheel: canonical params: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("strategy wheel: canonical params: %w", err)
	}
	delete(out, "strategy")
	if len(cfg.PricePositionCurve) > 0 {
		// A multi-point curve is the strategic source of truth. The endpoint
		// values are parser compatibility fields only and must not replace the
		// original curve at persistence/report boundaries.
		delete(out, "full_position_price")
		delete(out, "zero_position_price")
	}
	return out, nil
}

// Validate checks the named strategy and its structured params. The only
// registered name is wheel; old template names are explicit unknown errors.
func Validate(name string, params map[string]any) error {
	t, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("strategy: unknown template %q (want llm or wheel)", name)
	}
	if name == "llm" {
		values, err := buildParams(t, params)
		if err != nil {
			return err
		}
		minDTE, err := asInt(values["min_dte"])
		if err != nil {
			return err
		}
		maxDTE, err := asInt(values["max_dte"])
		if err != nil {
			return err
		}
		if minDTE > maxDTE {
			return fmt.Errorf("strategy llm: min_dte must be <= max_dte")
		}
		return nil
	}
	_, err := ParseConfig(params)
	return err
}

// ValidateConfig validates a typed domain configuration.
func ValidateConfig(cfg wheel.Config) error { return cfg.Validate() }

// Factory builds the wheel backtest adapter.
func Factory(name string, params map[string]any) (backtest.Strategy, error) {
	if _, ok := Lookup(name); !ok {
		return nil, fmt.Errorf("strategy: unknown template %q (want wheel)", name)
	}
	cfg, err := ParseConfig(params)
	if err != nil {
		return nil, err
	}
	return &WheelStrategy{Config: cfg}, nil
}

// ContractTemplate is the JSON contract served by GET /v1/strategies.
type ContractTemplate struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Params      []ContractParam `json:"params"`
}

// ContractParam is a structured UI/API field. Type is curve, number, or
// choice, and Required marks fields that users must supply explicitly.
type ContractParam struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Default     any      `json:"default,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Description string   `json:"description"`
}

// ContractTemplates renders the registry without adding engine strategies.
func ContractTemplates() []ContractTemplate {
	out := make([]ContractTemplate, 0, len(templates))
	for _, t := range templates {
		ct := ContractTemplate{Name: t.Name, Description: t.Description}
		for _, p := range t.Params {
			ct.Params = append(ct.Params, ContractParam{
				Name: p.Name, Type: p.Type, Default: p.Default, Required: p.Required,
				Choices: slices.Clone(p.Allowed), Description: p.Help,
			})
		}
		out = append(out, ct)
	}
	return out
}

// WheelStrategy adapts wheel.Evaluate to backtest.Strategy. It consumes only
// the State's current atomic quote batch; legacy option close bars are never
// converted into Greeks or market sides.
type WheelStrategy struct {
	Config     wheel.Config
	LastSignal wheel.Signal
	// Live trade gates need the last effective fill price; the adapter tracks
	// it from FillCount increments (settlement only counts option fills).
	seenFillCount int64
	prevBarClose  float64
	lastFillPrice float64
}

// Wheel is retained as a concise exported type name for callers that want to
// type assert the Factory result.
type Wheel = WheelStrategy

// WheelAdapter is an explicit adapter alias for callers that prefer the
// backtest-facing name.
type WheelAdapter = WheelStrategy

// Signal returns the most recent pure wheel decision.
func (s *WheelStrategy) Signal() wheel.Signal { return s.LastSignal }

func (s *WheelStrategy) OnBar(ctx context.Context, bar ingest.Bar, st *backtest.State) (backtest.Action, float64, error) {
	if err := ctx.Err(); err != nil {
		return backtest.ActionHold, 0, err
	}
	if st == nil {
		s.LastSignal = wheel.Signal{Action: wheel.ActionHold, Direction: wheel.DirectionHold, Reason: "wheel: backtest state is missing", Reasons: []string{"wheel: backtest state is missing"}, CapabilityStatus: wheel.CapabilityDataBlocked, BlockedBy: []string{"backtest_state"}}
		return backtest.ActionHold, 0, nil
	}
	// OnBar runs before this bar's settlement: a FillCount above what the
	// previous bar saw means the previous bar's option attempt filled, so its
	// close becomes the last effective fill price (live gate semantics).
	if st.FillCount > s.seenFillCount {
		s.seenFillCount = st.FillCount
		s.lastFillPrice = s.prevBarClose
	}
	s.prevBarClose = bar.Close
	batch := st.QuoteBatch
	if batch == nil || batch.ObservedAt.IsZero() || batch.SnapshotKey == "" || batch.UnderlyingPrice <= 0 || len(batch.Quotes) == 0 {
		const reason = "wheel: complete atomic quote snapshot unavailable (underlying_price, bid/ask, delta, implied_vol, volume, open_interest, quote_time); backtest is DATA_BLOCKED"
		s.LastSignal = wheel.Signal{Action: wheel.ActionHold, Direction: wheel.DirectionHold, Reason: reason, Reasons: []string{reason}, CapabilityStatus: wheel.CapabilityDataBlocked, BlockedBy: []string{"option_quote_snapshot"}}
		return backtest.ActionHold, 0, nil
	}

	// Historical snapshots contain the full option universe. DTE is a cheap,
	// deterministic loading-time gate, so avoid sending contracts outside this
	// bar's configured expiry window through the hot candidate validator.
	quotes := filterQuotesByExpiry(batch.Quotes, batch.ExpiryOrder, bar.Ts, s.Config)
	quoteByCode := make(map[string]wheel.OptionQuote, len(quotes))
	for _, q := range quotes {
		code := q.Symbol
		if code == "" {
			code = q.Code
		}
		if code != "" {
			quoteByCode[code] = q
		}
	}
	codes := make([]string, 0, len(st.Options))
	for code := range st.Options {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	positions := make([]wheel.OptionPosition, 0, len(codes))
	for _, code := range codes {
		p := st.Options[code]
		q, found := quoteByCode[code]
		delta := p.MarketDelta
		if delta == 0 {
			delta = p.Delta
		}
		if delta == 0 && found {
			delta = q.Delta
			if delta == 0 {
				delta = q.MarketDelta
			}
		}
		if delta == 0 {
			const reason = "wheel: existing option position delta is missing from the current atomic quote batch; HOLD"
			s.LastSignal = wheel.Signal{Action: wheel.ActionHold, Direction: wheel.DirectionHold, Reason: reason, Reasons: []string{reason}, CapabilityStatus: wheel.CapabilityDataBlocked, BlockedBy: []string{"position_delta"}}
			return backtest.ActionHold, 0, nil
		}
		lot := p.Lot
		if lot <= 0 {
			lot = p.LotSize
		}
		positions = append(positions, wheel.OptionPosition{Symbol: code, SignedContracts: p.Contracts, Strike: p.Strike, MarketDelta: delta, Delta: delta, LotSize: lot, OptionType: wheel.OptionType(p.Kind)})
	}
	signal, err := wheel.Evaluate(s.Config, wheel.DecisionInput{
		CurrentPrice: batch.UnderlyingPrice,
		// Validate freshness and DTE against the bar being replayed, not the
		// quote's own timestamp; otherwise a stale batch would validate itself.
		AsOf:                   bar.Ts,
		StockShares:            st.Position,
		Positions:              positions,
		CashAvailable:          st.Cash,
		HasCashAvailable:       true,
		LastEffectiveFillPrice: s.lastFillPrice,
		Quotes:                 quotes,
	})
	if err != nil {
		s.LastSignal = signal
		return backtest.ActionHold, 0, nil
	}
	restoreExpiryRejectedCandidates(&signal, batch.Quotes, quotes, bar.Ts, s.Config)
	s.LastSignal = signal
	// 急涨急跌直接买卖正股(wheel 既有机制):stock_switch_pct 触发时 Evaluate
	// 只给正股建议,人工处置在线下由人执行;回测将其机械化为对应方向的持仓调整。
	if signal.StockSuggestion != nil {
		shares := signal.StockSuggestion.Shares
		if shares <= 0 || (signal.StockSuggestion.Side != "BUY" && signal.StockSuggestion.Side != "SELL") {
			return backtest.ActionHold, 0, nil
		}
		if signal.StockSuggestion.Side == "SELL" {
			// 资金安全:正股卖出永不超出实际持仓(不裸卖)。缺口里含期权 delta
			// 折算的库存,超出实际股数的部分由人工处置线下对齐,回测不代做空。
			if shares > st.Position {
				shares = st.Position
			}
			if shares <= 0 {
				return backtest.ActionHold, 0, nil
			}
			return backtest.ActionSell, shares, nil
		}
		return backtest.ActionBuy, shares, nil
	}
	if signal.Action != wheel.ActionAlert || signal.Quote == nil || signal.Quantity <= 0 {
		return backtest.ActionHold, 0, nil
	}
	q := *signal.Quote
	kind := backtest.OptionKind(string(q.OptionType))
	if kind != backtest.OptionCall && kind != backtest.OptionPut {
		return backtest.ActionHold, 0, nil
	}
	premium := q.Bid
	if premium <= 0 { // Evaluate normally rejects this; retain fail-closed behavior.
		return backtest.ActionHold, 0, nil
	}
	st.Pending = &backtest.OptionPosition{Code: q.Symbol, Kind: kind, Strike: q.Strike, Expiry: q.Expiry, Lot: q.LotSize, Contracts: -float64(signal.Quantity), AvgPremium: premium, MarketDelta: q.Delta}
	if st.Pending.Code == "" {
		st.Pending.Code = q.Code
	}
	switch signal.Direction {
	case wheel.DirectionPut:
		return backtest.ActionSellPut, float64(signal.Quantity), nil
	case wheel.DirectionCall:
		return backtest.ActionSellCall, float64(signal.Quantity), nil
	default:
		st.Pending = nil
		return backtest.ActionHold, 0, nil
	}
}

func filterQuotesByExpiry(quotes []wheel.OptionQuote, expiryOrder []int, barTs time.Time, cfg wheel.Config) []wheel.OptionQuote {
	if barTs.IsZero() {
		return slices.Clone(quotes)
	}
	asOfDate := calendarDate(barTs)
	minExpiryDate := asOfDate.AddDate(0, 0, cfg.MinDTE)
	maxExpiryDate := asOfDate.AddDate(0, 0, cfg.MaxDTE)
	if len(expiryOrder) == len(quotes) {
		seen := make([]bool, len(quotes))
		var previousExpiry time.Time
		for _, index := range expiryOrder {
			if index < 0 || index >= len(quotes) || seen[index] {
				return filterQuotesByExpiryLinear(quotes, minExpiryDate, maxExpiryDate)
			}
			seen[index] = true
			expiry := calendarDate(quotes[index].Expiry)
			if !previousExpiry.IsZero() && expiry.Before(previousExpiry) {
				return filterQuotesByExpiryLinear(quotes, minExpiryDate, maxExpiryDate)
			}
			previousExpiry = expiry
		}
		lo := sort.Search(len(expiryOrder), func(i int) bool {
			return !calendarDate(quotes[expiryOrder[i]].Expiry).Before(minExpiryDate)
		})
		hi := sort.Search(len(expiryOrder), func(i int) bool {
			return calendarDate(quotes[expiryOrder[i]].Expiry).After(maxExpiryDate)
		})
		included := make([]bool, len(quotes))
		for _, index := range expiryOrder[lo:hi] {
			included[index] = true
		}
		filtered := make([]wheel.OptionQuote, 0, hi-lo)
		for i, q := range quotes {
			if included[i] || q.Expiry.IsZero() {
				filtered = append(filtered, q)
			}
		}
		return filtered
	}
	return filterQuotesByExpiryLinear(quotes, minExpiryDate, maxExpiryDate)
}

func filterQuotesByExpiryLinear(quotes []wheel.OptionQuote, minExpiryDate, maxExpiryDate time.Time) []wheel.OptionQuote {
	filtered := make([]wheel.OptionQuote, 0, len(quotes))
	for _, q := range quotes {
		if q.Expiry.IsZero() {
			filtered = append(filtered, q)
			continue
		}
		expiryDate := calendarDate(q.Expiry)
		if expiryDate.Before(minExpiryDate) || expiryDate.After(maxExpiryDate) {
			continue
		}
		filtered = append(filtered, q)
	}
	return filtered
}

// restoreExpiryRejectedCandidates keeps the signal/report contract identical
// to the unpruned evaluator. Out-of-window quotes cannot affect selection, so
// they are omitted from the hot validation loop and represented here as cheap
// rejected details only when the signal is materialized.
func restoreExpiryRejectedCandidates(signal *wheel.Signal, allQuotes, evaluatedQuotes []wheel.OptionQuote, barTs time.Time, cfg wheel.Config) {
	if signal == nil {
		return
	}
	direction := signal.Direction
	if direction == wheel.DirectionHold && signal.Reason == "no quote passed validation and risk checks" {
		// Evaluate changes Direction back to HOLD when every quote is
		// rejected. The inventory gap still carries the direction that the
		// unpruned evaluator used while constructing candidate diagnostics.
		direction = wheel.DirectionPut
		if signal.InventoryGap < 0 {
			direction = wheel.DirectionCall
		}
	}
	if direction != wheel.DirectionPut && direction != wheel.DirectionCall {
		return
	}
	byName := make(map[string][]wheel.CandidateEvaluation, len(signal.Candidates))
	for _, candidate := range signal.Candidates {
		name := candidate.Quote.Symbol
		if name == "" {
			name = candidate.Quote.Code
		}
		byName[name] = append(byName[name], candidate)
	}
	seen := make(map[string]int, len(evaluatedQuotes))
	merged := make([]wheel.CandidateEvaluation, 0, len(allQuotes))
	asOfDate := calendarDate(barTs)
	minExpiryDate := asOfDate.AddDate(0, 0, cfg.MinDTE)
	maxExpiryDate := asOfDate.AddDate(0, 0, cfg.MaxDTE)
	for _, quote := range allQuotes {
		name := quote.Symbol
		if name == "" {
			name = quote.Code
		}
		index := seen[name]
		candidates := byName[name]
		if index < len(candidates) {
			merged = append(merged, candidates[index])
			seen[name] = index + 1
			continue
		}
		if quote.Expiry.IsZero() || (!calendarDate(quote.Expiry).Before(minExpiryDate) && !calendarDate(quote.Expiry).After(maxExpiryDate)) {
			continue
		}
		reason := rejectedExpiryQuoteReason(quote, barTs, cfg, asOfDate, minExpiryDate, maxExpiryDate)
		merged = append(merged, wheel.CandidateEvaluation{
			Quote: quote, Direction: direction, Quantity: 1, SignedContracts: -1,
			Quality: wheel.QualityScore(quote), Reasons: []string{reason},
		})
	}
	if len(merged) == 0 {
		return
	}
	signal.Candidates = merged
	signal.RejectReasons = nil
	for _, candidate := range merged {
		if !candidate.Accepted {
			signal.RejectReasons = append(signal.RejectReasons, candidate.Reasons...)
		}
	}
}

// rejectedExpiryQuoteReason mirrors the validation order before DTE. It is
// used only while materializing candidate diagnostics after the expiry gate;
// the hot evaluator never formats these rejected-candidate reasons.
func rejectedExpiryQuoteReason(q wheel.OptionQuote, asOf time.Time, cfg wheel.Config, asOfDate, minExpiryDate, maxExpiryDate time.Time) string {
	delta := q.Delta
	if delta == 0 {
		delta = q.MarketDelta
	}
	typ := strings.ToLower(string(q.OptionType))
	if typ == "" {
		typ = strings.ToLower(string(q.Type))
	}
	iv := q.ImpliedVol
	if iv == 0 {
		iv = q.IV
	}
	oi := q.OpenInterest
	if oi == 0 {
		oi = q.OI
	}
	name := q.Symbol
	if name == "" {
		name = q.Code
	}
	if name == "" || strings.TrimSpace(q.Source) == "" || q.Expiry.IsZero() || !finiteNumber(delta) || delta < -1 || delta > 1 || !finiteNumber(q.Strike) || q.Strike <= 0 {
		return "wheel: quote missing symbol, source, expiry, strike or delta"
	}
	if typ != string(wheel.Put) && typ != string(wheel.Call) {
		return "wheel: quote has invalid option type"
	}
	if typ == string(wheel.Put) && delta >= 0 {
		return "wheel: put delta must be negative"
	}
	if typ == string(wheel.Call) && delta <= 0 {
		return "wheel: call delta must be positive"
	}
	if !finiteNumber(q.Bid) || !finiteNumber(q.Ask) || q.Bid <= 0 || q.Ask <= 0 || q.Ask < q.Bid {
		return "wheel: quote has missing or inverted bid/ask"
	}
	if !finiteNumber(iv) || iv <= 0 || q.Theta == nil || !finiteNumber(*q.Theta) || q.Volume <= 0 || oi <= 0 || q.LotSize <= 0 {
		return "wheel: quote missing IV, Theta, liquidity or lot size"
	}
	quoteTime := q.QuoteTime
	if quoteTime.IsZero() {
		quoteTime = q.CapturedAt
	}
	if quoteTime.IsZero() {
		quoteTime = q.Timestamp
	}
	if quoteTime.IsZero() {
		quoteTime = q.Ts
	}
	if !asOf.IsZero() && quoteTime.IsZero() {
		return "wheel: quote timestamp is missing"
	}
	if !asOf.IsZero() && quoteTime.After(asOf) {
		return "wheel: quote is from the future"
	}
	if !asOf.IsZero() && asOf.Sub(quoteTime) > cfg.QuoteMaxAge() {
		return "wheel: quote is stale"
	}
	if !asOf.IsZero() && !q.Expiry.After(asOf) {
		return "wheel: option has expired"
	}
	dte := int(calendarDate(q.Expiry).Sub(asOfDate) / (24 * time.Hour))
	if calendarDate(q.Expiry).Before(minExpiryDate) || calendarDate(q.Expiry).After(maxExpiryDate) {
		return fmt.Sprintf("wheel: DTE %d outside [%d,%d]", dte, cfg.MinDTE, cfg.MaxDTE)
	}
	return "wheel: option expiry is outside configured range"
}

func calendarDate(ts time.Time) time.Time {
	utc := ts.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func unwrapParams(params map[string]any) (map[string]any, error) {
	if raw, ok := params["strategy"]; ok {
		name, ok := raw.(string)
		if !ok || name != "wheel" {
			return nil, fmt.Errorf("strategy: unknown template %q (want wheel)", raw)
		}
		nested, ok := params["params"]
		if !ok {
			// A typed wheel.Config marshals as a flat object with a strategy
			// field. Accept that representation as well as the API envelope.
			out := make(map[string]any, len(params)-1)
			for key, value := range params {
				if key != "strategy" {
					out[key] = value
				}
			}
			return out, nil
		}
		out, ok := nested.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("strategy wheel: params must be an object")
		}
		return out, nil
	}
	return params, nil
}

type migrationAudit struct {
	lossy         bool
	warnings      []string
	originalCurve json.RawMessage
	priceCurve    []wheel.PricePoint
}

// normalizeLegacyParams implements the read-old/write-new boundary. Legacy
// keys are removed before schema validation, so a Config marshals only the new
// contract plus explicit migration audit metadata.
func normalizeLegacyParams(params map[string]any) (map[string]any, migrationAudit, error) {
	out := make(map[string]any, len(params))
	for key, value := range params {
		out[key] = value
	}
	audit := migrationAudit{}

	if raw, ok := out["migration_lossy"]; ok {
		if lossy, ok := raw.(bool); ok {
			audit.lossy = lossy
		} else {
			return nil, audit, fmt.Errorf("strategy wheel: param migration_lossy: want a boolean")
		}
		delete(out, "migration_lossy")
	}
	if raw, ok := out["migration_original_price_position_curve"]; ok {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, audit, fmt.Errorf("strategy wheel: migration curve audit: %w", err)
		}
		audit.originalCurve = encoded
		delete(out, "migration_original_price_position_curve")
	}
	if raw, ok := out["migration_warnings"]; ok {
		encoded, err := json.Marshal(raw)
		if err != nil || json.Unmarshal(encoded, &audit.warnings) != nil {
			return nil, audit, fmt.Errorf("strategy wheel: param migration_warnings: want a string array")
		}
		delete(out, "migration_warnings")
	}
	delete(out, "migration_warning_count") // recomputed from the warning list

	if rawCurve, hasCurve := out["price_position_curve"]; hasCurve {
		curve, err := parseCurve(rawCurve)
		if err != nil {
			return nil, audit, fmt.Errorf("strategy wheel: param price_position_curve: %w", err)
		}
		if len(curve) < 2 {
			return nil, audit, fmt.Errorf("strategy wheel: param price_position_curve: must contain at least two points")
		}
		for i, point := range curve {
			if !finiteNumber(point.Price) || point.Price <= 0 || !finiteNumber(point.TargetInventory) || point.TargetInventory < 0 {
				return nil, audit, fmt.Errorf("strategy wheel: param price_position_curve: invalid point %d", i)
			}
			if i > 0 && (point.Price <= curve[i-1].Price || point.TargetInventory > curve[i-1].TargetInventory) {
				return nil, audit, fmt.Errorf("strategy wheel: param price_position_curve: prices must increase and target inventory must not increase")
			}
		}
		audit.priceCurve = curve
		if _, hasFull := out["full_position_price"]; !hasFull {
			out["full_position_price"] = curve[0].Price
		}
		if _, hasZero := out["zero_position_price"]; !hasZero {
			out["zero_position_price"] = curve[len(curve)-1].Price
		}
		delete(out, "price_position_curve")
	}
	if rawGap, ok := out["no_trade_gap"]; ok {
		if _, hasNew := out["trade_gap"]; !hasNew {
			out["trade_gap"] = rawGap
		}
		delete(out, "no_trade_gap")
	}
	for _, key := range []string{"max_daily_orders", "extreme_max_daily_orders", "lot_size"} {
		if _, ok := out[key]; ok {
			audit.warnings = append(audit.warnings, "ignored legacy parameter: "+key)
			delete(out, key)
		}
	}
	return out, audit, nil
}

func finiteNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func buildParams(t *Template, params map[string]any) (map[string]any, error) {
	known := make(map[string]Param, len(t.Params))
	for _, p := range t.Params {
		known[p.Name] = p
	}
	for key := range params {
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("strategy %s: unknown param %q", t.Name, key)
		}
	}
	out := make(map[string]any, len(t.Params))
	for _, p := range t.Params {
		v, present := params[p.Name]
		if !present || v == nil {
			if p.Required {
				return nil, fmt.Errorf("strategy %s: param %s is required", t.Name, p.Name)
			}
			out[p.Name] = p.Default
			continue
		}
		switch p.Type {
		case "curve":
			if _, err := parseCurve(v); err != nil {
				return nil, fmt.Errorf("strategy %s: param %s: %w", t.Name, p.Name, err)
			}
			out[p.Name] = v
		case "number":
			f, err := asNumber(v)
			if err != nil {
				return nil, fmt.Errorf("strategy %s: param %s: %v", t.Name, p.Name, err)
			}
			if f < p.Min || f > p.Max {
				return nil, fmt.Errorf("strategy %s: param %s = %v; want in [%v, %v]", t.Name, p.Name, f, p.Min, p.Max)
			}
			out[p.Name] = f
		case "choice":
			s, ok := v.(string)
			if !ok || !slices.Contains(p.Allowed, s) {
				return nil, fmt.Errorf("strategy %s: param %s = %v; want one of %v", t.Name, p.Name, v, p.Allowed)
			}
			out[p.Name] = s
		}
	}
	return out, nil
}

func parseCurve(v any) ([]wheel.PricePoint, error) {
	if typed, ok := v.([]wheel.PricePoint); ok {
		return slices.Clone(typed), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("want an array of {price,target_inventory}: %v", err)
	}
	var curve []wheel.PricePoint
	if err := json.Unmarshal(b, &curve); err != nil || len(curve) == 0 {
		if err == nil {
			err = fmt.Errorf("must contain at least two points")
		}
		return nil, fmt.Errorf("want an array of {price,target_inventory}: %v", err)
	}
	return curve, nil
}

// asNumber accepts numeric values produced by JSON and common typed callers.
func asNumber(v any) (float64, error) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	case int:
		f = float64(n)
	case int8:
		f = float64(n)
	case int16:
		f = float64(n)
	case int32:
		f = float64(n)
	case int64:
		f = float64(n)
	case uint:
		f = float64(n)
	case uint8:
		f = float64(n)
	case uint16:
		f = float64(n)
	case uint32:
		f = float64(n)
	case uint64:
		f = float64(n)
	case json.Number:
		var err error
		f, err = n.Float64()
		if err != nil {
			return 0, fmt.Errorf("want a number, got %q", n)
		}
	default:
		return 0, fmt.Errorf("want a number, got %T", v)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("want a finite number, got %v", f)
	}
	return f, nil
}

func asInt(v any) (int, error) {
	f, err := asNumber(v)
	if err != nil {
		return 0, err
	}
	if math.Trunc(f) != f || f < 1 || f > float64(^uint(0)>>1) {
		return 0, fmt.Errorf("want a positive integer, got %v", f)
	}
	return int(f), nil
}

var _ backtest.Strategy = (*WheelStrategy)(nil)
