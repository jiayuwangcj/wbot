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
// 文档 3103 历史K线 第1页 10 次/30s、快照 20-30 次/30s；此处取更保守值。
var (
	// QuoteLimit is 20 req/s for every futu request.
	QuoteLimit = NewLimiter(50 * time.Millisecond)
	// KlineLimit is 5 req/s additionally applied to K-line requests.
	KlineLimit = NewLimiter(200 * time.Millisecond)
	// HistoryPageLimit gates /api/history-kline first pages (official 10 per 30s).
	HistoryPageLimit = NewLimiter(3 * time.Second)
	// BatchGap is the forced pause between K-line pages (coordination ≥1s/批).
	BatchGap = time.Second
)
