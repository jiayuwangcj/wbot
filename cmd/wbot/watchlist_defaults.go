package main

import (
	"context"
	"fmt"

	"github.com/jiayu/wbot/internal/futu"
)

func applyWheelDefaults(ctx context.Context, symbol string, params map[string]any) error {
	_, hasFull := params["full_position_price"]
	_, hasZero := params["zero_position_price"]
	_, hasLegacyCurve := params["price_position_curve"]
	if !hasLegacyCurve && (!hasFull || !hasZero) {
		price, err := (futuQuoter{client: futu.NewClient(resolveFutuGateway(""))}).Quote(ctx, symbol)
		if err != nil {
			return fmt.Errorf("current price for defaults: %w", err)
		}
		if price <= 0 {
			return fmt.Errorf("current price for defaults: %v is not positive", price)
		}
		if !hasFull {
			params["full_position_price"] = price * 0.8
		}
		if !hasZero {
			params["zero_position_price"] = price * 1.2
		}
	}
	if _, ok := params["max_inventory"]; !ok {
		params["max_inventory"] = 100
	}
	return nil
}
