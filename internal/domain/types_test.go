package domain

import "testing"

func TestSymbolValid(t *testing.T) {
	tests := []struct {
		symbol Symbol
		want   bool
	}{
		{"AAPL.US", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.symbol.Valid(); got != tt.want {
			t.Errorf("Valid(%q) = %v; want %v", tt.symbol, got, tt.want)
		}
	}
}

func TestOrderIDSpecified(t *testing.T) {
	tests := []struct {
		id   OrderID
		want bool
	}{
		{"ord-1", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.id.Specified(); got != tt.want {
			t.Errorf("Specified(%q) = %v; want %v", tt.id, got, tt.want)
		}
	}
}

func TestOrderTerminal(t *testing.T) {
	tests := []struct {
		name string
		o    Order
		want bool
	}{
		{"new", Order{Status: OrderNew}, false},
		{"working", Order{Status: OrderWorking}, false},
		{"filled", Order{Status: OrderFilled}, true},
		{"canceled", Order{Status: OrderCanceled}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.o.Terminal(); got != tt.want {
				t.Fatalf("Terminal() = %v; want %v", got, tt.want)
			}
		})
	}
}
