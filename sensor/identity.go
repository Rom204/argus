package sensor

import "github.com/Rom204/argus/event"

// identity is the credential pair Argus tracks per process.
type identity struct {
	uid uint32
	gid uint32
}

// identityTracker suppresses set*id events that did not actually change
// anything.
//
// The kernel probes stay dumb and report every successful set*id syscall
// (CLAUDE.md §6.2), which is the truthful record — but it is noisy: a single
// `sudo` makes 28 such calls while passing through only 4 distinct identities,
// because it repeatedly re-asserts credentials it already holds. Reporting all
// 28 would bury the four transitions that actually matter.
//
// So the filtering happens here, in user space, which is where §6.2 says it
// belongs.
type identityTracker struct {
	seen map[uint32]identity
}

func newIdentityTracker() *identityTracker {
	return &identityTracker{seen: make(map[uint32]identity)}
}

// observe reports whether ev should be emitted, updating the tracked state.
//
// EXECVE seeds a process's known identity, EXIT forgets it — which both bounds
// the map to live processes and prevents a recycled PID from inheriting the
// previous occupant's credentials.
func (t *identityTracker) observe(ev event.ProcessEvent) bool {
	current := identity{uid: ev.UID, gid: ev.GID}

	switch ev.Type {
	case event.TypeExecve:
		t.seen[ev.PID] = current
		return true

	case event.TypeExit:
		delete(t.seen, ev.PID)
		return true

	case event.TypeSetuid:
		// A process first seen mid-flight has no baseline, so its first
		// identity event is reported: we cannot claim nothing changed.
		if previous, known := t.seen[ev.PID]; known && previous == current {
			return false
		}
		t.seen[ev.PID] = current
		return true

	default:
		return true
	}
}

// tracked reports how many processes currently hold state, for tests.
func (t *identityTracker) tracked() int { return len(t.seen) }
