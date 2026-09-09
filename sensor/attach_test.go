package sensor

import (
	"strings"
	"testing"

	"github.com/cilium/ebpf"
)

// Each program's SEC() name decides how it attaches: cilium/ebpf parses
// SEC("tp/sched/sched_process_exec") into a TracePoint program with AttachTo
// "sched/sched_process_exec", and SEC("kprobe/commit_creds") into a Kprobe with
// AttachTo "commit_creds". This is the mapping that keeps the attach loop from
// needing a hard-coded hook list.
func TestProgramAttachTarget(t *testing.T) {
	tests := []struct {
		name     string
		progType ebpf.ProgramType
		attachTo string
		want     attachTarget
	}{
		{
			name:     "process exec tracepoint",
			progType: ebpf.TracePoint,
			attachTo: "sched/sched_process_exec",
			want:     attachTarget{kind: attachTracepoint, group: "sched", name: "sched_process_exec"},
		},
		{
			name:     "syscall tracepoint",
			progType: ebpf.TracePoint,
			attachTo: "syscalls/sys_exit_setresuid",
			want:     attachTarget{kind: attachTracepoint, group: "syscalls", name: "sys_exit_setresuid"},
		},
		{
			name:     "kprobe on a kernel symbol",
			progType: ebpf.Kprobe,
			attachTo: "commit_creds",
			want:     attachTarget{kind: attachKprobe, name: "commit_creds"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := programAttachTarget(tc.progType, tc.attachTo)
			if err != nil {
				t.Fatalf("programAttachTarget() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("programAttachTarget() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A target we cannot parse must fail loudly at startup rather than leaving a
// program silently unattached — a probe that never fires looks exactly like a
// quiet system.
func TestProgramAttachTargetRejectsBadInput(t *testing.T) {
	tests := []struct {
		name     string
		progType ebpf.ProgramType
		attachTo string
		wantErr  string
	}{
		{"tracepoint without a group", ebpf.TracePoint, "sched_process_exec", "group/name"},
		{"tracepoint with an empty group", ebpf.TracePoint, "/sched_process_exec", "group/name"},
		{"tracepoint with an empty name", ebpf.TracePoint, "sched/", "group/name"},
		{"kprobe without a symbol", ebpf.Kprobe, "", "symbol"},
		{"unsupported program type", ebpf.XDP, "eth0", "XDP"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := programAttachTarget(tc.progType, tc.attachTo)
			if err == nil {
				t.Fatalf("programAttachTarget(%v, %q) succeeded, want an error", tc.progType, tc.attachTo)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}
