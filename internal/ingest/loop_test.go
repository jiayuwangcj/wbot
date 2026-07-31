package ingest

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

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

func TestRunEveryResilient_recoversAfterFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var attempts, got int32
	err := RunEveryResilient(ctx, time.Millisecond, func(context.Context) error {
		if atomic.AddInt32(&attempts, 1) <= 3 {
			return errBoom
		}
		return nil
	}, func(e error) {
		if e != errBoom {
			t.Errorf("onErr err = %v want %v", e, errBoom)
		}
		atomic.AddInt32(&got, 1)
	})
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v want DeadlineExceeded", err)
	}
	if got != 3 {
		t.Fatalf("onErr calls: %d want 3", got)
	}
	if attempts < 4 {
		t.Fatalf("attempts: %d want at least 4 (loop must continue after failures)", attempts)
	}
}

func TestRunEveryResilient_keepsGoingOnPersistentFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var attempts, got int32
	err := RunEveryResilient(ctx, time.Millisecond, func(context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return errBoom
	}, func(e error) {
		if e != errBoom {
			t.Errorf("onErr err = %v want %v", e, errBoom)
		}
		atomic.AddInt32(&got, 1)
	})
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v want DeadlineExceeded", err)
	}
	if got < 3 {
		t.Fatalf("onErr calls: %d want at least 3", got)
	}
	if got != attempts {
		t.Fatalf("onErr calls: %d, attempts: %d; every failure must reach onErr", got, attempts)
	}
}

func TestRunEveryResilient_singleFailure(t *testing.T) {
	err := RunEveryResilient(context.Background(), 0, func(context.Context) error {
		return errBoom
	}, func(error) {
		t.Fatal("onErr called in one-shot mode")
	})
	if err != errBoom {
		t.Fatalf("err = %v want %v", err, errBoom)
	}
}

func TestRunEveryResilient_singleSuccess(t *testing.T) {
	err := RunEveryResilient(context.Background(), 0, func(context.Context) error {
		return nil
	}, func(error) {
		t.Fatal("onErr called in one-shot mode")
	})
	if err != nil {
		t.Fatalf("err = %v want nil", err)
	}
}
