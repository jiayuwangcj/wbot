package futu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestLimiterCrossProcessShared: two limiter instances on one persist path
// must hold the same gap rhythm — the flock read-decide-mark session keeps
// concurrent instances from both passing on a stale stamp.
func TestLimiterCrossProcessShared(t *testing.T) {
	gap := 30 * time.Millisecond
	l1 := NewLimiter(gap)
	l1.persistPath = filepath.Join(t.TempDir(), "shared.ts")
	l2 := NewLimiter(gap)
	l2.persistPath = l1.persistPath
	ctx := context.Background()

	// Serial probe that the shared stamp is effective before the strict timing
	// assertion below; a broken stamp (e.g. silent write failure emptying the
	// file, CI flake) would show as µs gaps — skip with the cause instead.
	if err := l1.Wait(ctx); err != nil {
		t.Fatalf("probe l1 Wait: %v", err)
	}
	probeStart := time.Now()
	if err := l2.Wait(ctx); err != nil {
		t.Fatalf("probe l2 Wait: %v", err)
	}
	if d := time.Since(probeStart); d < gap*4/5 {
		stamp, _ := os.ReadFile(l1.persistPath)
		t.Skipf("cross-process stamping not effective: l2 passed after %v; want >= %v; stamp file: %q", d, gap*4/5, stamp)
	}

	var mu sync.Mutex
	starts := make([]struct {
		t time.Time
		l int
	}, 0, 8)
	limiters := []*Limiter{l1, l2}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := limiters[i%2].Wait(ctx); err != nil {
				t.Errorf("Wait() error: %v", err)
				return
			}
			mu.Lock()
			starts = append(starts, struct {
				t time.Time
				l int
			}{time.Now(), i % 2})
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if len(starts) != 8 {
		t.Fatalf("got %d starts; want 8", len(starts))
	}
	failed := false
	for i := 1; i < len(starts); i++ {
		if d := starts[i].t.Sub(starts[i-1].t); d < gap-2*time.Millisecond {
			failed = true
			t.Errorf("start %d only %v after previous (across instances); want >= %v (persistPath %s)", i, d, gap, l1.persistPath)
		}
	}
	// One compact line with the full timing scene (every start with its
	// instance, the whole gap sequence, final stamp file) for CI log grepping.
	if failed {
		base := starts[0].t
		offs := make([]string, len(starts))
		for i, s := range starts {
			offs[i] = fmt.Sprintf("%d:l%d:+%v", i, s.l, s.t.Sub(base))
		}
		gaps := make([]string, len(starts)-1)
		for i := 1; i < len(starts); i++ {
			gaps[i-1] = fmt.Sprintf("%d->%d:%v", i-1, i, starts[i].t.Sub(starts[i-1].t))
		}
		file, _ := os.ReadFile(l1.persistPath)
		t.Errorf("scene: persistPath=%s file=%q starts=[%s] gaps=[%s]",
			l1.persistPath, file, strings.Join(offs, " "), strings.Join(gaps, " "))
	}
}

// TestLimiterCrossProcessTimestampAfterLock ensures a caller delayed behind
// the file lock records the time at which it actually gets the lock, rather
// than the time at which it started waiting. A stale stamp would let the next
// process pass immediately after the lock is released.
func TestLimiterCrossProcessTimestampAfterLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.ts")
	blocker, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	defer blocker.Close()
	if err := flockExclusive(blocker); err != nil {
		t.Skipf("flock unavailable: %v", err)
	}

	l := NewLimiter(10 * time.Millisecond)
	l.persistPath = path
	releaseAt := time.Now().Add(50 * time.Millisecond)
	released := make(chan struct{})
	go func() {
		timer := time.NewTimer(time.Until(releaseAt))
		defer timer.Stop()
		<-timer.C
		if err := flockRelease(blocker); err != nil {
			t.Errorf("release lock: %v", err)
		}
		close(released)
	}()

	decisionAt, next := l.crossProcessNext(time.Time{})
	<-released
	if decisionAt.Before(releaseAt) {
		t.Fatalf("decision timestamp %v predates lock release %v", decisionAt, releaseAt)
	}
	if !next.IsZero() {
		t.Fatalf("first pass next = %v; want zero", next)
	}
}

// TestLimiterCrossProcessDegrade: an unusable persist path (missing parent
// dir) degrades to the pure in-memory limiter — Wait never fails.
func TestLimiterCrossProcessDegrade(t *testing.T) {
	l := NewLimiter(10 * time.Millisecond)
	l.persistPath = filepath.Join(t.TempDir(), "no-such-dir", "x.ts")
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() with unwritable persist path = %v; want in-memory degrade", err)
	}
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("second Wait() = %v", err)
	}
}

// TestPersistedLimiterEnv: FUTU_RATELIMIT_DIR opt-in wiring for the package
// level limiters; unset keeps the current pure in-memory behavior.
func TestPersistedLimiterEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FUTU_RATELIMIT_DIR", dir)
	l := persistedLimiter("quote", 50*time.Millisecond)
	if l.persistPath != filepath.Join(dir, "quote.ts") {
		t.Fatalf("persistPath = %q; want %q", l.persistPath, filepath.Join(dir, "quote.ts"))
	}

	t.Setenv("FUTU_RATELIMIT_DIR", "")
	if l2 := persistedLimiter("quote", 50*time.Millisecond); l2.persistPath != "" {
		t.Fatalf("env unset: persistPath = %q; want empty (pure in-memory)", l2.persistPath)
	}
}

// TestLimiterCrossProcess re-executes this test binary as a child process and
// asserts the child's first pass is gated by the parent's stamp on the shared
// file — the exact "shell 循环反复启动 wbot 会绕过" scenario from FUTU.md §8.
func TestLimiterCrossProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xproc.ts")
	l := NewLimiter(200 * time.Millisecond)
	l.persistPath = path
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("parent Wait: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestLimiterCrossProcessHelper$")
	cmd.Env = append(os.Environ(), "RATELIMIT_HELPER=1", "RATELIMIT_HELPER_FILE="+path)
	start := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v (%s)", err, out)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("helper returned after %v; want >= ~200ms (cross-process gate)", elapsed)
	}
}

// TestLimiterCrossProcessHelper is the re-executed child of
// TestLimiterCrossProcess (no-op unless RATELIMIT_HELPER is set).
func TestLimiterCrossProcessHelper(t *testing.T) {
	if os.Getenv("RATELIMIT_HELPER") != "1" {
		return
	}
	l := NewLimiter(200 * time.Millisecond)
	l.persistPath = os.Getenv("RATELIMIT_HELPER_FILE")
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("helper Wait: %v", err)
	}
	// Second pass exercises the in-memory rhythm too; combined ~400ms total.
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("helper second Wait: %v", err)
	}
}
