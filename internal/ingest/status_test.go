package ingest

import (
	"context"
	"testing"
)

func TestRecentRuns_validation(t *testing.T) {
	ctx := context.Background()
	for _, limit := range []int{0, -1} {
		if _, err := RecentRuns(ctx, nil, limit); err == nil {
			t.Fatalf("RecentRuns(ctx, nil, %d): expected error for limit <= 0", limit)
		}
	}
}
