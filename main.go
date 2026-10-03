// Command argus is the Argus EDR agent: it loads the eBPF process-lifecycle
// probes, prints what they observe, and persists every event to the local
// PostgreSQL + TimescaleDB store.
//
// Requires root — loading BPF programs is a privileged operation — and a
// running database (`docker compose up -d`).
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
	"github.com/Rom204/argus/storage"
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

// defaultDBURL matches docker-compose.yml. It is a default rather than a
// required setting because `sudo` strips the environment, and a plain
// `sudo ./argus` should just work.
const defaultDBURL = "postgres://argus:argus@127.0.0.1:5432/argus?sslmode=disable"

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	clock, err := event.NewClock()
	if err != nil {
		return err
	}

	dbURL := os.Getenv("ARGUS_DB_URL")
	if dbURL == "" {
		dbURL = defaultDBURL
	}
	db, err := storage.Open(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("%w (is \"docker compose up -d\" running?)", err)
	}
	defer db.Close()

	writer := storage.NewWriter(db.Insert, clock)
	writer.Start()
	// Deferred after db.Close, so it runs first: the last batch is flushed
	// while the pool is still open.
	defer writer.Close()

	s, err := sensor.New(bpfObject)
	if err != nil {
		return err
	}
	defer s.Close()

	fmt.Fprintln(os.Stderr, "argus: probes attached, writing events to the database (Ctrl+C to stop)")

	return s.Run(ctx, func(ev event.ProcessEvent) {
		fmt.Printf("%s %s\n", clock.WallTime(ev.TimestampNS).Format("15:04:05.000"), ev)
		writer.Add(ev)
	})
}
