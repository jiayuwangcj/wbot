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
		Description:  "按价格—目标库存曲线管理库存，只生成人工提醒，不自动下单",
		NeedsOptions: true,
		Params: []Param{
			{Name: "price_position_curve", Type: "curve", Required: true, Help: "价格递增、目标库存单调不增的价格—目标库存锚点"},
			{Name: "max_inventory", Type: "number", Required: true, Min: 0, Max: math.MaxFloat64, Help: "允许的最大实际库存"},
			{Name: "lot_size", Type: "number", Default: 100.0, Min: 1, Max: math.MaxFloat64, Help: "期权合约乘数"},
			{Name: "min_dte", Type: "number", Default: 5.0, Min: 5, Max: 10, Help: "最小到期天数（DTE）"},
			{Name: "max_dte", Type: "number", Default: 10.0, Min: 5, Max: 10, Help: "最大到期天数（DTE）"},
			{Name: "min_option_quality", Type: "number", Default: 0.6, Min: 0, Max: 1, Help: "候选期权质量最低门槛"},
			{Name: "max_daily_orders", Type: "number", Default: 1.0, Min: 1, Max: 1, Help: "正常日最多提醒张数"},
			{Name: "extreme_max_daily_orders", Type: "number", Default: 2.0, Min: 1, Max: 2, Help: "极端日最多提醒张数"},
			{Name: "no_trade_gap", Type: "number", Default: 50.0, Min: 0, Max: math.MaxFloat64, Help: "库存缺口不超过此值时不交易"},
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
		return wheel.Config{}, fmt.Errorf("strategy wheel: price_position_curve and max_inventory are required")
	}
	params, err := unwrapParams(params)
	if err != nil {
		return wheel.Config{}, err
	}

	t, _ := Lookup("wheel")
	values, err := buildParams(t, params)
	if err != nil {
		return wheel.Config{}, err
	}
	curve, err := parseCurve(values["price_position_curve"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param price_position_curve: %w", err)
	}
	maxInventory, err := asNumber(values["max_inventory"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param max_inventory: %w", err)
	}
	lotSize, err := asInt(values["lot_size"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param lot_size: %w", err)
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
	maxDaily, err := asInt(values["max_daily_orders"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param max_daily_orders: %w", err)
	}
	extremeDaily, err := asInt(values["extreme_max_daily_orders"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param extreme_max_daily_orders: %w", err)
	}
	noTradeGap, err := asNumber(values["no_trade_gap"])
	if err != nil {
		return wheel.Config{}, fmt.Errorf("strategy wheel: param no_trade_gap: %w", err)
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
		Strategy:              "wheel",
		PricePositionCurve:    curve,
		MaxInventory:          maxInventory,
		LotSize:               lotSize,
		MinDTE:                minDTE,
		MaxDTE:                maxDTE,
		MinOptionQuality:      minQuality,
		MaxDailyOrders:        maxDaily,
		ExtremeMaxDailyOrders: extremeDaily,
		NoTradeGap:            noTradeGap,
		StrategicState:        state,
		MaxQuoteAgeSeconds:    maxQuoteAge,
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

// Validate checks the named product strategy and its structured params.
// Legacy template names remain explicit unknown errors.
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
	batch := st.QuoteBatch
	if batch == nil || batch.ObservedAt.IsZero() || batch.SnapshotKey == "" || batch.UnderlyingPrice <= 0 || len(batch.Quotes) == 0 {
		const reason = "wheel: complete atomic quote snapshot unavailable (underlying_price, bid/ask, delta, implied_vol, volume, open_interest, quote_time); backtest is DATA_BLOCKED"
		s.LastSignal = wheel.Signal{Action: wheel.ActionHold, Direction: wheel.DirectionHold, Reason: reason, Reasons: []string{reason}, CapabilityStatus: wheel.CapabilityDataBlocked, BlockedBy: []string{"option_quote_snapshot"}}
		return backtest.ActionHold, 0, nil
	}

	quotes := slices.Clone(batch.Quotes)
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
		AsOf:             bar.Ts,
		StockShares:      st.Position,
		Positions:        positions,
		DailyOrders:      st.DailyOrders,
		ExtremeDay:       st.ExtremeDay,
		CashAvailable:    st.Cash,
		HasCashAvailable: true,
		Quotes:           quotes,
	})
	if err != nil {
		s.LastSignal = signal
		return backtest.ActionHold, 0, nil
	}
	s.LastSignal = signal
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
