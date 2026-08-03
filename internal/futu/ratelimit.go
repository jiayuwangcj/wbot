package futu

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Limiter gates request starts to at most one per gap; package-level instances
// below form a global pool shared by every Client and command (incl. -every).
// With persistPath set, the timing is additionally shared across processes via
// a flock-guarded timestamp file (see persistedLimiter).
type Limiter struct {
	mu          sync.Mutex
	gap         time.Duration
	next        time.Time
	persistPath string
}

// NewLimiter returns a limiter that allows one request per gap.
func NewLimiter(gap time.Duration) *Limiter {
	return &Limiter{gap: gap}
}

// Wait blocks until the next allowed slot, or returns ctx.Err() when cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		next := l.next
		if l.persistPath != "" {
			if w := l.crossProcessNext(now); w.After(next) {
				next = w
			}
		}
		if !now.Before(next) {
			l.next = now.Add(l.gap)
			l.mu.Unlock()
			return nil
		}
		wait := next.Sub(now)
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// crossProcessNext extends the in-memory window with the shared on-disk
// rhythm: the read-decision-mark runs in one flock session so concurrent
// processes cannot both pass on a stale last-request stamp. Returns the next
// allowed slot (zero = no extra cross-process wait); when the request may
// proceed it also records now. Unreadable/unwritable state degrades to the
// pure in-memory limiter (never fails the request).
func (l *Limiter) crossProcessNext(now time.Time) time.Time {
	f, err := os.OpenFile(l.persistPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return time.Time{}
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	var last time.Time
	if b, err := io.ReadAll(f); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && n > 0 {
			last = time.Unix(0, n)
		}
	}
	window := last.Add(l.gap)
	if now.Before(window) {
		return window
	}
	// Record this request as the new last stamp (truncate first: the file may
	// hold a longer prior value).
	if err := f.Truncate(0); err == nil {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			_, _ = fmt.Fprintf(f, "%d", now.UnixNano())
		}
	}
	return time.Time{}
}

// persistedLimiter builds a package-level limiter whose rhythm is shared
// across processes when FUTU_RATELIMIT_DIR is set (one timestamp file per
// tier under that directory); unset → pure in-memory (current behavior).
func persistedLimiter(name string, gap time.Duration) *Limiter {
	dir := os.Getenv("FUTU_RATELIMIT_DIR")
	if dir == "" {
		return NewLimiter(gap)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return NewLimiter(gap)
	}
	l := NewLimiter(gap)
	l.persistPath = filepath.Join(dir, name+".ts")
	return l
}

// Shared rate pools (老板指令 2026-08-01: 严格控制拉取频率). Basis: 富途官方
// 文档 3103 历史K线 第1页 10 次/30s、快照 3203 一级 30/二级 20/三级 10 次/30s；
// 快照取官方下限档 10 次/30s 为保守值。限频池进程内共享;设置
// FUTU_RATELIMIT_DIR 后跨进程共享(flock 时间戳文件,见 persistedLimiter;
// shell 循环反复启动 wbot / 多进程并发的场景应设置,doc/FUTU.md §8)。
var (
	// QuoteLimit is the 20 req/s total cap shared by every futu request.
	QuoteLimit = persistedLimiter("quote", 50*time.Millisecond)
	// KlineLimit is the 5 req/s total cap additionally applied to K-line requests.
	KlineLimit = persistedLimiter("kline", 200*time.Millisecond)
	// HistoryPageLimit gates /api/history-kline first pages (official 10 per 30s).
	HistoryPageLimit = persistedLimiter("history-page", 3*time.Second)
	// SnapshotLimit gates /api/quote snapshots at the official lower tier
	// (10 per 30s = 1 per 3s; scripts hitting this faster risk market rights).
	SnapshotLimit = persistedLimiter("snapshot", 3*time.Second)
	// BatchGap is the forced pause between K-line pages (coordination ≥1s/批).
	BatchGap = time.Second
)
