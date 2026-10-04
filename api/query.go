package api

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Rom204/argus/storage"
)

const (
	// defaultLimit is what a request with no ?limit= gets: enough rows to fill
	// a screen, small enough to keep the 2-second refresh cheap.
	defaultLimit = 100

	// maxLimit caps how much one request can ask for, so a client cannot make
	// the agent's database do unbounded work.
	maxLimit = 1000
)

// allowedTypes is the set of event type names the contract defines
// (bpf/event.h, enum argus_event_type). An unknown type is rejected rather
// than sanitised: the value reaching SQL is always one of these literals.
var allowedTypes = map[string]bool{
	"EXECVE": true,
	"EXIT":   true,
	"SETUID": true,
	"CAPS":   true,
}

// parseEventQuery validates the ?limit= and ?type= parameters.
//
// It is a plain function over url.Values rather than a method on the handler,
// so every rule here is testable without an HTTP request or a database.
// Returned errors are safe to show a client — they describe the rule that was
// broken and nothing about the server.
func parseEventQuery(values url.Values) (storage.EventQuery, error) {
	query := storage.EventQuery{Limit: defaultLimit}

	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return storage.EventQuery{}, fmt.Errorf("limit must be a whole number, got %q", raw)
		}
		if limit < 1 || limit > maxLimit {
			return storage.EventQuery{}, fmt.Errorf("limit must be between 1 and %d, got %d", maxLimit, limit)
		}
		query.Limit = limit
	}

	if raw := values.Get("type"); raw != "" {
		eventType := strings.ToUpper(raw)
		if !allowedTypes[eventType] {
			return storage.EventQuery{}, fmt.Errorf("unknown event type %q", raw)
		}
		query.Type = eventType
	}

	return query, nil
}
