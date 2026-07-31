package backtest

import (
	"strings"
	"testing"
)

func TestCheckMaxDrawdown(t *testing.T) {
	tests := []struct {
		name    string
		res     *Result
		limit   float64
		wantErr string // empty means no error
	}{
		{"below limit", &Result{MaxDrawdown: 0.5}, 0.9, ""},
		{"equal to limit", &Result{MaxDrawdown: 0.5}, 0.5, ""},
		{"exceeds limit", &Result{MaxDrawdown: 0.5}, 0.2, "0.5"},
		{"exceeds limit message has limit", &Result{MaxDrawdown: 0.5}, 0.2, "0.2"},
		{"nil result", nil, 0.5, "nil result"},
		{"zero limit", &Result{}, 0, "invalid max drawdown limit 0"},
		{"limit above 1", &Result{}, 1.5, "invalid max drawdown limit 1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckMaxDrawdown(tt.res, tt.limit)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckMaxDrawdown(%+v, %v) = %v; want nil", tt.res, tt.limit, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("CheckMaxDrawdown(%+v, %v) error = %v; want containing %q", tt.res, tt.limit, err, tt.wantErr)
			}
		})
	}
}
