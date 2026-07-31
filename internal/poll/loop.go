package poll

import (
	"context"
	"fmt"
	"time"

	"github.com/jiayu/wbot/internal/agent"
	"github.com/jiayu/wbot/internal/master"
)

// Heartbeat performs one poll cycle: register this agent with the master.
func Heartbeat(a agent.Facade, m master.Facade) bool {
	return m.Register(a.Identity())
}

// Run repeats Heartbeat on interval until ctx is done (first run immediate); returns ctx.Err().
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
