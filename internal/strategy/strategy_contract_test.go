package strategy

import (
	"slices"
	"strings"
	"testing"
)

// TestContractTemplates pins the /v1/strategies JSON contract rendered from
// the registry: engine string params with an allowed set become "choice"
// with Choices, Help becomes Description, and the registry stays free of
// engine first-class strategies (buy-hold appended by httpapi).
func TestContractTemplates(t *testing.T) {
	got := ContractTemplates()
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2 (covered-call, cash-secured-put)", len(got))
	}
	for i, name := range []string{"covered-call", "cash-secured-put"} {
		if got[i].Name != name {
			t.Fatalf("templates[%d].name = %q; want %q", i, got[i].Name, name)
		}
	}
	for _, tmpl := range got {
		params := map[string]ContractParam{}
		for _, p := range tmpl.Params {
			params[p.Name] = p
		}
		if tmpl.Name == "cash-secured-put" {
			if d := params["cash_reserve"].Default; d != 1.0 {
				t.Fatalf("cash-secured-put cash_reserve default = %v; want 1.0", d)
			}
		} else if _, ok := params["cash_reserve"]; ok {
			t.Fatalf("covered-call must not expose cash_reserve, got %v", params)
		}
		if p := params["expiry_rule"]; p.Type != "choice" ||
			!slices.Equal(p.Choices, []string{"next_expiry", "days"}) {
			t.Fatalf("expiry_rule = %+v; want choice with next_expiry+days", p)
		}
		for _, p := range []string{"strike_pct_otm", "days_to_expiry", "fee_per_contract", "lot_size"} {
			if params[p].Type != "number" {
				t.Fatalf("%s type = %q; want number", p, params[p].Type)
			}
		}
		if params["strike_pct_otm"].Default != 0.03 ||
			params["days_to_expiry"].Default != 28.0 ||
			params["fee_per_contract"].Default != 0.0 ||
			params["lot_size"].Default != 100.0 {
			t.Fatalf("covered-call defaults drift: %+v", params)
		}
		if params["strike_pct_otm"].Description == "" || params["fee_per_contract"].Description == "" {
			t.Fatalf("param descriptions must render from engine Help")
		}
	}
}

func TestValidate(t *testing.T) {
	valid := []map[string]any{
		nil,
		{},
		{"strike_pct_otm": 0.03},
		{"expiry_rule": "next_expiry", "days_to_expiry": 30.0, "fee_per_contract": 0.5},
		{"expiry_rule": "days", "lot_size": 200.0},
	}
	for _, c := range valid {
		if err := Validate("covered-call", c); err != nil {
			t.Fatalf("Validate(covered-call, %v) = %v; want nil", c, err)
		}
	}
	if err := Validate("cash-secured-put", map[string]any{"cash_reserve": 2.0}); err != nil {
		t.Fatalf("Validate(cash-secured-put, cash_reserve) = %v; want nil", err)
	}
	rejects := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"nope", nil, "unknown template"},
		{"covered-call", map[string]any{"nope": 1}, "unknown param"},
		{"covered-call", map[string]any{"strike_pct_otm": "0.03"}, "want a number"},
		{"covered-call", map[string]any{"expiry_rule": "monthly"}, "want one of"},
		{"covered-call", map[string]any{"expiry_rule": 28.0}, "want one of"},
		{"covered-call", map[string]any{"strike_pct_otm": true}, "want a number"},
	}
	for _, c := range rejects {
		err := Validate(c.name, c.params)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("Validate(%q, %v) = %v; want contains %q", c.name, c.params, err, c.want)
		}
	}
}
