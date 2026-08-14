package ingest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestQueryBarCoverage_validation(t *testing.T) {
	ctx := context.Background()
	if _, err := QueryBarCoverage(ctx, nil); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestQueryBars_validation(t *testing.T) {
	ctx := context.Background()
	from := time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	if _, err := QueryBars(ctx, nil, "X.US", "1d", "none", time.Time{}, time.Time{}, 10, false); err == nil {
		t.Fatal("expected error for nil db")
	}
	if _, err := QueryBars(ctx, stubDB(), "", "1d", "none", time.Time{}, time.Time{}, 10, false); err == nil {
		t.Fatal("expected error for empty symbol")
	}
	if _, err := QueryBars(ctx, stubDB(), "X.US", "", "none", time.Time{}, time.Time{}, 10, false); err == nil {
		t.Fatal("expected error for empty timeframe")
	}
	if _, err := QueryBars(ctx, stubDB(), "X.US", "1d", "none", from, to, 10, false); err == nil {
		t.Fatal("expected error for from after to")
	} else if !strings.Contains(err.Error(), "from after to") {
		t.Fatalf("err = %q; want message mentioning 'from after to'", err)
	}
	for _, limit := range []int{0, -1} {
		if _, err := QueryBars(ctx, stubDB(), "X.US", "1d", "none", time.Time{}, time.Time{}, limit, false); err == nil {
			t.Fatalf("QueryBars(..., %d): expected error for limit <= 0", limit)
		}
	}
}

func TestProviderAdjustment(t *testing.T) {
	tests := []struct {
		source, adjust, want string
	}{
		{"tencent", "fwd", "qfq"},
		{"TENCENT", "back", "hfq"},
		{"tencent", "none", "none"},
		{"futu", "fwd", "fwd"},
		{"file", "none", "none"},
	}
	for _, tt := range tests {
		if got := providerAdjustment(tt.source, tt.adjust); got != tt.want {
			t.Errorf("providerAdjustment(%q, %q) = %q; want %q", tt.source, tt.adjust, got, tt.want)
		}
	}
}
