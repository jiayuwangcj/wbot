// Package strategy registers parameterized backtest strategy templates
// (covered-call, cash-secured-put); Factory validates params against the
// template schema and returns a ready backtest.Strategy.
package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/jiayu/wbot/internal/backtest"
)

// Param is one template parameter's schema: type (number|string), default and
// validation range (Min/Max for numbers, Allowed values for strings).
type Param struct {
	Name    string
	Type    string
	Default any
	Min     float64
	Max     float64
	Allowed []string
	Help    string
}

// Template describes one registered strategy template (name + schema).
type Template struct {
	Name         string
	Description  string
	NeedsOptions bool // requires option_quotes/chain input (backtest RunOptions)
	Params       []Param
}

// templates is the strategy registry; Templates() exposes it read-only.
var templates = []Template{
	{
		Name:         "covered-call",
		Description:  "持有正股（lot 股）+ 卖出 1 张价外看涨，到期结算后自动滚仓",
		NeedsOptions: true,
		Params: []Param{
			{Name: "strike_pct_otm", Type: "number", Default: 0.03, Min: 0, Max: math.MaxFloat64, Help: "目标行权价偏离率：现价×(1+pct)"},
			{Name: "expiry_rule", Type: "string", Default: "next_expiry", Allowed: []string{"next_expiry", "days"}, Help: "到期选择：next_expiry 最近到期 / days 按 days_to_expiry"},
			{Name: "days_to_expiry", Type: "number", Default: 28.0, Min: 1, Max: math.MaxFloat64, Help: "expiry_rule=days 时的目标到期天数"},
			{Name: "fee_per_contract", Type: "number", Default: 0.0, Min: 0, Max: math.MaxFloat64, Help: "每张合约费用（从权利金中扣除）"},
			{Name: "lot_size", Type: "number", Default: 100.0, Min: 1, Max: math.MaxFloat64, Help: "合约乘数（option_quotes 无 lot 列，以参数为准）"},
		},
	},
	{
		Name:         "cash-secured-put",
		Description:  "现金担保卖出价外看跌；被行权按 strike 买入正股并于下一 bar 卖出，到期结算后滚仓",
		NeedsOptions: true,
		Params: []Param{
			{Name: "strike_pct_otm", Type: "number", Default: 0.03, Min: 0, Max: math.MaxFloat64, Help: "目标行权价偏离率：现价×(1-pct)"},
			{Name: "expiry_rule", Type: "string", Default: "next_expiry", Allowed: []string{"next_expiry", "days"}, Help: "到期选择：next_expiry 最近到期 / days 按 days_to_expiry"},
			{Name: "days_to_expiry", Type: "number", Default: 28.0, Min: 1, Max: math.MaxFloat64, Help: "expiry_rule=days 时的目标到期天数"},
			{Name: "fee_per_contract", Type: "number", Default: 0.0, Min: 0, Max: math.MaxFloat64, Help: "每张合约费用（从权利金中扣除）"},
			{Name: "lot_size", Type: "number", Default: 100.0, Min: 1, Max: math.MaxFloat64, Help: "合约乘数（option_quotes 无 lot 列，以参数为准）"},
			{Name: "cash_reserve", Type: "number", Default: 1.0, Min: 1, Max: math.MaxFloat64, Help: "现金担保倍率：开仓要求 cash >= cash_reserve×strike×lot×张数"},
		},
	},
}

// Templates lists the registered strategy templates (read-only).
func Templates() []Template {
	return slices.Clone(templates)
}

// Lookup returns the template for name (false when unknown).
func Lookup(name string) (*Template, bool) {
	for i := range templates {
		if templates[i].Name == name {
			return &templates[i], true
		}
	}
	return nil, false
}

// Factory builds a strategy from a template name and params; unknown names,
// unknown param keys, wrong types and out-of-range values all error.
func Factory(name string, params map[string]any) (backtest.Strategy, error) {
	t, ok := Lookup(name)
	if !ok {
		names := make([]string, 0, len(templates))
		for _, tt := range templates {
			names = append(names, tt.Name)
		}
		return nil, fmt.Errorf("strategy: unknown template %q (want %s)", name, strings.Join(names, " or "))
	}
	p, err := buildParams(t, params)
	if err != nil {
		return nil, err
	}
	switch name {
	case "covered-call":
		return &CoveredCall{base: baseFrom(p)}, nil
	case "cash-secured-put":
		return &CashSecuredPut{base: baseFrom(p), cashReserve: p["cash_reserve"].(float64)}, nil
	}
	return nil, fmt.Errorf("strategy: template %q registered but not implemented", name)
}

// buildParams merges defaults and validates every param against the schema.
func buildParams(t *Template, params map[string]any) (map[string]any, error) {
	for k := range params {
		if !slices.ContainsFunc(t.Params, func(p Param) bool { return p.Name == k }) {
			return nil, fmt.Errorf("strategy %s: unknown param %q", t.Name, k)
		}
	}
	out := make(map[string]any, len(t.Params))
	for _, p := range t.Params {
		v, ok := params[p.Name]
		if !ok {
			out[p.Name] = p.Default
			continue
		}
		switch p.Type {
		case "number":
			f, err := asNumber(v)
			if err != nil {
				return nil, fmt.Errorf("strategy %s: param %s: %v", t.Name, p.Name, err)
			}
			if f < p.Min || f > p.Max {
				return nil, fmt.Errorf("strategy %s: param %s = %v; want in [%v, %v]", t.Name, p.Name, f, p.Min, p.Max)
			}
			out[p.Name] = f
		case "string":
			s, ok := v.(string)
			if !ok || !slices.Contains(p.Allowed, s) {
				return nil, fmt.Errorf("strategy %s: param %s = %v; want one of %v", t.Name, p.Name, v, p.Allowed)
			}
			out[p.Name] = s
		}
	}
	return out, nil
}

// asNumber accepts the numeric shapes json.Unmarshal yields (float64, int...).
func asNumber(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	}
	return 0, fmt.Errorf("want a number, got %T", v)
}
