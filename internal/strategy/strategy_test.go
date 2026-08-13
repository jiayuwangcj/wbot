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
		"price_position_curve": []any{
			map[string]any{"price": 400.0, "target_inventory": 1200.0},
			map[string]any{"price": 480.0, "target_inventory": 600.0},
			map[string]any{"price": 550.0, "target_inventory": 0.0},
		},
		"max_inventory": 1200.0,
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
	for _, template := range templates {
		for _, p := range template.Params {
			if p.Type != "curve" && p.Type != "number" && p.Type != "choice" {
				t.Fatalf("%s has unsupported schema type %q", p.Name, p.Type)
			}
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
	for _, name := range []string{"price_position_curve", "max_inventory"} {
		p, ok := byName[name]
		if !ok || !p.Required {
			t.Fatalf("%s = %+v; want required", name, p)
		}
	}
	if byName["price_position_curve"].Type != "curve" || byName["max_inventory"].Type != "number" {
		t.Fatalf("critical schema = %+v %+v", byName["price_position_curve"], byName["max_inventory"])
	}
	defaults := map[string]any{
		"min_dte": 5.0, "max_dte": 10.0,
		"min_option_quality": 0.6, "max_daily_orders": 1.0,
		"extreme_max_daily_orders": 2.0, "no_trade_gap": 50.0,
		"strategic_state": wheel.StateNormal,
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
	for _, name := range []string{"price_position_curve", "max_inventory"} {
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
	if cfg.MinDTE != 5 || cfg.MaxDTE != 10 || cfg.MinOptionQuality != 0.6 ||
		cfg.MaxDailyOrders != 1 || cfg.ExtremeMaxDailyOrders != 2 || cfg.NoTradeGap != 50 || cfg.StrategicState != wheel.StateNormal {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestParseConfigValidationAndRoundTrip(t *testing.T) {
	params := validParams()
	params["strategic_state"] = wheel.StateCaution
	params["lot_size"] = 200 // legacy key must be ignored (2026-08-13)
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
	if parsed.MaxInventory != cfg.MaxInventory || parsed.StrategicState != cfg.StrategicState {
		t.Fatalf("round trip = %+v; want %+v", parsed, cfg)
	}

	bad := validParams()
	bad["price_position_curve"] = []any{
		map[string]any{"price": 480.0, "target_inventory": 600.0},
		map[string]any{"price": 400.0, "target_inventory": 1200.0},
	}
	if _, err := ParseConfig(bad); err == nil || !strings.Contains(err.Error(), "curve") {
		t.Fatalf("invalid curve error = %v", err)
	}
	bad = validParams()
	bad["max_inventory"] = "1200"
	if _, err := ParseConfig(bad); err == nil || !strings.Contains(err.Error(), "number") {
		t.Fatalf("invalid max_inventory error = %v", err)
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
