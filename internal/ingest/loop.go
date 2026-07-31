package ingest

import (
	"context"
	"time"
)

// RunEvery repeats fn every interval until ctx is cancelled; interval <= 0 runs fn once.
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

// RunEveryResilient is RunEvery plus onErr: a failing fn is reported and the loop continues.
func RunEveryResilient(ctx context.Context, interval time.Duration, fn func(context.Context) error, onErr func(error)) error {
	if interval <= 0 {
		return fn(ctx)
	}
	for {
		if err := fn(ctx); err != nil {
			onErr(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
