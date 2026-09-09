package sensor

import (
	"fmt"
	"strings"

	"github.com/cilium/ebpf"
)

// attachKind is how a program gets hooked into the kernel.
type attachKind int

const (
	attachTracepoint attachKind = iota
	attachKprobe
)

// attachTarget is where one program attaches, derived entirely from its SEC()
// name. group is empty for kprobes, where name is the kernel symbol.
type attachTarget struct {
	kind  attachKind
	group string
	name  string
}

// programAttachTarget maps a program's type and AttachTo value — both parsed by
// cilium/ebpf out of the SEC() name — to the attachment it needs.
//
// Keeping this a pure function is what makes the dispatch testable without a
// kernel: attaching needs root, deciding how to attach does not.
func programAttachTarget(progType ebpf.ProgramType, attachTo string) (attachTarget, error) {
	switch progType {
	case ebpf.TracePoint:
		group, name, found := strings.Cut(attachTo, "/")
		if !found || group == "" || name == "" {
			return attachTarget{}, fmt.Errorf("cannot parse tracepoint target %q, want group/name", attachTo)
		}
		return attachTarget{kind: attachTracepoint, group: group, name: name}, nil

	case ebpf.Kprobe:
		if attachTo == "" {
			return attachTarget{}, fmt.Errorf("kprobe has no target symbol, want SEC(\"kprobe/<symbol>\")")
		}
		return attachTarget{kind: attachKprobe, name: attachTo}, nil

	default:
		return attachTarget{}, fmt.Errorf("unsupported program type %s: add a case here when a probe needs it", progType)
	}
}
