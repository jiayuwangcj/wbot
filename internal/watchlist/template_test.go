package watchlist

import (
	"slices"
	"strings"
	"testing"
)

// TestTemplatesContract pins the ⑫-b draft contract and mirrors the
// internal/strategy engine registry (Templates/Factory, doc/BACKTEST.md):
// swap to the registry (unification task) must keep these green.
func TestTemplatesContract(t *testing.T) {
	tmpls := Templates()
	if len(tmpls) != 3 {
		t.Fatalf("len = %d; want 3", len(tmpls))
	}
	for i, name := range []string{"buy-hold", "covered-call", "cash-secured-put"} {
		if tmpls[i].Name != name {
			t.Fatalf("templates[%d].name = %q; want %q", i, tmpls[i].Name, name)
		}
	}
	for _, tmpl := range tmpls {
		params := map[string]Param{}
		for _, p := range tmpl.Params {
			params[p.Name] = p
		}
		if tmpl.Name == "buy-hold" {
			if len(params) != 0 {
				t.Fatalf("buy-hold must have no params, got %v", params)
			}
			continue
		}
		if d := params["strike_pct_otm"].Default; d != 0.03 {
			t.Fatalf("%s strike_pct_otm default = %v; want 0.03", tmpl.Name, d)
		}
		if d := params["days_to_expiry"].Default; d != 28.0 {
			t.Fatalf("%s days_to_expiry default = %v; want 28", tmpl.Name, d)
		}
		if d := params["fee_per_contract"].Default; d != 0.0 {
			t.Fatalf("%s fee_per_contract default = %v; want 0", tmpl.Name, d)
		}
		if d := params["lot_size"].Default; d != 100.0 {
			t.Fatalf("%s lot_size default = %v; want 100", tmpl.Name, d)
		}
		if p := params["expiry_rule"]; p.Default != "next_expiry" {
			t.Fatalf("%s expiry_rule default = %v; want next_expiry", tmpl.Name, p.Default)
		} else if !slices.Contains(p.Choices, "next_expiry") || !slices.Contains(p.Choices, "days") {
			t.Fatalf("%s expiry_rule choices = %v; want next_expiry and days (engine registry parity)", tmpl.Name, p.Choices)
		}
		if tmpl.Name == "cash-secured-put" {
			if d := params["cash_reserve"].Default; d != 1.0 {
				t.Fatalf("cash-secured-put cash_reserve default = %v; want 1.0", d)
			}
		} else if _, ok := params["cash_reserve"]; ok {
			t.Fatalf("covered-call must not expose cash_reserve, got %v", params)
		}
	}
}

func TestValidateValidParams(t *testing.T) {
	cases := []map[string]any{
		nil,
		{},
		{"strike_pct_otm": 0.03},
		{"strike_pct_otm": 0.03, "days_to_expiry": 30.0, "fee_per_contract": 0.5, "expiry_rule": "next_expiry"},
		{"expiry_rule": "days", "days_to_expiry": 30.0, "lot_size": 200.0},
	}
	for _, c := range cases {
		for _, name := range []string{"covered-call", "cash-secured-put"} {
			if err := Validate(name, c); err != nil {
				t.Fatalf("Validate(%q, %v) = %v; want nil", name, c, err)
			}
		}
	}
	// cash_reserve is cash-secured-put only.
	if err := Validate("cash-secured-put", map[string]any{"cash_reserve": 2.0, "lot_size": 100.0}); err != nil {
		t.Fatalf("Validate(cash-secured-put, cash_reserve) = %v; want nil", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"nope", nil, "unknown strategy template"},
		{"covered-call", map[string]any{"nope": 1}, "unknown parameter"},
		{"covered-call", map[string]any{"strike_pct_otm": "0.03"}, "want number"},
		{"covered-call", map[string]any{"days_to_expiry": "28"}, "want number"},
		{"covered-call", map[string]any{"expiry_rule": "monthly"}, "want one of"},
		{"covered-call", map[string]any{"expiry_rule": 28.0}, "want one of"},
		{"covered-call", map[string]any{"strike_pct_otm": true}, "want number"},
	}
	for _, c := range cases {
		err := Validate(c.name, c.params)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("Validate(%q, %v) = %v; want contains %q", c.name, c.params, err, c.want)
		}
	}
}
