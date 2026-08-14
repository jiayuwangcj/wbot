package wheelrun

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/wheel"
)

func TestParseOptionCode(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		strike   float64
		expiry   time.Time
		typ      wheel.OptionType
		wantErr  bool
		errMatch string
	}{
		{"call", "TCH260807C335000", 335.0, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), wheel.Call, false, ""},
		{"put", "TCH260807P335000", 335.0, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), wheel.Put, false, ""},
		{"market prefix", "HK.TCH260807C335000", 335.0, time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), wheel.Call, false, ""},
		{"fractional strike", "TCH260828C42500", 42.5, time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), wheel.Call, false, ""},
		{"high strike", "BABA260828P880000", 880.0, time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), wheel.Put, false, ""},
		{"stock code", "00700", 0, time.Time{}, "", true, ""},
		{"bad type letter", "TCH260807X335000", 0, time.Time{}, "", true, ""},
		{"bad month", "TCH261332C335000", 0, time.Time{}, "", true, ""},
		{"missing strike", "TCH260807C", 0, time.Time{}, "", true, ""},
		{"missing expiry", "TCHC335000", 0, time.Time{}, "", true, ""},
		{"empty", "", 0, time.Time{}, "", true, ""},
		{"SH ETF shape", "510050260807C335000", 0, time.Time{}, "", true, "underlying must start with a letter"},
		{"SZ ETF shape", "159915P4250", 0, time.Time{}, "", true, "underlying must start with a letter"},
		{"digit prefix with suffix", "700C335000", 0, time.Time{}, "", true, "underlying must start with a letter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strike, expiry, typ, err := parseOptionCode(tt.code)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseOptionCode(%q) = nil error; want error", tt.code)
				}
				if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("parseOptionCode(%q) error = %q; want containing %q", tt.code, err, tt.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOptionCode(%q) error: %v", tt.code, err)
			}
			if strike != tt.strike || typ != tt.typ || !expiry.Equal(tt.expiry) {
				t.Fatalf("parseOptionCode(%q) = (%v, %v, %v); want (%v, %v, %v)",
					tt.code, strike, expiry, typ, tt.strike, tt.expiry, tt.typ)
			}
		})
	}
}

func TestPositionsInput(t *testing.T) {
	pos := []Position{
		{Symbol: "HK.00700", Code: "00700", Qty: 100, Side: SideLong},
		{Symbol: "HK.00700", Code: "00700", Qty: 50, Side: SideShort},
		{Symbol: "HK.TCH260807C335000", Code: "TCH260807C335000", Qty: 1, Side: SideShort},
		{Symbol: "HK.TCH260807P335000", Code: "TCH260807P335000", Qty: 2, Side: SideLong},
	}
	stock, opts, err := PositionsInput(pos)
	if err != nil {
		t.Fatalf("PositionsInput() error: %v", err)
	}
	if stock != 50 { // 100 long − 50 short
		t.Fatalf("stockShares = %v; want 50", stock)
	}
	if len(opts) != 2 {
		t.Fatalf("opts = %d positions; want 2", len(opts))
	}
	call := opts[0]
	if call.Symbol != "HK.TCH260807C335000" || call.SignedContracts != -1 || call.Strike != 335 || call.OptionType != wheel.Call {
		t.Fatalf("call position = %+v; want short 1 × 335 call", call)
	}
	put := opts[1]
	if put.SignedContracts != 2 || put.OptionType != wheel.Put || put.Strike != 335 {
		t.Fatalf("put position = %+v; want long 2 × 335 put", put)
	}
}

func TestStockAverageCostIgnoresOptionsAndWeightsStock(t *testing.T) {
	positions := []Position{
		{Symbol: "HK.00700", Code: "00700", Qty: 100, Side: SideLong, AvgCost: 500},
		{Symbol: "HK.00700", Code: "00700", Qty: 300, Side: SideLong, AvgCost: 520},
		{Symbol: "HK.TCH260807C650000", Code: "TCH260807C650000", Qty: 1, Side: SideShort, AvgCost: 999},
	}
	if got := StockAverageCost(positions); got != 515 {
		t.Fatalf("StockAverageCost() = %v; want 515", got)
	}
}

func TestFilterPositionsBySymbolAndOptionChain(t *testing.T) {
	positions := []Position{
		{Symbol: "HK.00700", Code: "00700", Qty: 200, Side: SideLong},
		{Symbol: "HK.00883", Code: "00883", Qty: 22000, Side: SideLong},
		{Symbol: "US.JD", Code: "JD", Qty: 100, Side: SideLong},
		{Symbol: "HK.TCH260807C335000", Code: "TCH260807C335000", Qty: 1, Side: SideShort},
		{Symbol: "US.JD260807P335000", Code: "JD260807P335000", Qty: 2, Side: SideLong},
	}

	filtered, skipped := filterPositions("HK.00700", positions, []string{"HK.TCH260807C335000"})
	stock, opts, err := PositionsInput(filtered)
	if err != nil {
		t.Fatalf("PositionsInput(filtered) error: %v", err)
	}
	if stock != 200 || len(opts) != 1 || opts[0].Symbol != "HK.TCH260807C335000" || opts[0].SignedContracts != -1 {
		t.Fatalf("HK.00700 filtered input = (%v, %+v); want 200 shares and its short call", stock, opts)
	}
	if len(skipped) != 1 || skipped[0].Code != "JD260807P335000" {
		t.Fatalf("HK.00700 unassigned options = %+v; want the JD option", skipped)
	}

	filtered, skipped = filterPositions("US.JD", positions, []string{"US.JD260807P335000"})
	stock, opts, err = PositionsInput(filtered)
	if err != nil {
		t.Fatalf("PositionsInput(filtered JD) error: %v", err)
	}
	if stock != 100 || len(opts) != 1 || opts[0].Symbol != "US.JD260807P335000" || opts[0].SignedContracts != 2 {
		t.Fatalf("US.JD filtered input = (%v, %+v); want 100 shares and its long put", stock, opts)
	}
	if len(skipped) != 1 || skipped[0].Code != "TCH260807C335000" {
		t.Fatalf("US.JD unassigned options = %+v; want the Tencent option", skipped)
	}
}

func TestFilterPositionsMatchesBareOptionCode(t *testing.T) {
	positions := []Position{{Code: "TCH260807C335000", Qty: 1, Side: SideShort}}
	filtered, skipped := filterPositions("HK.00700", positions, []string{"HK.TCH260807C335000"})
	if len(filtered) != 1 || len(skipped) != 0 {
		t.Fatalf("bare-code option filter = filtered %+v, skipped %+v; want matched option", filtered, skipped)
	}
}

func TestPositionsInputSymbolFallback(t *testing.T) {
	// Code-only position: symbol falls back to the code for the option key.
	stock, opts, err := PositionsInput([]Position{
		{Code: "TCH260807C335000", Qty: 3, Side: SideShort},
	})
	if err != nil {
		t.Fatalf("PositionsInput() error: %v", err)
	}
	if stock != 0 || len(opts) != 1 || opts[0].Symbol != "TCH260807C335000" || opts[0].SignedContracts != -3 {
		t.Fatalf("PositionsInput(code-only) = (%v, %+v); want option −3", stock, opts)
	}
}

func TestPositionsInputErrors(t *testing.T) {
	if _, _, err := PositionsInput([]Position{{Code: "", Symbol: ""}}); err == nil {
		t.Fatal("PositionsInput(empty code) = nil error; want error")
	}
	if _, _, err := PositionsInput([]Position{{Symbol: "HK.00700", Code: "00700", Qty: 1, Side: 9}}); err == nil {
		t.Fatal("PositionsInput(unknown side) = nil error; want error")
	}
	if _, _, err := PositionsInput([]Position{{Symbol: "HK.00700", Code: "00700", Qty: -5, Side: SideLong}}); err == nil {
		t.Fatal("PositionsInput(negative qty) = nil error; want error")
	}
	if _, _, err := PositionsInput([]Position{{Symbol: "HK.00700", Code: "00700", Qty: 1, Side: SideShort}}); err != nil {
		t.Fatalf("PositionsInput(short stock) error: %v", err)
	}
}

func TestPositionsInputUnsupportedOptionShape(t *testing.T) {
	// a digit-led option-shaped code must fail loudly, never count as stock
	_, _, err := PositionsInput([]Position{
		{Symbol: "SH.510050260807C335000", Code: "510050260807C335000", Qty: 1, Side: SideLong},
	})
	if err == nil || !strings.Contains(err.Error(), "underlying must start with a letter") {
		t.Fatalf("PositionsInput(ETF option shape) error = %v; want unsupported option code", err)
	}
}

// compile-time check that a fake position source satisfies the interface.
var _ TradePositions = fakePositions(nil)

type fakePositions []Position

func (f fakePositions) Positions(ctx context.Context, acc any) ([]Position, error) {
	return f, nil
}
