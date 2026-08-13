package strategy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/backtest"
	"github.com/jiayu/wbot/internal/ingest"
	"github.com/jiayu/wbot/internal/wheel"
)

func validParams() map[string]any {
	return map[string]any{
		"full_position_price": 400.0,
		"zero_position_price": 550.0,
		"max_inventory":       1200.0,
	}
}

func TestRegistryIncludesLLMAndWheel(t *testing.T) {
	templates := Templates()
	if len(templates) != 2 || templates[0].Name != "llm" || templates[1].Name != "wheel" {
		t.Fatalf("Templates() = %+v; want llm and wheel", templates)
	}
	if !templates[0].NeedsOptions || !templates[1].NeedsOptions {
		t.Fatal("wheel must declare that it needs option data")
	}
	if _, ok := Lookup("covered-call"); ok {
		t.Fatal("covered-call must be unknown")
	}
	if _, ok := Lookup("cash-secured-put"); ok {
		t.Fatal("cash-secured-put must be unknown")
	}
	for _, p := range templates[0].Params {
		if p.Type != "number" && p.Type != "choice" {
			t.Fatalf("%s has unsupported schema type %q", p.Name, p.Type)
		}
	}
}

func TestContractSchemaRequiredAndDefaults(t *testing.T) {
	got := ContractTemplates()
	if len(got) != 2 || got[0].Name != "llm" || got[1].Name != "wheel" {
		t.Fatalf("ContractTemplates() = %+v", got)
	}
	wheelContract := got[1]
	byName := make(map[string]ContractParam)
	for _, p := range wheelContract.Params {
		byName[p.Name] = p
	}
	for _, name := range []string{"full_position_price", "zero_position_price", "max_inventory"} {
		p, ok := byName[name]
		if !ok || !p.Required {
			t.Fatalf("%s = %+v; want required", name, p)
		}
	}
	if byName["full_position_price"].Type != "number" || byName["zero_position_price"].Type != "number" || byName["max_inventory"].Type != "number" {
		t.Fatalf("critical schema = %+v %+v %+v", byName["full_position_price"], byName["zero_position_price"], byName["max_inventory"])
	}
	defaults := map[string]any{
		"move_interval_pct": 0.0, "min_premium_per_share": 0.0,
		"stock_switch_pct": 0.0, "trade_gap": 50.0,
		"min_dte": 5.0, "max_dte": 10.0, "min_option_quality": 0.6,
		"max_quote_age_seconds": 86400.0, "strategic_state": wheel.StateNormal,
	}
	for name, want := range defaults {
		if got := byName[name].Default; got != want {
			t.Fatalf("default %s = %#v; want %#v", name, got, want)
		}
	}
	if got := byName["strategic_state"]; got.Type != "choice" || len(got.Choices) != 4 {
		t.Fatalf("strategic_state schema = %+v; want choice", got)
	}
}

func TestParseConfigRequiresStrategicInputs(t *testing.T) {
	for _, name := range []string{"full_position_price", "zero_position_price", "max_inventory"} {
		params := validParams()
		delete(params, name)
		if _, err := ParseConfig(params); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("missing %s: error = %v", name, err)
		}
	}
	cfg, err := ParseConfig(validParams())
	if err != nil {
		t.Fatalf("ParseConfig(defaults) error: %v", err)
	}
	if cfg.MoveIntervalPct != 0 || cfg.MinPremiumPerShare != 0 || cfg.StockSwitchPct != 0 || cfg.TradeGap != 50 ||
		cfg.MinDTE != 5 || cfg.MaxDTE != 10 || cfg.MinOptionQuality != 0.6 || cfg.MaxQuoteAgeSeconds != 86400 ||
		cfg.StrategicState != wheel.StateNormal {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestParseConfigMaxQuoteAgeSeconds(t *testing.T) {
	params := validParams()
	params["max_quote_age_seconds"] = 3600
	cfg, err := ParseConfig(params)
	if err != nil {
		t.Fatalf("ParseConfig(explicit) error: %v", err)
	}
	if cfg.MaxQuoteAgeSeconds != 3600 {
		t.Fatalf("MaxQuoteAgeSeconds = %d; want 3600", cfg.MaxQuoteAgeSeconds)
	}

	cfg, err = ParseConfig(validParams())
	if err != nil {
		t.Fatalf("ParseConfig(missing) error: %v", err)
	}
	if cfg.MaxQuoteAgeSeconds != 86400 {
		t.Fatalf("default MaxQuoteAgeSeconds = %d; want 86400", cfg.MaxQuoteAgeSeconds)
	}

	for _, bad := range []any{0, -1, "abc"} {
		p := validParams()
		p["max_quote_age_seconds"] = bad
		if _, err := ParseConfig(p); err == nil || !strings.Contains(err.Error(), "max_quote_age_seconds") {
			t.Fatalf("invalid %v: error = %v; want max_quote_age_seconds error", bad, err)
		}
	}
}

func TestParseConfigValidationAndRoundTrip(t *testing.T) {
	params := validParams()
	params["strategic_state"] = wheel.StateCaution
	params["move_interval_pct"] = 0.018
	cfg, err := ParseConfig(params)
	if err != nil {
		t.Fatalf("ParseConfig() error: %v", err)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["strategy"] = "wheel"
	parsed, err := ParseConfig(decoded)
	if err != nil {
		t.Fatalf("ParseConfig(JSON map) error: %v", err)
	}
	if parsed.MaxInventory != cfg.MaxInventory || parsed.MoveIntervalPct != cfg.MoveIntervalPct || parsed.StrategicState != cfg.StrategicState {
		t.Fatalf("round trip = %+v; want %+v", parsed, cfg)
	}

	bad := validParams()
	bad["zero_position_price"] = 400.0
	if _, err := ParseConfig(bad); err == nil || !strings.Contains(err.Error(), "zero_position_price") {
		t.Fatalf("invalid price boundary error = %v", err)
	}
	bad = validParams()
	bad["max_inventory"] = "1200"
	if _, err := ParseConfig(bad); err == nil || !strings.Contains(err.Error(), "number") {
		t.Fatalf("invalid max_inventory error = %v", err)
	}
}

func TestParseConfigMigratesLegacyParamsWithAudit(t *testing.T) {
	legacyCurve := []any{
		map[string]any{"price": 40.0, "target_inventory": 22000.0},
		map[string]any{"price": 48.0, "target_inventory": 11000.0},
		map[string]any{"price": 55.0, "target_inventory": 0.0},
	}
	cfg, err := ParseConfig(map[string]any{
		"price_position_curve":     legacyCurve,
		"max_inventory":            22000,
		"no_trade_gap":             50,
		"max_daily_orders":         1,
		"extreme_max_daily_orders": 2,
		"lot_size":                 100,
	})
	if err != nil {
		t.Fatalf("ParseConfig(legacy) error: %v", err)
	}
	if cfg.FullPositionPrice != 40 || cfg.ZeroPositionPrice != 55 || cfg.TradeGap != 50 {
		t.Fatalf("migrated config = %+v", cfg)
	}
	if !cfg.MigrationLossy || cfg.MigrationWarningCount != 3 || len(cfg.MigrationWarnings) != 3 {
		t.Fatalf("migration audit = %+v", cfg)
	}
	var preserved []map[string]any
	if err := json.Unmarshal(cfg.MigrationOriginalCurve, &preserved); err != nil || len(preserved) != 3 || preserved[1]["price"] != 48.0 {
		t.Fatalf("preserved curve = %s err=%v", cfg.MigrationOriginalCurve, err)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, oldKey := range []string{`"price_position_curve"`, `"no_trade_gap"`, `"max_daily_orders"`, `"extreme_max_daily_orders"`, `"lot_size"`} {
		if strings.Contains(string(b), oldKey) && oldKey != `"price_position_curve"` {
			t.Fatalf("new config wrote legacy key %s: %s", oldKey, b)
		}
	}
	if strings.Contains(string(b), `"price_position_curve":`) {
		t.Fatalf("new config wrote legacy curve key: %s", b)
	}
}

func TestParseConfigAcceptsStored00883And09988Shapes(t *testing.T) {
	for _, tc := range []struct {
		symbol                string
		full, zero, inventory float64
	}{
		{symbol: "HK.00883", full: 40, zero: 55, inventory: 22000},
		{symbol: "HK.09988", full: 90, zero: 130, inventory: 1000},
	} {
		t.Run(tc.symbol, func(t *testing.T) {
			cfg, err := ParseConfig(map[string]any{
				"price_position_curve": []any{
					map[string]any{"price": tc.full, "target_inventory": tc.inventory},
					map[string]any{"price": tc.zero, "target_inventory": 0},
				},
				"max_inventory": tc.inventory,
				"no_trade_gap":  50,
				"lot_size":      100,
			})
			if err != nil {
				t.Fatalf("stored config did not parse: %v", err)
			}
			if cfg.FullPositionPrice != tc.full || cfg.ZeroPositionPrice != tc.zero || cfg.MoveIntervalPct != 0 || cfg.MinPremiumPerShare != 0 || cfg.StockSwitchPct != 0 {
				t.Fatalf("migrated config = %+v", cfg)
			}
		})
	}
}

func TestOldNamesUnknown(t *testing.T) {
	for _, name := range []string{"covered-call", "cash-secured-put", "nope"} {
		if err := Validate(name, validParams()); err == nil || !strings.Contains(err.Error(), "unknown template") {
			t.Fatalf("Validate(%q) = %v; want unknown template", name, err)
		}
		if _, err := Factory(name, validParams()); err == nil || !strings.Contains(err.Error(), "unknown template") {
			t.Fatalf("Factory(%q) = %v; want unknown template", name, err)
		}
	}
}

func TestWheelAdapterHoldsWithoutRealTimeSnapshot(t *testing.T) {
	s, err := Factory("wheel", validParams())
	if err != nil {
		t.Fatal(err)
	}
	ws, ok := s.(*WheelStrategy)
	if !ok {
		t.Fatalf("Factory type = %T; want *WheelStrategy", s)
	}
	state := &backtest.State{Cash: 100000, Position: 0, Options: map[string]backtest.OptionPosition{}, OptBars: backtest.OptionBars{
		// A close-only option bar must not be interpreted as a quote.
		"P": {{Ts: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 10}},
	}}
	act, size, err := ws.OnBar(context.Background(), ingest.Bar{Ts: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Close: 400}, state)
	if err != nil {
		t.Fatal(err)
	}
	if act != backtest.ActionHold || size != 0 {
		t.Fatalf("OnBar() = %v, %v; want HOLD, 0", act, size)
	}
	if ws.LastSignal.Action != wheel.ActionHold || ws.LastSignal.Direction != wheel.DirectionHold || ws.LastSignal.Quote != nil {
		t.Fatalf("LastSignal = %+v; want HOLD without quote", ws.LastSignal)
	}
	if !strings.Contains(ws.LastSignal.Reason, "snapshot") {
		t.Fatalf("LastSignal.Reason = %q; want snapshot block", ws.LastSignal.Reason)
	}
}
