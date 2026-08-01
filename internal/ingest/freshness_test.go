package ingest

import (
	"context"
	"testing"
	"time"
)

func TestJudgeFreshness(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		maxTs  time.Time
		maxAge time.Duration
		want   FreshnessStatus
	}{
		{"younger than threshold", now.Add(-2 * time.Hour), 72 * time.Hour, Fresh},
		{"at threshold is fresh", now.Add(-72 * time.Hour), 72 * time.Hour, Fresh},
		{"past threshold", now.Add(-73 * time.Hour), 72 * time.Hour, Stale},
		{"future ts is fresh", now.Add(time.Hour), 72 * time.Hour, Fresh},
		{"zero threshold with future ts", now.Add(time.Hour), 0, Fresh},
		{"no data", time.Time{}, 72 * time.Hour, Unknown},
		{"no data any threshold", time.Time{}, 0, Unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JudgeFreshness(tt.maxTs, now, tt.maxAge); got != tt.want {
				t.Fatalf("JudgeFreshness(%v, %v, %v) = %q; want %q", tt.maxTs, now, tt.maxAge, got, tt.want)
			}
		})
	}
}

func TestMaxAgeForTimeframe(t *testing.T) {
	tests := []struct {
		tf   string
		want time.Duration
	}{
		{"1m", 10 * time.Minute},  // 3×1m below the 10m floor
		{"5m", 15 * time.Minute},  // 3×5m
		{"15m", 45 * time.Minute}, // 3×15m
		{"60m", 3 * time.Hour},    // 3×60m
		{"1h", 3 * time.Hour},     // 3×1h
		{"1d", 72 * time.Hour},    // 3×1d (draft: 1d → 3 days)
		{"1w", 21 * 24 * time.Hour},
		{"1mo", 90 * 24 * time.Hour},
		{"", 24 * time.Hour}, // unparseable → 24h fallback
		{"bogus", 24 * time.Hour},
		{"1x", 24 * time.Hour},
		{"0m", 24 * time.Hour}, // zero interval is not a valid timeframe
	}
	for _, tt := range tests {
		t.Run(tt.tf, func(t *testing.T) {
			if got := MaxAgeForTimeframe(tt.tf); got != tt.want {
				t.Fatalf("MaxAgeForTimeframe(%q) = %v; want %v", tt.tf, got, tt.want)
			}
		})
	}
}

func TestQueryFreshness_validation(t *testing.T) {
	ctx := context.Background()
	if _, err := QueryFreshness(ctx, nil, time.Now()); err == nil {
		t.Fatal("expected error for nil db")
	}
}
