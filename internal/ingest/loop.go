package ingest

import (
	"context"
	"time"
)

// RunEvery invokes fn once when interval <= 0. Otherwise it runs fn, then waits
// interval, repeating until ctx is cancelled. The first invocation is immediate
// (no initial sleep).
func RunEvery(ctx context.Context, interval time.Duration, fn func(context.Context) error) error {
	if interval <= 0 {
		return fn(ctx)
	}
	for {
		if err := fn(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
