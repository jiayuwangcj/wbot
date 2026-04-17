package poll

import (
	"context"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/agent"
	"github.com/jiayu/wbot/internal/master"
)

// Heartbeat performs one logical poll cycle: register this agent with the master.
// A future HTTPS client will call the same step on a timer.
func Heartbeat(a agent.Facade, m master.Facade) bool {
	return m.Register(a.Identity())
}

// Run repeats Heartbeat on interval until ctx is done. The first heartbeat runs
// immediately; subsequent ones run after each tick. Returns ctx.Err(), or a
// non-nil error if interval is not positive.
func Run(ctx context.Context, interval time.Duration, a agent.Facade, m master.Facade) error {
	if interval <= 0 {
		return fmt.Errorf("poll: interval must be positive")
	}
	Heartbeat(a, m)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			Heartbeat(a, m)
		}
	}
}
