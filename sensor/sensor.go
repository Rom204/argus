// Package sensor loads Argus's eBPF programs, attaches them, and streams the
// events they produce.
//
// It knows nothing about what the events mean or where they end up — decoding
// lives in package event, and the caller decides what to do with each one.
package sensor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"github.com/Rom204/argus/event"
)

// ringbufMapName must match the map declared in bpf/sensor.bpf.c.
const ringbufMapName = "events"

// kthreaddPID is the PID of kthreadd, the parent of every kernel thread.
// Kernel threads are not user-space processes and are out of scope (CLAUDE.md
// §4.2), so they are filtered here in user space rather than in the probes —
// §6.2 keeps producers dumb and puts filtering wherever it is cheapest to
// express correctly.
const kthreaddPID = 2

// Sensor owns the loaded BPF objects and the ring buffer reader.
type Sensor struct {
	coll   *ebpf.Collection
	links  []link.Link
	reader *ringbuf.Reader
}

// New loads the compiled BPF object and attaches every program in it.
//
// Attachment is driven entirely by each program's SEC() name, so adding a hook
// in the C source needs no change here (CLAUDE.md §6.2: the read loop is
// written once).
func New(object []byte) (*Sensor, error) {
	// Older kernels cap locked memory for BPF maps. Harmless on 5.11+, which
	// charges maps to the cgroup instead.
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("raise memlock rlimit: %w", err)
	}

	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(object))
	if err != nil {
		return nil, fmt.Errorf("parse BPF object: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		// The verifier's rejection reason is in here and is the single most
		// useful thing to read when a probe fails to load.
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			return nil, fmt.Errorf("load BPF programs, verifier said:\n%+v", ve)
		}
		return nil, fmt.Errorf("load BPF programs: %w", err)
	}

	s := &Sensor{coll: coll}

	for name, prog := range coll.Programs {
		group, tp, err := tracepointTarget(spec.Programs[name].AttachTo)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("program %q: %w", name, err)
		}

		l, err := link.Tracepoint(group, tp, prog, nil)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("attach %q to %s/%s: %w", name, group, tp, err)
		}
		s.links = append(s.links, l)
	}

	rd, err := ringbuf.NewReader(coll.Maps[ringbufMapName])
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("open ring buffer %q: %w", ringbufMapName, err)
	}
	s.reader = rd

	return s, nil
}

// tracepointTarget splits an AttachTo value such as "sched/sched_process_exec"
// into its tracepoint group and name.
func tracepointTarget(attachTo string) (group, name string, err error) {
	group, name, found := strings.Cut(attachTo, "/")
	if !found || group == "" || name == "" {
		return "", "", fmt.Errorf("cannot parse tracepoint target %q, want group/name", attachTo)
	}
	return group, name, nil
}

// Run reads events until ctx is cancelled, passing each decoded event to handle.
//
// Records that fail to decode are logged and skipped rather than fatal: a
// single malformed record should not take the agent down, but it must never
// pass silently — a size or version mismatch means the kernel and this binary
// were built from different versions of bpf/event.h.
func (s *Sensor) Run(ctx context.Context, handle func(event.ProcessEvent)) error {
	// Closing the reader is what unblocks Read below.
	go func() {
		<-ctx.Done()
		s.reader.Close()
	}()

	identities := newIdentityTracker()

	for {
		record, err := s.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read ring buffer: %w", err)
		}

		ev, err := event.Unmarshal(record.RawSample)
		if err != nil {
			log.Printf("skipping malformed event: %v", err)
			continue
		}

		if ev.PID == kthreaddPID || ev.PPID == kthreaddPID {
			continue
		}

		// Drop set*id calls that re-assert credentials the process already
		// held — see identityTracker.
		if !identities.observe(ev) {
			continue
		}

		handle(ev)
	}
}

// Close detaches every program and releases the loaded objects.
func (s *Sensor) Close() error {
	var errs []error

	if s.reader != nil {
		if err := s.reader.Close(); err != nil && !errors.Is(err, ringbuf.ErrClosed) {
			errs = append(errs, err)
		}
	}
	for _, l := range s.links {
		if err := l.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.coll != nil {
		s.coll.Close()
	}

	return errors.Join(errs...)
}
