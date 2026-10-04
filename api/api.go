// Package api serves the stored events over HTTP as JSON, and serves the page
// that reads them.
//
// It is the read side of Argus and runs as its own process: the agent needs
// root to load eBPF programs, this does not. Nothing here knows that the
// events came from the kernel.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/Rom204/argus/storage"
)

// EventStore is the one thing the handlers need from the database.
//
// The handlers depend on this interface rather than on *storage.DB so their
// tests can pass an in-memory fake and run with no database and no Docker. In
// Go the interface is declared here, by the consumer, and *storage.DB satisfies
// it without naming it.
type EventStore interface {
	RecentEvents(ctx context.Context, query storage.EventQuery) ([]storage.EventRow, error)
}

// Server holds what the handlers need: somewhere to read events from, and the
// page to serve at /.
type Server struct {
	store EventStore
	ui    http.Handler
}

// NewServer returns a Server reading from store. ui may be nil, which is what
// the tests do — then only the JSON routes exist.
func NewServer(store EventStore, ui http.Handler) *Server {
	return &Server{store: store, ui: ui}
}

// Routes returns the handler for the whole API.
//
// The patterns carry the method ("GET /api/events"), so the router answers 405
// for anything else by itself and no handler needs to re-check.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", s.handleEvents)
	if s.ui != nil {
		mux.Handle("GET /", s.ui)
	}
	return mux
}

// eventJSON is one event as the wire sees it.
//
// A separate type from storage.EventRow on purpose: this is the published
// shape, and the json tags are the API's contract with the page. Renaming a Go
// field should not silently rename a JSON key.
type eventJSON struct {
	Time string `json:"time"`
	Type string `json:"type"`
	PID  int32  `json:"pid"`
	PPID int32  `json:"ppid"`
	UID  int64  `json:"uid"`
	GID  int64  `json:"gid"`
	Comm string `json:"comm"`
	// Capabilities are a bitmask, so they travel as hex: "0x2000" says
	// "bit 13, CAP_NET_RAW", where 8192 says nothing.
	Caps string `json:"caps"`
}

// eventsResponse wraps the array in an object rather than returning a bare
// array, so a field like "total" or "next" can be added later without
// breaking every client.
type eventsResponse struct {
	Count  int         `json:"count"`
	Events []eventJSON `json:"events"`
}

// timeFormat is RFC 3339 with milliseconds — precise enough to order events
// that are microseconds apart, short enough to read in a table.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	query, err := parseEventQuery(r.URL.Query())
	if err != nil {
		// The parse errors describe a broken rule, so they are safe to return.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rows, err := s.store.RecentEvents(r.Context(), query)
	if err != nil {
		// Log the real reason, tell the client nothing: a database error can
		// name schemas and columns.
		log.Printf("api: reading events failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read events"})
		return
	}

	events := make([]eventJSON, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventJSON{
			Time: row.Time.UTC().Format(timeFormat),
			Type: row.Type,
			PID:  row.PID,
			PPID: row.PPID,
			UID:  row.UID,
			GID:  row.GID,
			Comm: row.Comm,
			Caps: fmt.Sprintf("%#x", row.Caps),
		})
	}

	writeJSON(w, http.StatusOK, eventsResponse{Count: len(events), Events: events})
}

// writeJSON sends one JSON response.
//
// The header is set before the status, and the status before the body —
// net/http sends the headers with the first write, so anything set afterwards
// is silently dropped.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so this cannot become a 500 — all
		// that is left is to record it.
		log.Printf("api: writing response failed: %v", err)
	}
}
