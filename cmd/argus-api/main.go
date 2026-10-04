// Command argus-api serves the stored process events over HTTP, and serves the
// page that displays them.
//
// It is the read half of Argus and deliberately a separate program from the
// agent: the agent needs root to load eBPF programs, this needs only a
// database connection. The process listening on a network port is therefore
// the one with the fewest privileges.
//
// Needs the event store running (`docker compose up -d`). Does NOT need sudo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Rom204/argus/api"
	"github.com/Rom204/argus/storage"
	"github.com/Rom204/argus/web"
)

// defaultDBURL matches docker-compose.yml, and the agent's default in main.go.
const defaultDBURL = "postgres://argus:argus@127.0.0.1:5432/argus?sslmode=disable"

// shutdownTimeout bounds how long in-flight requests get to finish on Ctrl+C.
const shutdownTimeout = 5 * time.Second

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatalf("argus-api: %v", err)
	}
}

func run() error {
	// 0.0.0.0 rather than localhost: the page is opened from the Mac's
	// browser, so the port has to accept connections from off the VM. The
	// database port stays on loopback.
	addr := flag.String("addr", "0.0.0.0:8080", "address to listen on")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("ARGUS_DB_URL")
	if dbURL == "" {
		dbURL = defaultDBURL
	}

	db, err := storage.Open(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("%w (is \"docker compose up -d\" running?)", err)
	}
	defer db.Close()

	server := &http.Server{
		Addr:    *addr,
		Handler: api.NewServer(db, web.Handler()).Routes(),
		// Without this a client can open a connection and never send headers,
		// holding it open indefinitely.
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ListenAndServe blocks, so it runs in its own goroutine and reports back
	// over a channel; the main path waits on either that or a signal.
	serveErr := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "argus-api: listening on http://%s (Ctrl+C to stop)\n", *addr)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// ErrServerClosed is what a deliberate Shutdown produces, not a fault.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen on %s: %w", *addr, err)

	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "argus-api: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
