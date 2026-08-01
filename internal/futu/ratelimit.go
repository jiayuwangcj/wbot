package futu

import (
	"context"
	"sync"
	"time"
)

// Limiter gates request starts to at most one per gap; package-level instances
// below form a global pool shared by every Client and command (incl. -every).
type Limiter struct {
	mu   sync.Mutex
	gap  time.Duration
	next time.Time
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
		if !now.Before(l.next) {
			l.next = now.Add(l.gap)
			l.mu.Unlock()
			return nil
		}
		wait := l.next.Sub(now)
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Shared rate pools (老板指令 2026-08-01: 严格控制拉取频率). Basis: 富途官方
// 文档 3103 历史K线 第1页 10 次/30s、快照 3203 一级 30/二级 20/三级 10 次/30s；
// 快照取官方下限档 10 次/30s 为保守值。限频池仅进程内共享——shell 循环反复
// 启动 wbot 会绕过；跨进程聚合排期待后续（见 doc/FUTU.md §8）。
var (
	// QuoteLimit is the 20 req/s total cap shared by every futu request.
	QuoteLimit = NewLimiter(50 * time.Millisecond)
	// KlineLimit is the 5 req/s total cap additionally applied to K-line requests.
	KlineLimit = NewLimiter(200 * time.Millisecond)
	// HistoryPageLimit gates /api/history-kline first pages (official 10 per 30s).
	HistoryPageLimit = NewLimiter(3 * time.Second)
	// SnapshotLimit gates /api/quote snapshots at the official lower tier
	// (10 per 30s = 1 per 3s; scripts hitting this faster risk market rights).
	SnapshotLimit = NewLimiter(3 * time.Second)
	// BatchGap is the forced pause between K-line pages (coordination ≥1s/批).
	BatchGap = time.Second
)
