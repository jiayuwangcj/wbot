package paper

import (
	"errors"
	"testing"

	"github.com/jiayu/wbot/internal/domain"
)

func TestSubmitInvalidSymbol(t *testing.T) {
	e := NewEngine()
	_, err := e.Submit(domain.Order{Symbol: "", Side: domain.SideBuy})
	if !errors.Is(err, ErrInvalidSymbol) {
		t.Fatalf("Submit = %v; want %v", err, ErrInvalidSymbol)
	}
}

func TestSubmitFills(t *testing.T) {
	e := NewEngine()
	got, err := e.Submit(domain.Order{Symbol: "TEST.US", Side: domain.SideSell})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got.Status != domain.OrderFilled {
		t.Fatalf("Status = %v; want OrderFilled", got.Status)
	}
	if !got.ID.Specified() {
		t.Fatal("expected generated OrderID")
	}
	if got.Symbol != "TEST.US" || got.Side != domain.SideSell {
		t.Fatalf("side/symbol mismatch: %+v", got)
	}
}

func TestSubmitIDsIncrease(t *testing.T) {
	e := NewEngine()
	a, err := e.Submit(domain.Order{Symbol: "A.US", Side: domain.SideBuy})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Submit(domain.Order{Symbol: "B.US", Side: domain.SideBuy})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("expected distinct OrderIDs")
	}
}
