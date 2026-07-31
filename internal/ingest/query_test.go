package ingest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestQueryBars_validation(t *testing.T) {
	ctx := context.Background()
	from := time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	if _, err := QueryBars(ctx, nil, "X.US", "1d", time.Time{}, time.Time{}, 10); err == nil {
		t.Fatal("expected error for nil db")
	}
	if _, err := QueryBars(ctx, stubDB(), "", "1d", time.Time{}, time.Time{}, 10); err == nil {
		t.Fatal("expected error for empty symbol")
	}
	if _, err := QueryBars(ctx, stubDB(), "X.US", "", time.Time{}, time.Time{}, 10); err == nil {
		t.Fatal("expected error for empty timeframe")
	}
	if _, err := QueryBars(ctx, stubDB(), "X.US", "1d", from, to, 10); err == nil {
		t.Fatal("expected error for from after to")
	} else if !strings.Contains(err.Error(), "from after to") {
		t.Fatalf("err = %q; want message mentioning 'from after to'", err)
	}
	for _, limit := range []int{0, -1} {
		if _, err := QueryBars(ctx, stubDB(), "X.US", "1d", time.Time{}, time.Time{}, limit); err == nil {
			t.Fatalf("QueryBars(..., %d): expected error for limit <= 0", limit)
		}
	}
}
