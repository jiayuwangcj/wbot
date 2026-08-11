package main

import (
	"context"
	"fmt"

	"github.com/jiayu/wbot/internal/futu"
)

func applyWheelDefaults(ctx context.Context, symbol string, params map[string]any) error {
	_, hasCurve := params["price_position_curve"]
	if !hasCurve {
		price, err := (futuQuoter{client: futu.NewClient(resolveFutuGateway(""))}).Quote(ctx, symbol)
		if err != nil {
			return fmt.Errorf("current price for defaults: %w", err)
		}
		if price <= 0 {
			return fmt.Errorf("current price for defaults: %v is not positive", price)
		}
		params["price_position_curve"] = []map[string]any{
			{"price": price * 0.8, "target_inventory": 100},
			{"price": price * 1.2, "target_inventory": 0},
		}
	}
	if _, ok := params["max_inventory"]; !ok {
		params["max_inventory"] = 100
	}
	return nil
}
