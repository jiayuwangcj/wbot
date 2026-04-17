package paper

import (
	"errors"
	"fmt"
	"sync"

	"github.com/jiayu/wbot/internal/domain"
)

// ErrInvalidSymbol is returned when Submit receives an empty symbol.
var ErrInvalidSymbol = errors.New("paper: invalid symbol")

// Engine is a minimal simulated executor: accepts orders and marks them filled in-process.
type Engine struct {
	mu  sync.Mutex
	seq uint64
}

// NewEngine returns an empty paper engine.
func NewEngine() *Engine {
	return &Engine{}
}

// Submit ingests an order intent and returns a filled snapshot with a generated OrderID.
// Invalid symbols are rejected with ErrInvalidSymbol.
func (e *Engine) Submit(o domain.Order) (domain.Order, error) {
	if !o.Symbol.Valid() {
		return domain.Order{}, ErrInvalidSymbol
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	id := domain.OrderID(fmt.Sprintf("paper-%d", e.seq))
	return domain.Order{
		Symbol: o.Symbol,
		Side:   o.Side,
		ID:     id,
		Status: domain.OrderFilled,
	}, nil
}
