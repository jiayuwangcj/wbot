package backtest

import "fmt"

// CheckMaxDrawdown errors when res.MaxDrawdown exceeds limit; limit must be in (0, 1].
func CheckMaxDrawdown(res *Result, limit float64) error {
	if limit <= 0 || limit > 1 {
		return fmt.Errorf("backtest: invalid max drawdown limit %v", limit)
	}
	if res == nil {
		return fmt.Errorf("backtest: nil result")
	}
	if res.MaxDrawdown > limit {
		return fmt.Errorf("backtest: max drawdown %v exceeds limit %v", res.MaxDrawdown, limit)
	}
	return nil
}
