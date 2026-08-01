package watchlist

import (
	"strings"
	"testing"
)

// TestTemplatesContract pins the ⑫-b draft contract: swap to the
// internal/strategy registry (feat/strategy-impl) must keep these green.
func TestTemplatesContract(t *testing.T) {
	tmpls := Templates()
	if len(tmpls) != 2 {
		t.Fatalf("len = %d; want 2", len(tmpls))
	}
	for i, name := range []string{"covered-call", "cash-secured-put"} {
		if tmpls[i].Name != name {
			t.Fatalf("templates[%d].name = %q; want %q", i, tmpls[i].Name, name)
		}
	}
	for _, tmpl := range tmpls {
		params := map[string]Param{}
		for _, p := range tmpl.Params {
			params[p.Name] = p
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
		if params["expiry_rule"].Default != "next_expiry" {
			t.Fatalf("%s expiry_rule default = %v; want next_expiry", tmpl.Name, params["expiry_rule"].Default)
		}
	}
}

func TestValidateValidParams(t *testing.T) {
	cases := []map[string]any{
		nil,
		{},
		{"strike_pct_otm": 0.03},
		{"strike_pct_otm": 0.03, "days_to_expiry": 30.0, "fee_per_contract": 0.5, "expiry_rule": "next_expiry"},
	}
	for _, c := range cases {
		for _, name := range []string{"covered-call", "cash-secured-put"} {
			if err := Validate(name, c); err != nil {
				t.Fatalf("Validate(%q, %v) = %v; want nil", name, c, err)
			}
		}
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
