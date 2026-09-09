package sensor

import (
	"testing"

	"github.com/Rom204/argus/event"
)

func ev(typ event.Type, pid, uid, gid uint32) event.ProcessEvent {
	return event.ProcessEvent{Type: typ, PID: pid, UID: uid, GID: gid, Comm: "test"}
}

// capsEv is ev with a capability mask and a parent, for the commit_creds path.
func capsEv(typ event.Type, pid, ppid, uid, gid uint32, caps uint64) event.ProcessEvent {
	e := ev(typ, pid, uid, gid)
	e.PPID = ppid
	e.CapEffective = caps
	return e
}

const capNetRaw = uint64(1) << 13

// A binary can gain privilege without any uid changing at all — file
// capabilities are exactly that, and are invisible to the set*id tracepoints.
func TestObserveDetectsCapabilityChange(t *testing.T) {
	tr := newIdentityTracker()

	tr.observe(capsEv(event.TypeExecve, 100, 99, 1000, 1000, 0))

	if !tr.observe(capsEv(event.TypeCaps, 100, 99, 1000, 1000, capNetRaw)) {
		t.Error("gaining CAP_NET_RAW with unchanged uid/gid should be emitted")
	}
	if tr.observe(capsEv(event.TypeCaps, 100, 99, 1000, 1000, capNetRaw)) {
		t.Error("re-installing the same capability set should be suppressed")
	}
}

// The behaviour that motivated this type: sudo re-asserts credentials it
// already holds, so only genuine transitions should survive.
func TestObserveSuppressesRepeatedIdentities(t *testing.T) {
	tr := newIdentityTracker()

	// Process starts as uid 1000.
	if !tr.observe(ev(event.TypeExecve, 100, 1000, 1000)) {
		t.Fatal("EXECVE should always be emitted")
	}

	tests := []struct {
		name string
		e    event.ProcessEvent
		want bool
	}{
		{"escalate to root", ev(event.TypeSetuid, 100, 0, 1000), true},
		{"re-assert root", ev(event.TypeSetuid, 100, 0, 1000), false},
		{"re-assert root again", ev(event.TypeSetuid, 100, 0, 1000), false},
		{"drop back", ev(event.TypeSetuid, 100, 1000, 1000), true},
		{"re-assert", ev(event.TypeSetuid, 100, 1000, 1000), false},
		{"gid change only", ev(event.TypeSetuid, 100, 1000, 0), true},
		{"full root", ev(event.TypeSetuid, 100, 0, 0), true},
	}
	for _, tc := range tests {
		if got := tr.observe(tc.e); got != tc.want {
			t.Errorf("%s: observe() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Well-behaved programs hold privilege only while they need it: ping raises
// CAP_NET_RAW, opens its socket, and drops it again, and sudo does the same
// several times over. Losing privilege is not a threat signal, and neither is
// taking back a level the process demonstrably already held.
func TestObserveSuppressesCapabilityDrops(t *testing.T) {
	tr := newIdentityTracker()

	tr.observe(capsEv(event.TypeExecve, 100, 99, 1000, 1000, capNetRaw))

	if tr.observe(capsEv(event.TypeCaps, 100, 99, 1000, 1000, 0)) {
		t.Error("dropping capabilities should be suppressed")
	}
	if tr.observe(capsEv(event.TypeCaps, 100, 99, 1000, 1000, capNetRaw)) {
		t.Error("re-raising to a level already held should be suppressed: the drop must not become the new baseline")
	}
}

// commit_creds runs inside the setuid syscall, so a single transition reaches
// the reader twice: CAPS first, then SETUID with an identical credential set.
// Only the first should print.
func TestObserveDedupsAcrossEventTypes(t *testing.T) {
	tr := newIdentityTracker()

	tr.observe(capsEv(event.TypeExecve, 100, 99, 1000, 1000, 0))

	if !tr.observe(capsEv(event.TypeCaps, 100, 99, 0, 0, 0)) {
		t.Error("the escalation to root should be emitted once")
	}
	if tr.observe(capsEv(event.TypeSetuid, 100, 99, 0, 0, 0)) {
		t.Error("the same transition reported by the setuid tracepoint should be suppressed")
	}
}

// A process already running when Argus starts has no recorded baseline, so its
// first identity event must not be suppressed — we cannot know it was a no-op.
func TestObserveEmitsFirstEventForUnknownProcess(t *testing.T) {
	tr := newIdentityTracker()

	if !tr.observe(ev(event.TypeSetuid, 200, 0, 0)) {
		t.Error("first SETUID for an unseen process should be emitted")
	}
	if tr.observe(ev(event.TypeSetuid, 200, 0, 0)) {
		t.Error("second identical SETUID should be suppressed")
	}
}

// State must not accumulate, and a recycled PID must not inherit the previous
// occupant's credentials.
func TestObserveForgetsExitedProcesses(t *testing.T) {
	tr := newIdentityTracker()

	tr.observe(ev(event.TypeExecve, 300, 0, 0))
	if tr.tracked() != 1 {
		t.Fatalf("tracked() = %d, want 1", tr.tracked())
	}

	if !tr.observe(ev(event.TypeExit, 300, 0, 0)) {
		t.Error("EXIT should always be emitted")
	}
	if tr.tracked() != 0 {
		t.Errorf("tracked() = %d after EXIT, want 0", tr.tracked())
	}

	// PID 300 is reused by a new process with the same credentials; because
	// the old state was dropped, this reports rather than being suppressed.
	if !tr.observe(ev(event.TypeSetuid, 300, 0, 0)) {
		t.Error("recycled PID should not inherit the previous process's identity")
	}
}

// Distinct processes must not interfere with each other.
func TestObserveIsPerProcess(t *testing.T) {
	tr := newIdentityTracker()

	tr.observe(ev(event.TypeExecve, 400, 1000, 1000))
	tr.observe(ev(event.TypeExecve, 401, 1000, 1000))

	if !tr.observe(ev(event.TypeSetuid, 400, 0, 0)) {
		t.Error("pid 400 changed identity, should be emitted")
	}
	if !tr.observe(ev(event.TypeSetuid, 401, 0, 0)) {
		t.Error("pid 401 changed identity independently, should be emitted")
	}
	if tr.observe(ev(event.TypeSetuid, 400, 0, 0)) {
		t.Error("pid 400 re-asserting should be suppressed")
	}
}
