// SPDX-License-Identifier: GPL-2.0
/*
 * sensor.bpf.c — Argus kernel-side probes.
 *
 * Observes user-space process lifecycle and emits one `struct process_event`
 * per occurrence into a ring buffer (see event.h — that struct is the contract
 * with the Go agent).
 *
 * These programs stay deliberately dumb: they collect and emit, they do not
 * decide what is interesting (CLAUDE.md §6.2). The single exception is the
 * thread-vs-process check on exit, which is event *semantics* rather than
 * policy — see handle_exit().
 */
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

#include "event.h"

char LICENSE[] SEC("license") = "GPL";

/* 256KB. Must be a page-aligned power of two. At 48 bytes per event this
 * holds ~5,400 events, well above the 1,000 events/sec budget (CLAUDE.md §10)
 * even if the reader stalls briefly. */
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024);
} events SEC(".maps");

/*
 * Reserve a slot and fill every field common to all event types.
 * Returns NULL if the ring buffer is full — callers must check, and the
 * verifier will reject the program if they don't.
 */
static __always_inline struct process_event *event_begin(__u32 type)
{
	struct process_event *e;
	struct task_struct *task;
	__u64 pid_tgid, uid_gid;

	e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e)
		return NULL;

	e->timestamp_ns = bpf_ktime_get_ns();
	e->version = ARGUS_EVENT_VERSION;
	e->type = type;

	/* High half is the TGID — the PID userspace tools show. */
	pid_tgid = bpf_get_current_pid_tgid();
	e->pid = pid_tgid >> 32;

	/* Parent's TGID. The only field needing CO-RE: real_parent's offset
	 * inside task_struct varies between kernels, so BPF_CORE_READ emits a
	 * relocation the loader patches at load time. real_parent (not parent)
	 * is the true creator — parent can be changed by ptrace. */
	task = (struct task_struct *)bpf_get_current_task();
	e->ppid = BPF_CORE_READ(task, real_parent, tgid);

	uid_gid = bpf_get_current_uid_gid();
	e->uid = (__u32)uid_gid;
	e->gid = uid_gid >> 32;

	bpf_get_current_comm(&e->comm, sizeof(e->comm));

	return e;
}

/*
 * Process creation.
 *
 * Hooked at sched_process_exec, NOT sys_enter_execve. At syscall entry the
 * exec has not happened yet, so comm would still be the *calling* program
 * (every event would read "bash"), and the tracepoint also fires for execs
 * that go on to fail. sched_process_exec fires only after a successful exec,
 * when comm is the new program.
 */
SEC("tp/sched/sched_process_exec")
int handle_exec(void *ctx)
{
	struct process_event *e = event_begin(ARGUS_EVENT_EXECVE);

	if (!e)
		return 0;

	bpf_ringbuf_submit(e, 0);
	return 0;
}

/*
 * Process termination.
 *
 * This tracepoint fires for every *thread*, so a multi-threaded program
 * exiting would produce dozens of events for one process. Emitting only when
 * pid == tgid restricts us to the thread group leader, i.e. the process
 * itself. That is what "a process exited" means, not a policy filter.
 */
SEC("tp/sched/sched_process_exit")
int handle_exit(void *ctx)
{
	struct process_event *e;
	__u64 pid_tgid = bpf_get_current_pid_tgid();

	if ((__u32)pid_tgid != (pid_tgid >> 32))
		return 0;

	e = event_begin(ARGUS_EVENT_EXIT);
	if (!e)
		return 0;

	bpf_ringbuf_submit(e, 0);
	return 0;
}

/*
 * Identity changes.
 *
 * Hooked at syscall *exit* rather than entry for two reasons: the exit
 * tracepoint carries the return value, so we emit only on success instead of
 * reporting failed privilege attempts as if they worked; and by this point
 * the credentials have actually changed, so bpf_get_current_uid_gid() returns
 * the new uid/gid — which is what event.h documents those fields to hold.
 */
static __always_inline int handle_setid(struct trace_event_raw_sys_exit *ctx)
{
	struct process_event *e;

	if (ctx->ret != 0)
		return 0;

	e = event_begin(ARGUS_EVENT_SETUID);
	if (!e)
		return 0;

	bpf_ringbuf_submit(e, 0);
	return 0;
}

/* The whole set-*id family. setresuid matters most: it is what su and sudo
 * actually call, so hooking setuid alone would miss the most security-relevant
 * transition on the system. */
SEC("tp/syscalls/sys_exit_setuid")
int handle_setuid(struct trace_event_raw_sys_exit *ctx) { return handle_setid(ctx); }

SEC("tp/syscalls/sys_exit_setgid")
int handle_setgid(struct trace_event_raw_sys_exit *ctx) { return handle_setid(ctx); }

SEC("tp/syscalls/sys_exit_setreuid")
int handle_setreuid(struct trace_event_raw_sys_exit *ctx) { return handle_setid(ctx); }

SEC("tp/syscalls/sys_exit_setregid")
int handle_setregid(struct trace_event_raw_sys_exit *ctx) { return handle_setid(ctx); }

SEC("tp/syscalls/sys_exit_setresuid")
int handle_setresuid(struct trace_event_raw_sys_exit *ctx) { return handle_setid(ctx); }

SEC("tp/syscalls/sys_exit_setresgid")
int handle_setresgid(struct trace_event_raw_sys_exit *ctx) { return handle_setid(ctx); }
