package storage

import (
	"context"
	"fmt"
	"time"
)

// EventQuery narrows a read of the events table.
//
// Type is the event type name as stored ('EXECVE', 'EXIT', ...); the empty
// string means "every type". Validating these values is the API layer's job —
// this layer only passes them to Postgres as bound parameters.
type EventQuery struct {
	Limit int
	Type  string
}

// EventRow is one events row, in the Go types the API serves.
//
// The integer widths mirror storage/row.go on the way back out: pid/ppid fit
// in int32, while uid/gid are full-range uint32 and cap_effective is a uint64
// bitmask, so both widen to int64.
type EventRow struct {
	Time time.Time
	Type string
	PID  int32
	PPID int32
	UID  int64
	GID  int64
	Comm string
	Caps int64
}

// recentEventsSQL reads the newest events, optionally for one type only.
//
// The values are bound as parameters ($1, $2), never formatted into the
// string: Postgres parses this text once and binds the values separately, so a
// parameter can never be read as SQL. The explicit ::text casts are needed
// because Postgres cannot infer a bare parameter's type from `$1 = ”` alone.
const recentEventsSQL = `
SELECT time, type, pid, ppid, uid, gid, comm, cap_effective
FROM events
WHERE ($1::text = '' OR type = $1::text)
ORDER BY time DESC
LIMIT $2`

// RecentEvents returns the newest events first, matching query.
func (db *DB) RecentEvents(ctx context.Context, query EventQuery) ([]EventRow, error) {
	rows, err := db.pool.Query(ctx, recentEventsSQL, query.Type, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	// Non-nil even when empty, so it marshals to JSON `[]` rather than `null`
	// — the page iterates this array without a nil check.
	events := []EventRow{}
	for rows.Next() {
		var evt EventRow
		err := rows.Scan(&evt.Time, &evt.Type, &evt.PID, &evt.PPID,
			&evt.UID, &evt.GID, &evt.Comm, &evt.Caps)
		if err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		events = append(events, evt)
	}

	// rows.Next() returns false both at the end of the result and on a broken
	// connection; only rows.Err() tells them apart.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return events, nil
}
