package sensor

import (
	"testing"

	"github.com/Rom204/argus/event"
)

// Kernel threads are out of scope (CLAUDE.md §4.2) and are recognised by their
// parentage: kthreadd is PID 2 and every kernel thread descends from it.
func TestIsKernelThread(t *testing.T) {
	tests := []struct {
		name string
		e    event.ProcessEvent
		want bool
	}{
		{"kthreadd itself", event.ProcessEvent{PID: 2, PPID: 1, Comm: "kthreadd"}, true},
		{"child of kthreadd", event.ProcessEvent{PID: 4242, PPID: 2, Comm: "kworker/0:1"}, true},
		{"init", event.ProcessEvent{PID: 1, PPID: 0, Comm: "systemd"}, false},
		{"ordinary user process", event.ProcessEvent{PID: 5000, PPID: 4999, Comm: "bash"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isKernelThread(tc.e); got != tc.want {
				t.Errorf("isKernelThread(%+v) = %v, want %v", tc.e, got, tc.want)
			}
		})
	}
}
