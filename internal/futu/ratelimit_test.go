package futu

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLimiterSpacing(t *testing.T) {
	gap := 30 * time.Millisecond
	l := NewLimiter(gap)
	ctx := context.Background()

	var mu sync.Mutex
	starts := make([]time.Time, 0, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Wait(ctx); err != nil {
				t.Errorf("Wait() error: %v", err)
				return
			}
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(starts) != 8 {
		t.Fatalf("got %d starts; want 8", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if d := starts[i].Sub(starts[i-1]); d < gap-2*time.Millisecond {
			t.Fatalf("start %d only %v after previous; want >= %v", i, d, gap)
		}
	}
}

// TestDefaultTiers locks the conservative official-tier gaps (review P1): any
// loosening must be deliberate and re-documented in doc/FUTU.md §8.
func TestDefaultTiers(t *testing.T) {
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"quote total cap", QuoteLimit.gap, 50 * time.Millisecond},
		{"kline total cap", KlineLimit.gap, 200 * time.Millisecond},
		{"kline first page", HistoryPageLimit.gap, 3 * time.Second},
		{"snapshot", SnapshotLimit.gap, 3 * time.Second},
		{"kline batch gap", BatchGap, time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s gap = %v; want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestLimiterContextCancel(t *testing.T) {
	l := NewLimiter(10 * time.Millisecond)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx); err != context.Canceled {
		t.Fatalf("Wait(cancelled) = %v; want context.Canceled", err)
	}
}
