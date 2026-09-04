// Command argus is the Argus EDR agent: it loads the eBPF process-lifecycle
// probes and streams what they observe.
//
// Requires root — loading BPF programs is a privileged operation.
package main

import (
	_ "embed"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Rom204/argus/event"
	"github.com/Rom204/argus/sensor"
)

// The compiled probes are embedded so the agent is a single self-contained
// binary that runs from any directory (and, at M3, from a systemd unit).
//
// This means the BPF object must be built before `go build`, which is the order
// documented in CLAUDE.md §9 and used by ci.yml.
//
//go:embed bpf/sensor.bpf.o
var bpfObject []byte

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatalf("argus: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	clock, err := event.NewClock()
	if err != nil {
		return err
	}

	s, err := sensor.New(bpfObject)
	if err != nil {
		return err
	}
	defer s.Close()

	fmt.Fprintln(os.Stderr, "argus: probes attached, watching process lifecycle (Ctrl+C to stop)")

	return s.Run(ctx, func(ev event.ProcessEvent) {
		fmt.Printf("%s %s\n", clock.WallTime(ev.TimestampNS).Format("15:04:05.000"), ev)
	})
}
