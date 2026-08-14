package wheelrun

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/jiayu/wbot/internal/wheelstore"
)

const defaultSnapshotQueueSize = 64

// QuoteSnapshotRecorder is intentionally narrower than SignalRepository.
// Snapshot persistence is optional and asynchronous; wheel_signals remains on
// the synchronous SignalRepository path because review and push depend on its
// returned ID.
type QuoteSnapshotRecorder interface {
	AppendQuoteSnapshot(context.Context, wheelstore.QuoteSnapshotRecord) (int64, error)
}

type snapshotBatch struct {
	underlying string
	records    []wheelstore.QuoteSnapshotRecord
}

// asyncSnapshotRecorder turns quote persistence into a bounded, best-effort
// side channel. enqueue never waits for the database; a full queue drops one
// batch and emits an operational diagnostic.
type asyncSnapshotRecorder struct {
	recorder QuoteSnapshotRecorder
	queue    chan snapshotBatch
	done     chan struct{}

	mu     sync.Mutex
	closed bool
}

func newAsyncSnapshotRecorder(recorder QuoteSnapshotRecorder, size int) *asyncSnapshotRecorder {
	if recorder == nil {
		return nil
	}
	if size <= 0 {
		size = defaultSnapshotQueueSize
	}
	w := &asyncSnapshotRecorder{
		recorder: recorder,
		queue:    make(chan snapshotBatch, size),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *asyncSnapshotRecorder) run() {
	defer close(w.done)
	for batch := range w.queue {
		for _, record := range batch.records {
			if _, err := w.recorder.AppendQuoteSnapshot(context.Background(), record); err != nil {
				fmt.Fprintf(os.Stderr, "wheelrun: async quote snapshot %s: %v\n", batch.underlying, err)
			}
		}
	}
}

func (w *asyncSnapshotRecorder) enqueue(batch snapshotBatch) {
	if w == nil || len(batch.records) == 0 {
		return
	}
	// Holding the mutex across the non-blocking send makes close and enqueue
	// race-free without ever making the evaluation goroutine wait for storage.
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.queue <- batch:
	default:
		fmt.Fprintf(os.Stderr, "wheelrun: async quote snapshot queue full; dropped %d records for %s\n", len(batch.records), batch.underlying)
	}
}

func (w *asyncSnapshotRecorder) close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	<-w.done
}
