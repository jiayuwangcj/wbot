package backtest

// State is the portfolio state of a running backtest. Price is updated by the
// Runner to the current bar's close before each strategy call.
type State struct {
	Cash     float64
	Position float64
	Price    float64
}

// Equity returns the total portfolio value: cash plus the position priced at
// the given price.
func (s *State) Equity(price float64) float64 {
	return s.Cash + s.Position*price
}
