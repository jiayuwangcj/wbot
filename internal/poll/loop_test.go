package poll

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/jiayu/wbot/internal/agent"
	"github.com/jiayu/wbot/internal/master"
)

func TestHeartbeatRegisters(t *testing.T) {
	m := master.NewMemory()
	a := agent.Stub{ID: "poll-1"}
	if !Heartbeat(a, m) {
		t.Fatal("expected first register to succeed")
	}
	if Heartbeat(a, m) {
		t.Fatal("duplicate register should report not new")
	}
	got := m.Agents()
	sort.Strings(got)
	if len(got) != 1 || got[0] != "poll-1" {
		t.Fatalf("Agents() = %v; want [poll-1]", got)
	}
}

func TestRunPollsUntilCancel(t *testing.T) {
	m := master.NewMemory()
	a := agent.Stub{ID: "loop-1"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, 15*time.Millisecond, a, m)
	}()

	time.Sleep(45 * time.Millisecond)
	cancel()

	if err := <-errCh; err != context.Canceled {
		t.Fatalf("Run returned %v; want %v", err, context.Canceled)
	}
	got := m.Agents()
	sort.Strings(got)
	if len(got) != 1 || got[0] != "loop-1" {
		t.Fatalf("Agents() = %v; want [loop-1]", got)
	}
}

func TestRunInvalidInterval(t *testing.T) {
	m := master.NewMemory()
	a := agent.Stub{ID: "x"}
	ctx := context.Background()
	err := Run(ctx, 0, a, m)
	if err == nil {
		t.Fatal("expected error for interval <= 0")
	}
}

func TestRunReturnsCanceled(t *testing.T) {
	m := master.NewMemory()
	a := agent.Stub{ID: "y"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, time.Millisecond, a, m)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v; want %v", err, context.Canceled)
	}
}
