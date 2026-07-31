package backtest

import (
	"context"

	"github.com/jiayu/wbot/internal/ingest"
)

// Action is a strategy's decision for one bar.
type Action int

const (
	ActionHold Action = iota
	ActionBuy
	ActionSell
)

// Strategy decides one bar's trade; OnBar runs serially before the runner settles at the close.
type Strategy interface {
	OnBar(ctx context.Context, bar ingest.Bar, st *State) (Action, float64, error)
}

// HoldStrategy never trades.
type HoldStrategy struct{}

// OnBar always holds.
func (HoldStrategy) OnBar(_ context.Context, _ ingest.Bar, _ *State) (Action, float64, error) {
	return ActionHold, 0, nil
}

// BuyHoldStrategy buys all-in at the first bar's close, then holds; not reusable between runs.
type BuyHoldStrategy struct {
	bought bool
}

// OnBar buys all available cash at the first bar's close, then holds.
func (s *BuyHoldStrategy) OnBar(_ context.Context, bar ingest.Bar, st *State) (Action, float64, error) {
	if s.bought {
		return ActionHold, 0, nil
	}
	s.bought = true
	return ActionBuy, st.Cash / bar.Close, nil
}
