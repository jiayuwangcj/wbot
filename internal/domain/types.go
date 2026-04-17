package domain

// Symbol identifies an instrument (e.g. "AAPL.US").
type Symbol string

// Valid reports whether s is non-empty.
func (s Symbol) Valid() bool {
	return s != ""
}

// Side is order direction.
type Side uint8

const (
	SideBuy Side = iota + 1
	SideSell
)

// OrderID is an optional broker-assigned identifier; empty means unspecified.
type OrderID string

// Specified reports whether id is set.
func (id OrderID) Specified() bool {
	return id != ""
}

// OrderStatus is the lifecycle state of an order.
type OrderStatus uint8

const (
	OrderNew OrderStatus = iota + 1
	OrderWorking
	OrderFilled
	OrderCanceled
)

// Order is a minimal snapshot (no sizing in this slice).
type Order struct {
	Symbol Symbol
	Side   Side
	ID     OrderID
	Status OrderStatus
}

// Terminal reports whether the order is done for good.
func (o Order) Terminal() bool {
	return o.Status == OrderFilled || o.Status == OrderCanceled
}
