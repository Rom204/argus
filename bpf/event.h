/* SPDX-License-Identifier: GPL-2.0 */
/*
 * event.h — the kernel/user-space contract for Argus.
 *
 * This struct is written into a BPF ring buffer by the kernel probes and read
 * back byte-for-byte by the Go agent. It is the ONE definition the Go decoder
 * and the Postgres schema both derive from (CLAUDE.md §6.2). Changing a field
 * means changing all three, and bumping ARGUS_EVENT_VERSION.
 *
 * Layout rules:
 *   - fixed-width types only, ordered widest -> narrowest
 *   - total size is a multiple of 8, so there is no trailing padding either
 *   - always append new fields, never insert: appending keeps every existing
 *     offset stable, which matters once there are rows on disk
 *   - NO __attribute__((packed)): natural alignment is what we want; the
 *     static asserts at the bottom are what enforces the layout
 */
#ifndef __ARGUS_EVENT_H
#define __ARGUS_EVENT_H

/* vmlinux.h defines __u32/__u64 itself; including both is a redefinition
 * error. Guarding keeps this header self-contained either way. */
#ifndef __VMLINUX_H__
#include <linux/types.h>
#endif

#define ARGUS_EVENT_VERSION 2u

/* Matches the kernel's TASK_COMM_LEN; bpf_get_current_comm() expects exactly this. */
#define ARGUS_COMM_LEN 16

/* Starts at 1 so an all-zero (uninitialised) record is detectably invalid. */
enum argus_event_type {
	ARGUS_EVENT_EXECVE = 1,
	ARGUS_EVENT_EXIT   = 2,
	ARGUS_EVENT_SETUID = 3,
	/* A credential set was installed (commit_creds). Broader than SETUID:
	 * it also covers capset() and the file capabilities a binary picks up
	 * at exec, neither of which any set*id syscall reports. */
	ARGUS_EVENT_CAPS   = 4,
};

struct process_event {
	/* CLOCK_MONOTONIC ns since boot (bpf_ktime_get_ns), NOT wall clock.
	 * User space converts to wall clock by adding the boot epoch. */
	__u64 timestamp_ns;   /* offset  0 */

	__u32 version;        /* offset  8 — ARGUS_EVENT_VERSION at emit time */
	__u32 type;           /* offset 12 — enum argus_event_type            */

	/* TGID, i.e. the PID `ps` shows — bpf_get_current_pid_tgid() >> 32. */
	__u32 pid;            /* offset 16 */
	__u32 ppid;           /* offset 20 */

	/* REAL uid/gid at emit time — bpf_get_current_uid_gid() reads cred->uid
	 * and cred->gid, not the effective pair. For SETUID and CAPS events
	 * this is the value *after* the transition; old->new pairs would need
	 * new fields + a version bump. */
	__u32 uid;            /* offset 24 */
	__u32 gid;            /* offset 28 */

	char  comm[ARGUS_COMM_LEN];  /* offset 32 — NUL-padded, not a full path */

	/* Effective capability mask (cred->cap_effective), the caps actually in
	 * force. Present on every event type, so a process's privilege is
	 * visible at exec and exit, not only when it changes. */
	__u64 cap_effective;  /* offset 48 */
};

_Static_assert(sizeof(struct process_event) == 56,
	       "process_event layout changed - update the Go decoder and DB schema");
_Static_assert(__builtin_offsetof(struct process_event, comm) == 32,
	       "process_event field order changed - update the Go decoder");
_Static_assert(__builtin_offsetof(struct process_event, cap_effective) == 48,
	       "process_event field order changed - update the Go decoder");

#endif /* __ARGUS_EVENT_H */
