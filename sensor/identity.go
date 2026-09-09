package sensor

import "github.com/Rom204/argus/event"

// identity is the credential set Argus tracks per process. Capabilities are
// part of it because a process can gain privilege without any uid changing —
// a binary with file capabilities does exactly that.
type identity struct {
	uid  uint32
	gid  uint32
	caps uint64
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
	current := identity{uid: ev.UID, gid: ev.GID, caps: ev.CapEffective}

	switch ev.Type {
	case event.TypeExecve:
		t.seen[ev.PID] = current
		return true

	case event.TypeExit:
		delete(t.seen, ev.PID)
		return true

	case event.TypeSetuid, event.TypeCaps:
		// Both types describe the same thing — the credentials a process
		// now holds — so they are compared against one shared baseline.
		// A sudo seen by both the commit_creds kprobe and the setresuid
		// tracepoint therefore prints once, not twice.
		//
		// A process first seen mid-flight has no baseline, so its first
		// identity event is reported: we cannot claim nothing changed.
		previous, known := t.seen[ev.PID]
		if !known {
			t.seen[ev.PID] = current
			return true
		}

		if previous == current {
			return false
		}

		// Giving up privilege is not a threat signal. Suppressing the drop
		// *without recording it* leaves the baseline at the highest
		// privilege the process has held, so taking that level back is
		// recognised as a no-op too — which is what collapses ping's and
		// sudo's raise/drop cycles down to their one real escalation.
		if isPrivilegeDrop(previous, current) {
			return false
		}

		t.seen[ev.PID] = current
		return true

	default:
		return true
	}
}

// isPrivilegeDrop reports whether current is strictly less privileged than
// previous: the same user and group, and not one capability bit that previous
// did not already hold.
//
// Any uid or gid change disqualifies it, however the capabilities move — a
// process becoming a different user is a transition worth reporting even when
// it sheds capabilities on the way.
func isPrivilegeDrop(previous, current identity) bool {
	if current.uid != previous.uid || current.gid != previous.gid {
		return false
	}
	gained := current.caps &^ previous.caps
	return gained == 0
}

// tracked reports how many processes currently hold state, for tests.
func (t *identityTracker) tracked() int { return len(t.seen) }
