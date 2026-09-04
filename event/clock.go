package event

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// Clock converts the kernel's monotonic timestamps into wall-clock time.
//
// bpf_ktime_get_ns() returns nanoseconds on CLOCK_MONOTONIC — time since boot,
// which says nothing about the date. Sampling both clocks once at startup gives
// the wall-clock instant of boot, and every event timestamp is an offset from
// there.
//
// Monotonic is the right choice in the kernel: it cannot jump backwards when
// NTP adjusts the system clock, so event ordering stays sound. The cost is this
// conversion, done once here rather than per event.
type Clock struct {
	boot time.Time
}

// NewClock samples the monotonic and wall clocks together.
func NewClock() (Clock, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return Clock{}, fmt.Errorf("read CLOCK_MONOTONIC: %w", err)
	}

	uptime := time.Duration(ts.Sec)*time.Second + time.Duration(ts.Nsec)*time.Nanosecond
	return Clock{boot: time.Now().Add(-uptime)}, nil
}

// WallTime converts a kernel monotonic timestamp to wall-clock time.
func (c Clock) WallTime(monotonicNS uint64) time.Time {
	return c.boot.Add(time.Duration(monotonicNS))
}
