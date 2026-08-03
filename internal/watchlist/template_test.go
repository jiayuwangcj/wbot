package watchlist

import (
	"strings"
	"testing"
)

// TestValidateValidParams covers the write-surface contract: registered
// strategy params pass through to the internal/strategy registry (single
// source of truth, doc/BACKTEST.md); buy-hold accepts no params.
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
	if err := Validate("buy-hold", nil); err != nil {
		t.Fatalf("Validate(buy-hold, nil) = %v; want nil", err)
	}
	if err := Validate("buy-hold", map[string]any{}); err != nil {
		t.Fatalf("Validate(buy-hold, {}) = %v; want nil", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"nope", nil, "unknown template"},
		{"covered-call", map[string]any{"nope": 1}, "unknown param"},
		{"covered-call", map[string]any{"strike_pct_otm": "0.03"}, "want a number"},
		{"covered-call", map[string]any{"days_to_expiry": "28"}, "want a number"},
		{"covered-call", map[string]any{"expiry_rule": "monthly"}, "want one of"},
		{"covered-call", map[string]any{"expiry_rule": 28.0}, "want one of"},
		{"covered-call", map[string]any{"strike_pct_otm": true}, "want a number"},
		{"buy-hold", map[string]any{"nope": 1}, "unknown parameter"},
	}
	for _, c := range cases {
		err := Validate(c.name, c.params)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("Validate(%q, %v) = %v; want contains %q", c.name, c.params, err, c.want)
		}
	}
}
