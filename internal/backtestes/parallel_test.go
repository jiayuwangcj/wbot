package backtestes

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelMapPreservesOrderAndBoundsWorkers(t *testing.T) {
	const taskCount = 24
	var active, maximum int32
	tasks := make([]func(context.Context) (int, error), taskCount)
	for i := range tasks {
		index := i
		tasks[i] = func(context.Context) (int, error) {
			current := atomic.AddInt32(&active, 1)
			for {
				old := atomic.LoadInt32(&maximum)
				if current <= old || atomic.CompareAndSwapInt32(&maximum, old, current) {
					break
				}
			}
			time.Sleep(time.Duration(taskCount-index) * time.Microsecond)
			atomic.AddInt32(&active, -1)
			return index, nil
		}
	}
	got, err := ParallelMap(context.Background(), tasks)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range got {
		if value != i {
			t.Fatalf("result[%d] = %d; want input order", i, value)
		}
	}
	if maximum > EvaluationWorkers {
		t.Fatalf("maximum concurrent workers = %d; want <= %d", maximum, EvaluationWorkers)
	}
	if maximum < 2 {
		t.Fatalf("maximum concurrent workers = %d; want parallel evaluation", maximum)
	}
}
