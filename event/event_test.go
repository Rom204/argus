package event

import (
	"encoding/binary"
	"errors"
	"testing"
)

// record builds a well-formed 48-byte record at the offsets bpf/event.h
// declares. Written independently of the decoder's own constants where it
// matters, so a wrong offset in event.go shows up as a test failure rather
// than cancelling out.
func record(ts uint64, version uint32, typ Type, pid, ppid, uid, gid uint32, comm string) []byte {
	b := make([]byte, Size)
	binary.NativeEndian.PutUint64(b[0:], ts)
	binary.NativeEndian.PutUint32(b[8:], version)
	binary.NativeEndian.PutUint32(b[12:], uint32(typ))
	binary.NativeEndian.PutUint32(b[16:], pid)
	binary.NativeEndian.PutUint32(b[20:], ppid)
	binary.NativeEndian.PutUint32(b[24:], uid)
	binary.NativeEndian.PutUint32(b[28:], gid)
	copy(b[32:48], comm)
	return b
}

func TestUnmarshal(t *testing.T) {
	raw := record(1234567890, Version, TypeExecve, 4242, 1000, 1001, 1002, "bash")

	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	want := ProcessEvent{
		TimestampNS: 1234567890,
		Version:     Version,
		Type:        TypeExecve,
		PID:         4242,
		PPID:        1000,
		UID:         1001,
		GID:         1002,
		Comm:        "bash",
	}
	if got != want {
		t.Errorf("Unmarshal() =\n  %+v\nwant\n  %+v", got, want)
	}
}

// A task name of exactly CommLen bytes leaves no NUL terminator. The decoder
// must not read past the field or truncate the last character.
func TestUnmarshalCommFillsField(t *testing.T) {
	const full = "0123456789abcdef" // exactly 16 bytes
	if len(full) != CommLen {
		t.Fatalf("test fixture is %d bytes, want %d", len(full), CommLen)
	}

	got, err := Unmarshal(record(1, Version, TypeExit, 1, 1, 0, 0, full))
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Comm != full {
		t.Errorf("Comm = %q, want %q", got.Comm, full)
	}
}

// The whole point of the fixed layout: a record of the wrong size means the
// kernel and this binary disagree, and must be rejected rather than decoded
// into convincing nonsense.
func TestUnmarshalRejectsWrongSize(t *testing.T) {
	for _, size := range []int{0, Size - 1, Size + 1, Size + 4} {
		b := make([]byte, size)
		if size >= 12 {
			binary.NativeEndian.PutUint32(b[8:], Version)
		}

		_, err := Unmarshal(b)

		var bad ErrBadSize
		if !errors.As(err, &bad) {
			t.Errorf("Unmarshal(%d bytes) error = %v, want ErrBadSize", size, err)
			continue
		}
		if bad.Got != size {
			t.Errorf("ErrBadSize.Got = %d, want %d", bad.Got, size)
		}
	}
}

// The version field exists so a stale BPF object meeting a newer agent is
// detectable instead of silently misread.
func TestUnmarshalRejectsWrongVersion(t *testing.T) {
	_, err := Unmarshal(record(1, Version+1, TypeExecve, 1, 1, 0, 0, "x"))

	var bad ErrBadVersion
	if !errors.As(err, &bad) {
		t.Fatalf("Unmarshal() error = %v, want ErrBadVersion", err)
	}
	if bad.Got != Version+1 {
		t.Errorf("ErrBadVersion.Got = %d, want %d", bad.Got, Version+1)
	}
}

func TestTypeString(t *testing.T) {
	tests := map[Type]string{
		TypeExecve: "EXECVE",
		TypeExit:   "EXIT",
		TypeSetuid: "SETUID",
		Type(99):   "TYPE(99)",
	}
	for typ, want := range tests {
		if got := typ.String(); got != want {
			t.Errorf("Type(%d).String() = %q, want %q", uint32(typ), got, want)
		}
	}
}
