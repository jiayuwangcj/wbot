package backtestexec

// Unit tests for the shared CLI/API run contract: Build validation and
// SaveParams shape. Run's DB paths are covered by the httpapi integration
// tests (real PostgreSQL, WBOT_PG_DSN).

import (
	"context"
	"strings"
	"testing"

	"github.com/jiayu/wbot/internal/backtest"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		name      string
		strategy  string
		params    map[string]any
		wantTempl bool
		wantErr   string
	}{
		{"hold", "hold", nil, false, ""},
		{"buy-hold", "buy-hold", map[string]any{}, false, ""},
		{"hold rejects params", "hold", map[string]any{"a": 1}, false, "no params"},
		{"buy-hold rejects params", "buy-hold", map[string]any{"a": 1}, false, "no params"},
		{"covered-call", "covered-call", nil, true, ""},
		{"covered-call defaults", "covered-call", map[string]any{"strike_pct_otm": 0.05}, true, ""},
		{"cash-secured-put", "cash-secured-put", map[string]any{"cash_reserve": 1.2}, true, ""},
		{"unknown strategy", "nope", nil, false, "unknown template"},
		{"unknown param", "covered-call", map[string]any{"bogus": 1}, false, "unknown param"},
		{"wrong type", "covered-call", map[string]any{"strike_pct_otm": "0.03"}, false, "want a number"},
		{"out of range", "covered-call", map[string]any{"strike_pct_otm": -1}, false, "want in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, templ, err := Build(tt.strategy, tt.params)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Build(%q) err = %v; want containing %q", tt.strategy, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Build(%q) err = %v; want nil", tt.strategy, err)
			}
			if s == nil {
				t.Fatalf("Build(%q) strategy = nil; want a strategy", tt.strategy)
			}
			if (templ != nil) != tt.wantTempl {
				t.Fatalf("Build(%q) templ = %v; want templ present = %v", tt.strategy, templ, tt.wantTempl)
			}
			if tt.strategy == "hold" || tt.strategy == "buy-hold" {
				if _, ok := s.(backtest.HoldStrategy); !ok {
					if _, ok := s.(*backtest.BuyHoldStrategy); !ok {
						t.Fatalf("Build(%q) strategy type = %T; want hold/buy-hold", tt.strategy, s)
					}
				}
			}
		})
	}
}

func TestSaveParams(t *testing.T) {
	got := SaveParams(Options{Cash: 10000, Fee: 1.5, Timeframe: "1d", Adjust: "fwd"})
	want := map[string]any{"cash": 10000.0, "fee": 1.5, "timeframe": "1d", "adjust": "fwd"}
	if len(got) != len(want) {
		t.Fatalf("SaveParams = %v; want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("SaveParams[%q] = %v; want %v", k, got[k], v)
		}
	}
}

func TestRunRejectsNilDB(t *testing.T) {
	if _, err := Run(context.Background(), nil, Options{Symbol: "DEMO.US", Strategy: "hold"}); err == nil {
		t.Fatal("Run(nil db) err = nil; want error")
	}
}

func TestRunRejectsMissingInputs(t *testing.T) {
	for _, o := range []Options{
		{Symbol: "", Strategy: "hold"},
		{Symbol: "DEMO.US", Strategy: ""},
	} {
		if _, err := Run(context.Background(), nil, o); err == nil {
			t.Fatalf("Run(%+v) err = nil; want error", o)
		}
	}
}
