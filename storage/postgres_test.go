package storage

import (
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Rom204/argus/event"
)

//go:embed migrations/001_events.sql
var migrationSQL string

// testDB returns a DB whose connections all use a fresh schema holding the
// real migration, dropped again when the test ends. Tests using it can run in
// parallel without seeing each other's rows.
//
// It skips unless ARGUS_TEST_DSN points at a TimescaleDB instance — e.g. the
// one from `docker compose up -d` — and never runs under -short.
func testDB(t *testing.T) *DB {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: skipped with -short")
	}
	dsn := os.Getenv("ARGUS_TEST_DSN")
	if dsn == "" {
		t.Skip("integration test: set ARGUS_TEST_DSN to run (see CLAUDE.md §9)")
	}

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })

	schema := fmt.Sprintf("argus_test_%d", time.Now().UnixNano())
	setup := fmt.Sprintf(`
		CREATE EXTENSION IF NOT EXISTS timescaledb SCHEMA public;
		CREATE SCHEMA %[1]s;
		SET search_path TO %[1]s, public;
	`, schema)
	if _, err := admin.Exec(ctx, setup); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	if _, err := admin.Exec(ctx, migrationSQL); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := Open(ctx, dsn+sep+"search_path="+url.QueryEscape(schema+",public"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestProcessesView_PairsEachExecWithItsOwnExit(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	ctx := context.Background()

	t0 := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	evt := func(typ event.Type, pid uint32, comm string) event.ProcessEvent {
		return event.ProcessEvent{Version: event.Version, Type: typ, PID: pid, PPID: 1, Comm: comm}
	}

	err := db.Insert(ctx, [][]any{
		toRow(evt(event.TypeExecve, 100, "sleep"), at(0)),
		toRow(evt(event.TypeExit, 100, "sleep"), at(1)),
		// pid 100 recycled for a different process that is still running.
		toRow(evt(event.TypeExecve, 100, "cat"), at(5)),
		toRow(evt(event.TypeExecve, 200, "bash"), at(2)),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	type proc struct {
		pid     int32
		comm    string
		started time.Time
		exited  *time.Time
	}
	exit1 := at(1)
	want := []proc{
		{100, "sleep", at(0), &exit1},
		{200, "bash", at(2), nil},
		{100, "cat", at(5), nil},
	}

	rows, err := db.pool.Query(ctx,
		`SELECT pid, comm, started_at, exited_at FROM processes ORDER BY started_at`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer rows.Close()

	var got []proc
	for rows.Next() {
		var p proc
		if err := rows.Scan(&p.pid, &p.comm, &p.started, &p.exited); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("processes has %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.pid != w.pid || g.comm != w.comm || !g.started.Equal(w.started) {
			t.Errorf("row %d = pid %d %s started %v, want pid %d %s started %v",
				i, g.pid, g.comm, g.started, w.pid, w.comm, w.started)
		}
		switch {
		case w.exited == nil && g.exited != nil:
			t.Errorf("row %d (%s) exited_at = %v, want NULL (still running)", i, w.comm, *g.exited)
		case w.exited != nil && (g.exited == nil || !g.exited.Equal(*w.exited)):
			t.Errorf("row %d (%s) exited_at = %v, want %v", i, w.comm, g.exited, *w.exited)
		}
	}
}

func TestInsert_EventsReadBackFieldForField(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	ctx := context.Background()

	// Postgres stores microseconds; keep the fixture at that precision.
	wall := time.Date(2026, 9, 17, 12, 25, 4, 868_123_000, time.UTC)
	sudo := event.ProcessEvent{
		Version: event.Version, Type: event.TypeCaps,
		PID: 71915, PPID: 71894, UID: 0, GID: 1000,
		Comm: "sudo", CapEffective: 0x1ffffffffff,
	}
	long := event.ProcessEvent{
		Version: event.Version, Type: event.TypeExecve,
		PID: 71920, PPID: 71919, UID: 4294967295, GID: 4294967295,
		Comm: "cpuUsage.sh12345", // 16 chars: a comm that fills its field
	}

	if err := db.Insert(ctx, [][]any{toRow(sudo, wall), toRow(long, wall)}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rows, err := db.pool.Query(ctx,
		`SELECT time, version, type, pid, ppid, uid, gid, cap_effective, comm
		 FROM events ORDER BY pid`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer rows.Close()

	want := [][]any{toRow(sudo, wall), toRow(long, wall)}
	i := 0
	for rows.Next() {
		var (
			ts             time.Time
			version        int16
			typ, comm      string
			pid, ppid      int32
			uid, gid, caps int64
		)
		if err := rows.Scan(&ts, &version, &typ, &pid, &ppid, &uid, &gid, &caps, &comm); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if i >= len(want) {
			t.Fatalf("more rows than inserted")
		}
		got := []any{ts.UTC(), version, typ, pid, ppid, uid, gid, caps, comm}
		for c, col := range columns {
			if col == "time" {
				if !got[c].(time.Time).Equal(want[i][c].(time.Time)) {
					t.Errorf("row %d time = %v, want %v", i, got[c], want[i][c])
				}
				continue
			}
			if got[c] != want[i][c] {
				t.Errorf("row %d %s = %#v, want %#v", i, col, got[c], want[i][c])
			}
		}
		i++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if i != len(want) {
		t.Errorf("read back %d rows, want %d", i, len(want))
	}
}
