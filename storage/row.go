// Package storage persists decoded events to PostgreSQL + TimescaleDB.
//
// The table layout lives in migrations/001_events.sql and mirrors
// struct process_event in bpf/event.h one column per field (CLAUDE.md §6.2).
package storage

import (
	"time"

	"github.com/Rom204/argus/event"
)

// columns is the insert column order. It must match both toRow and the
// events table in migrations/001_events.sql.
var columns = []string{
	"time", "version", "type", "pid", "ppid", "uid", "gid", "cap_effective", "comm",
}

// toRow converts one event into values in columns order.
//
// Postgres has no unsigned integers, so each field goes into the narrowest
// signed type that holds its full range: uid/gid are uint32 and can exceed
// int32, so they widen to int64. cap_effective is uint64 and is cast
// bit-for-bit to int64 — the highest capability bit is ~40, far below the sign
// bit, and the cast is reversible either way.
func toRow(evt event.ProcessEvent, wall time.Time) []any {
	return []any{
		wall,
		int16(evt.Version),
		evt.Type.String(),
		int32(evt.PID),
		int32(evt.PPID),
		int64(evt.UID),
		int64(evt.GID),
		int64(evt.CapEffective),
		evt.Comm,
	}
}
