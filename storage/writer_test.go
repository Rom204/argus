package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Rom204/argus/event"
)

// recorder is an in-memory flush target: every batch the Writer flushes is
// sent on batches, so a test can wait for it.
type recorder struct {
	batches chan [][]any
}

func newRecorder() *recorder {
	return &recorder{batches: make(chan [][]any, 16)}
}

func (r *recorder) flush(_ context.Context, rows [][]any) error {
	r.batches <- rows
	return nil
}

// next waits for one flushed batch, failing the test if none arrives.
func (r *recorder) next(t *testing.T) [][]any {
	t.Helper()
	select {
	case b := <-r.batches:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a flush")
		return nil
	}
}

func execEvent(pid uint32) event.ProcessEvent {
	return event.ProcessEvent{Version: event.Version, Type: event.TypeExecve, PID: pid, Comm: "true"}
}

func TestWriter_FlushError_IsReportedAndBatchIsNotRetried(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	failed := false
	flush := func(ctx context.Context, rows [][]any) error {
		if !failed {
			failed = true
			return errors.New("connection refused")
		}
		return rec.flush(ctx, rows)
	}

	reports := make(chan string, 4)
	w := newWriter(flush, event.Clock{}, 1, time.Hour)
	w.logf = func(format string, args ...any) { reports <- fmt.Sprintf(format, args...) }
	w.Start()
	defer w.Close()

	w.Add(execEvent(1)) // this batch fails
	w.Add(execEvent(2))

	batch := rec.next(t)
	if len(batch) != 1 {
		t.Fatalf("second flush had %d rows, want 1 (failed rows must not be retried)", len(batch))
	}
	if pid := batch[0][3]; pid != int32(2) {
		t.Errorf("second flush carried pid %v, want 2", pid)
	}

	select {
	case msg := <-reports:
		if !strings.Contains(msg, "connection refused") {
			t.Errorf("report %q does not mention the cause", msg)
		}
	default:
		t.Error("flush error was not reported")
	}
}

func TestWriter_Close_FlushesRemainingRows(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	w := newWriter(rec.flush, event.Clock{}, 500, time.Hour)
	w.Start()

	w.Add(execEvent(1))
	w.Add(execEvent(2))
	w.Close()

	if got := len(rec.next(t)); got != 2 {
		t.Errorf("flushed %d rows on close, want 2", got)
	}
}

func TestWriter_PartialBatch_FlushesWhenIntervalElapses(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	w := newWriter(rec.flush, event.Clock{}, 500, 20*time.Millisecond)
	w.Start()
	defer w.Close()

	w.Add(execEvent(1))

	if got := len(rec.next(t)); got != 1 {
		t.Errorf("flushed %d rows, want 1", got)
	}
}

func TestWriter_FullBatch_FlushesWithoutWaitingForInterval(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	w := newWriter(rec.flush, event.Clock{}, 3, time.Hour)
	w.Start()
	defer w.Close()

	for pid := uint32(1); pid <= 3; pid++ {
		w.Add(execEvent(pid))
	}

	if got := len(rec.next(t)); got != 3 {
		t.Errorf("flushed %d rows, want 3", got)
	}
}
