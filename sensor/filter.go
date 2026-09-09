package sensor

import "github.com/Rom204/argus/event"

// kthreaddPID is the PID of kthreadd, the parent of every kernel thread.
const kthreaddPID = 2

// isKernelThread reports whether an event describes a kernel thread rather than
// a user-space process.
//
// Kernel threads are out of scope (CLAUDE.md §4.2), and they are filtered here
// in user space rather than in the probes — §6.2 keeps producers dumb and puts
// filtering wherever it is cheapest to express correctly.
//
// Identification is by parentage: kthreadd is PID 2, and the kernel creates its
// threads as kthreadd's children, so a PPID of 2 marks one. kthreadd itself is
// matched by PID for the same reason.
func isKernelThread(ev event.ProcessEvent) bool {
	return ev.PID == kthreaddPID || ev.PPID == kthreaddPID
}
