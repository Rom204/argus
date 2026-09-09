// Package event decodes the raw records Argus's eBPF probes write into the
// ring buffer.
//
// The layout here mirrors `struct process_event` in bpf/event.h byte for byte.
// That header is authoritative: nothing on this side may be reordered or
// resized independently. The kernel writes raw memory and this package reads
// raw memory, so a mismatch produces plausible-looking garbage rather than a
// compile error — which is why Size and Version are checked on every record.
package event

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	// Size is sizeof(struct process_event). The C side holds this with a
	// _Static_assert, so a layout change breaks the BPF build first.
	Size = 56

	// CommLen matches ARGUS_COMM_LEN (the kernel's TASK_COMM_LEN).
	CommLen = 16

	// Version matches ARGUS_EVENT_VERSION.
	Version = 2
)

// Field offsets within a record, matching bpf/event.h.
const (
	offTimestamp = 0
	offVersion   = 8
	offType      = 12
	offPID       = 16
	offPPID      = 20
	offUID       = 24
	offGID       = 28
	offComm      = 32
	offCaps      = 48
)

// Type identifies what the kernel observed. Values match enum argus_event_type.
type Type uint32

const (
	TypeExecve Type = 1
	TypeExit   Type = 2
	TypeSetuid Type = 3
	TypeCaps   Type = 4
)

func (t Type) String() string {
	switch t {
	case TypeExecve:
		return "EXECVE"
	case TypeExit:
		return "EXIT"
	case TypeSetuid:
		return "SETUID"
	case TypeCaps:
		return "CAPS"
	default:
		return fmt.Sprintf("TYPE(%d)", uint32(t))
	}
}

// ProcessEvent is one decoded record.
type ProcessEvent struct {
	// TimestampNS is nanoseconds on CLOCK_MONOTONIC (since boot), not wall
	// clock. Use a Clock to convert.
	TimestampNS uint64
	Version     uint32
	Type        Type
	PID         uint32 // TGID — the PID that ps shows
	PPID        uint32
	UID         uint32 // real uid, not effective — see bpf/event.h
	GID         uint32 // real gid, not effective
	Comm        string // task name, NUL-trimmed; not a full path

	// CapEffective is the capability mask actually in force, one bit per
	// capability (CAP_NET_RAW is bit 13, so ping reads 0x2000).
	CapEffective uint64
}

// ErrBadSize means the record was not exactly Size bytes, so the kernel and
// this decoder disagree about the layout. Decoding anyway would yield
// convincing nonsense.
type ErrBadSize struct{ Got int }

func (e ErrBadSize) Error() string {
	return fmt.Sprintf("record is %d bytes, want %d: kernel/user-space layout mismatch", e.Got, Size)
}

// ErrBadVersion means the producer emitted a struct version this decoder was
// not written for. The field exists precisely so this is detectable.
type ErrBadVersion struct{ Got uint32 }

func (e ErrBadVersion) Error() string {
	return fmt.Sprintf("event version %d, want %d: rebuild the BPF object and the agent together", e.Got, Version)
}

// Unmarshal decodes one ring buffer record.
//
// Fields are read at explicit offsets in native byte order — the kernel wrote
// them with the CPU's own endianness, and the offsets are stated here so this
// file can be diffed directly against bpf/event.h.
func Unmarshal(b []byte) (ProcessEvent, error) {
	if len(b) != Size {
		return ProcessEvent{}, ErrBadSize{Got: len(b)}
	}

	e := ProcessEvent{
		TimestampNS:  binary.NativeEndian.Uint64(b[offTimestamp:]),
		Version:      binary.NativeEndian.Uint32(b[offVersion:]),
		Type:         Type(binary.NativeEndian.Uint32(b[offType:])),
		PID:          binary.NativeEndian.Uint32(b[offPID:]),
		PPID:         binary.NativeEndian.Uint32(b[offPPID:]),
		UID:          binary.NativeEndian.Uint32(b[offUID:]),
		GID:          binary.NativeEndian.Uint32(b[offGID:]),
		Comm:         commString(b[offComm : offComm+CommLen]),
		CapEffective: binary.NativeEndian.Uint64(b[offCaps:]),
	}

	if e.Version != Version {
		return ProcessEvent{}, ErrBadVersion{Got: e.Version}
	}

	return e, nil
}

// commString trims the NUL padding the kernel leaves after a short task name.
func commString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// String renders one event as a single line. The capability mask is printed in
// hex — it is a bitmask, and decimal makes it unreadable.
func (e ProcessEvent) String() string {
	return fmt.Sprintf("%-6s pid=%-7d ppid=%-7d uid=%-5d gid=%-5d caps=%#-16x comm=%s",
		e.Type, e.PID, e.PPID, e.UID, e.GID, e.CapEffective, e.Comm)
}
