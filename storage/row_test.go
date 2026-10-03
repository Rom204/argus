package storage

import (
	"testing"
	"time"

	"github.com/Rom204/argus/event"
)

func TestToRow_CapsWithHighBitSet_RoundTripsThroughInt64(t *testing.T) {
	t.Parallel()

	const caps = uint64(0x8000_0000_0000_2000)
	row := toRow(event.ProcessEvent{CapEffective: caps}, time.Time{})

	stored, ok := row[capColumn()].(int64)
	if !ok {
		t.Fatalf("cap_effective stored as %T, want int64", row[capColumn()])
	}
	if uint64(stored) != caps {
		t.Errorf("round trip gave %#x, want %#x", uint64(stored), caps)
	}
}

func capColumn() int {
	for i, c := range columns {
		if c == "cap_effective" {
			return i
		}
	}
	panic("no cap_effective column")
}

func TestToRow_ValuesFollowColumnOrder(t *testing.T) {
	t.Parallel()

	wall := time.Date(2026, 9, 17, 12, 25, 4, 865_000_000, time.UTC)
	ev := event.ProcessEvent{
		Version:      2,
		Type:         event.TypeExecve,
		PID:          71913,
		PPID:         71894,
		UID:          1000,
		GID:          1000,
		Comm:         "ping",
		CapEffective: 0x2000,
	}

	got := toRow(ev, wall)

	want := map[string]any{
		"time":          wall,
		"version":       int16(2),
		"type":          "EXECVE",
		"pid":           int32(71913),
		"ppid":          int32(71894),
		"uid":           int64(1000),
		"gid":           int64(1000),
		"cap_effective": int64(0x2000),
		"comm":          "ping",
	}

	if len(got) != len(columns) {
		t.Fatalf("row has %d values, columns has %d", len(got), len(columns))
	}
	if len(columns) != len(want) {
		t.Fatalf("columns = %v, want exactly the %d event columns", columns, len(want))
	}
	for i, col := range columns {
		w, ok := want[col]
		if !ok {
			t.Fatalf("unexpected column %q", col)
		}
		if got[i] != w {
			t.Errorf("column %q = %#v, want %#v", col, got[i], w)
		}
	}
}
