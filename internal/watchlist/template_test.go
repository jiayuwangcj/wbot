package watchlist

import (
	"strings"
	"testing"
)

func validWheelParams() map[string]any {
	return map[string]any{
		"full_position_price": 400.0,
		"zero_position_price": 550.0,
		"max_inventory":       1200.0,
	}
}

func TestValidateWheel(t *testing.T) {
	if err := Validate("wheel", validWheelParams()); err != nil {
		t.Fatalf("Validate(wheel) = %v", err)
	}
	for _, name := range []string{"covered-call", "cash-secured-put", "buy-hold"} {
		if err := Validate(name, validWheelParams()); err == nil || !strings.Contains(err.Error(), "unknown template") {
			t.Fatalf("Validate(%q) = %v; want unknown template", name, err)
		}
	}
}

func TestValidateWheelRejectsMissingOrInvalidParams(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"missing full price", map[string]any{"zero_position_price": 550.0, "max_inventory": 1200.0}, "full_position_price"},
		{"missing zero price", map[string]any{"full_position_price": 400.0, "max_inventory": 1200.0}, "zero_position_price"},
		{"missing max inventory", map[string]any{"full_position_price": 400.0, "zero_position_price": 550.0}, "max_inventory"},
		{"unknown param", map[string]any{"full_position_price": 400.0, "zero_position_price": 550.0, "max_inventory": 1200.0, "nope": 1}, "unknown param"},
		{"bad anchors", map[string]any{"full_position_price": 400.0, "zero_position_price": 300.0, "max_inventory": 1200.0}, "zero_position_price"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate("wheel", tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate(wheel, %v) = %v; want contains %q", tc.params, err, tc.want)
			}
		})
	}
}
