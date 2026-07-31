package backtest

// State is a backtest's portfolio state; Run updates Price to each bar's close before OnBar.
type State struct {
	Cash     float64
	Position float64
	Price    float64
}

// Equity returns total portfolio value: cash plus position at the given price.
func (s *State) Equity(price float64) float64 {
	return s.Cash + s.Position*price
}
