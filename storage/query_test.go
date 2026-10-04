package storage

import (
	"context"
	"testing"
	"time"

	"github.com/Rom204/argus/event"
)

// TestRecentEvents covers the one read the API needs: newest events first,
// optionally narrowed to a single type, capped by a limit.
func TestRecentEvents(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	ctx := context.Background()

	base := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }
	evt := func(typ event.Type, pid uint32, comm string) event.ProcessEvent {
		return event.ProcessEvent{Version: event.Version, Type: typ, PID: pid, PPID: 1, Comm: comm}
	}

	// Inserted deliberately out of order, so a passing test proves the SQL
	// sorts rather than the insert order happening to be right.
	err := db.Insert(ctx, [][]any{
		toRow(evt(event.TypeExecve, 200, "bash"), at(2)),
		toRow(evt(event.TypeExecve, 100, "sleep"), at(0)),
		toRow(evt(event.TypeExit, 100, "sleep"), at(1)),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Each want entry is "TYPE comm", newest first.
	tests := []struct {
		name  string
		query EventQuery
		want  []string
	}{
		{
			name:  "all types, newest first",
			query: EventQuery{Limit: 10},
			want:  []string{"EXECVE bash", "EXIT sleep", "EXECVE sleep"},
		},
		{
			name:  "limit truncates to the newest",
			query: EventQuery{Limit: 1},
			want:  []string{"EXECVE bash"},
		},
		{
			name:  "type filter keeps only EXECVE",
			query: EventQuery{Limit: 10, Type: "EXECVE"},
			want:  []string{"EXECVE bash", "EXECVE sleep"},
		},
		{
			name:  "type filter keeps only EXIT",
			query: EventQuery{Limit: 10, Type: "EXIT"},
			want:  []string{"EXIT sleep"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.RecentEvents(ctx, tc.query)
			if err != nil {
				t.Fatalf("RecentEvents: %v", err)
			}
			var got []string
			for _, r := range rows {
				got = append(got, r.Type+" "+r.Comm)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d rows %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("row %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRecentEvents_FieldsRoundTrip proves every column the API serves comes
// back with the value that went in — especially the two that change type on
// the way through Postgres.
func TestRecentEvents_FieldsRoundTrip(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	ctx := context.Background()

	when := time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC)
	in := event.ProcessEvent{
		Version: event.Version, Type: event.TypeExecve,
		PID: 4321, PPID: 4320,
		UID: 4294967295, GID: 65534, // full-range uint32: must survive as BIGINT
		Comm:         "ping",
		CapEffective: 0x2000, // CAP_NET_RAW
	}
	if err := db.Insert(ctx, [][]any{toRow(in, when)}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rows, err := db.RecentEvents(ctx, EventQuery{Limit: 1})
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	got := rows[0]

	if !got.Time.Equal(when) {
		t.Errorf("Time = %v, want %v", got.Time, when)
	}
	if got.Type != "EXECVE" || got.Comm != "ping" {
		t.Errorf("Type/Comm = %q/%q, want EXECVE/ping", got.Type, got.Comm)
	}
	if got.PID != 4321 || got.PPID != 4320 {
		t.Errorf("PID/PPID = %d/%d, want 4321/4320", got.PID, got.PPID)
	}
	if got.UID != 4294967295 || got.GID != 65534 {
		t.Errorf("UID/GID = %d/%d, want 4294967295/65534", got.UID, got.GID)
	}
	if got.Caps != 0x2000 {
		t.Errorf("Caps = %#x, want 0x2000", got.Caps)
	}
}
