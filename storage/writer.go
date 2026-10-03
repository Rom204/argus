package storage

import (
	"context"
	"log"
	"time"

	"github.com/Rom204/argus/event"
)

const (
	// defaultBatchSize and defaultInterval bound how long an event waits in
	// memory: a busy host fills a batch quickly, an idle one hits the interval.
	// 100ms keeps kernel→DB latency well under the 500ms budget (CLAUDE.md §10).
	defaultBatchSize = 500
	defaultInterval  = 100 * time.Millisecond

	// queueSize is how many rows can wait between the ring buffer reader and
	// the flush loop. When it fills, Add blocks, the reader stops draining the
	// ring buffer, and the kernel starts dropping events — back-pressure lands
	// on telemetry rather than on the host being observed.
	queueSize = 8192

	// closeTimeout bounds the final flush so shutdown cannot hang on a dead DB.
	closeTimeout = 2 * time.Second
)

// FlushFunc writes one batch of rows, each in columns order.
type FlushFunc func(ctx context.Context, rows [][]any) error

// Writer batches events on their way to the database, so the ring buffer
// reader never waits on a network round trip per event.
//
// A batch that fails to flush is reported and dropped, not retried. That is a
// deliberate v1 limit: retrying needs bounded buffering and a policy for a DB
// that stays down, which is more machinery than this project needs yet.
type Writer struct {
	flush     FlushFunc
	clock     event.Clock
	batchSize int
	interval  time.Duration
	logf      func(format string, args ...any)

	in   chan []any
	done chan struct{}
}

// NewWriter returns a Writer that hands batches to flush, converting kernel
// timestamps to wall-clock time with clock.
func NewWriter(flush FlushFunc, clock event.Clock) *Writer {
	return newWriter(flush, clock, defaultBatchSize, defaultInterval)
}

func newWriter(flush FlushFunc, clock event.Clock, batchSize int, interval time.Duration) *Writer {
	return &Writer{
		flush:     flush,
		clock:     clock,
		batchSize: batchSize,
		interval:  interval,
		logf:      log.Printf,
		in:        make(chan []any, queueSize),
		done:      make(chan struct{}),
	}
}

// Start launches the background loop that flushes batches.
func (w *Writer) Start() {
	go w.run()
}

// Add queues one event for writing. It must not be called after Close.
func (w *Writer) Add(ev event.ProcessEvent) {
	w.in <- toRow(ev, w.clock.WallTime(ev.TimestampNS))
}

// Close stops accepting events, flushes whatever is still queued, and waits
// for that to finish.
func (w *Writer) Close() {
	close(w.in)
	<-w.done
}

func (w *Writer) run() {
	defer close(w.done)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	batch := make([][]any, 0, w.batchSize)
	send := func(ctx context.Context) {
		if len(batch) == 0 {
			return
		}
		if err := w.flush(ctx, batch); err != nil {
			w.logf("storage: dropped %d events, write failed: %v", len(batch), err)
		}
		// A fresh slice, not batch[:0]: the flush target may still hold the
		// old one.
		batch = make([][]any, 0, w.batchSize)
	}

	for {
		select {
		case row, ok := <-w.in:
			if !ok {
				ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
				send(ctx)
				cancel()
				return
			}
			batch = append(batch, row)
			if len(batch) == w.batchSize {
				send(context.Background())
			}
		case <-ticker.C:
			send(context.Background())
		}
	}
}
