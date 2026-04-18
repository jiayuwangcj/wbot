package ingest

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunEvery_onceWhenNoInterval(t *testing.T) {
	ctx := context.Background()
	var n int32
	err := RunEvery(ctx, 0, func(context.Context) error {
		atomic.AddInt32(&n, 1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("calls: %d want 1", n)
	}
}

func TestRunEvery_repeatsUntilCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Millisecond)
	defer cancel()
	var n int32
	err := RunEvery(ctx, 10*time.Millisecond, func(context.Context) error {
		atomic.AddInt32(&n, 1)
		return nil
	})
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v want DeadlineExceeded", err)
	}
	if n < 3 {
		t.Fatalf("calls: %d want at least 3", n)
	}
}
