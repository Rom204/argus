# CLAUDE.md

Guidance for Claude Code when working in this repository.

**Purpose:** the stable spec for Argus — what it is, what's locked, and how to work with Rom. Milestone progress lives in `ROADMAP.md`; solved problems live in `KNOWLEDGE_BASE.md`. If any of those conflict with this file, **this file wins**.

---

## 0. Operating rules — read before anything else

These four rules override convenience. They apply to every task in this repo, without exception.

### 0.1 Start of every task

When I name a milestone or a coding task, your first action is always:

1. Read the current milestone in `ROADMAP.md`.
2. Produce a work plan per §7 step 1 (PLAN) — scope in/out, ordered steps, module changes, test plan, open questions.
3. **Stop and wait for my approval.** Write no code, create no files.

Never begin implementing from my request alone.

**Exception — narrow, fully-specified requests I hand you directly.** Editing a doc, answering a question, renaming a symbol, fixing a line I pointed at: just do it. No `ROADMAP.md` read, no plan, no approval gate. The test is whether the request already contains its own scope. If you find yourself making design decisions I did not make, it is not narrow — stop and plan.

### 0.2 You cannot run privileged commands

`sudo` on this VM requires an interactive password and caches per-TTY. Your shell cannot use it. This affects apt installs, loading BPF programs, Docker, and systemd.

When a step needs root: **stop, and emit a fenced copy-paste block** for me to run in my own terminal, followed by what output you need back from me. Do not attempt the command "to see if it works." Do not route around it.

### 0.3 The two-attempt stop rule

If the same problem defeats you twice — verifier rejection, failing integration test, build error, anything — **stop.**

Report: what you tried (both attempts), the exact error output, your current hypothesis, and what you'd try next. Then wait.

Do not try a third approach unprompted. Do not silently change scope to avoid the obstacle. A fast, honest block report is worth more to me than an hour of autonomous thrashing.

### 0.4 What you hand me at the end of a task

My role is reviewer, not driver. A task is not delivered until you give me, in this order:

1. **What changed** — files touched, one line each.
2. **The raw test output** — pasted, not summarized. `go test ./...` and `go vet ./...`.
3. **Manual steps for me** — any privileged or interactive command I need to run, as a copy-paste block, plus what I should see.
4. **Deviations** — anything you did that departs from the approved plan or from §4–§6, and why.

Never claim tests pass. Show them passing.

---

## 1. Document map

| File | Owns | Grows when |
|------|------|------------|
| `CLAUDE.md` (this file) | Spec + guardrails: identity, locked decisions, principles, architecture contract, working style | A decision or scope changes |
| `ROADMAP.md` | Milestone checklist, per-milestone goals and sub-tasks, current status | Work progresses on a milestone |
| `KNOWLEDGE_BASE.md` | Engineering ledger: Issue → Root Cause → Resolution, plus concepts learned | A problem gets solved |
| `README.md` | Public-facing pitch (see §12) | Before sharing the repo |

---

## 2. Project in one paragraph

**Argus** — lightweight EDR for Linux, process observability via eBPF.

An Endpoint Detection & Response agent that runs on Linux hosts, uses eBPF to observe user-space process lifecycle at the kernel level, streams telemetry to a local queryable datastore, and provides the foundation for behavioral threat detection based on MITRE ATT&CK patterns. Positioned as a learning-oriented implementation of the same core pattern used by CrowdStrike, SentinelOne, Falco, and Tetragon.

- **Owner:** Rom, 2nd-year CS student at Reichman University
- **Purpose:** portfolio artifact for cybersecurity roles — *not* a production system
- **Timeline:** ~1 month

---

## 3. What v1 must prove

> "Process lifecycle events are captured in the kernel via eBPF, surfaced through a Go agent, persisted to a queryable datastore, and the whole thing runs inside a real Linux VM at an overhead small enough to be credible."

v1 is reached at the end of Milestone 3 in `ROADMAP.md`. M4 and M5 are optional.

---

## 4. Locked decisions

### 4.1 Tech stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Kernel probes | eBPF (C) | Industry standard; leverages Rom's C strength |
| User-space agent | Go + `cilium/ebpf` library | Best library ecosystem; matches cloud-native security industry; static binary deploy |
| Storage | PostgreSQL + TimescaleDB | Relational schema for analytics + time-series optimization |
| Orchestration | Docker Compose | Reproducible local deploy; requires `--privileged` for eBPF (standard, like Falco) |
| Base OS | Ubuntu 24.04.4 LTS **Server** ARM64 | Kernel 6.8, full BTF/CO-RE support; headless matches production EDR deployment |
| CI/CD | GitHub Actions | Build + vet + test on every push |

### 4.2 Scope

**In scope (v1):**
- Full lifecycle tracing of every user-space process: creation, identity/permission changes, termination
- Filtering out kernel threads (PPID == 2)
- Persistence to local PostgreSQL + TimescaleDB with a schema designed for downstream analytics
- Deployment as a systemd service from boot
- Packaging via Docker Compose for reproducible local deploy
- Runs inside a single Ubuntu VM on Rom's Mac (via UTM)

**Out of scope (v1):**
- No response actions (process kill, network block) — **detection only**
- No machine learning — rule-based detection only, deferred to a later milestone
- No Windows or macOS support
- No cloud deployment — strictly local
- No multi-endpoint / multi-tenant support (architecture should not preclude it, but it is not a v1 feature)
- No file event capture in early milestones (volume too high; added later with sampling)
- No network event capture until Milestone 3

### 4.3 Event detail level ("medium" scope)

**Captured per process:**
- Process creation (execve / fork)
- Process termination (exit)
- Identity changes during process lifetime (setuid, setgid, capabilities changes)

**Not captured in early milestones:**
- File open/read/write events — deferred, volume issue
- Network connections — added in Milestone 3
- Full system call trace — never, infeasible volume

### 4.4 Development environment

- **Host:** Mac Pro M2 (Apple Silicon, ARM64); editor VS Code
- **Hypervisor:** UTM
- **Guest OS:** Ubuntu 24.04.4 LTS Server ARM64, kernel 6.8.0-138-generic
- **VM identity:** hostname `romubuntuvm`, host-only IP `192.168.64.5`, user `rom` (sudo-enabled)
- **VM resources:** 4 vCPU, 4GB RAM, 30GB virtual disk; root filesystem 27GB (the Ubuntu Server installer originally left half the volume group unallocated — extended with `lvextend -l +100%FREE` + `resize2fs` on 2026-09-02)
- **Toolchain** (verified on this VM 2026-09-02): `clang 18.1.3`, `LLVM 18.1.3`, `libbpf-dev 1.3.0` — all installed **from apt**, which is exactly what `ci.yml` does, so dev and CI stay on identical versions. Go 1.27.1 from the official tarball in `/usr/local/go`, on PATH via `/etc/profile.d/go.sh` (apt's Go is 1.22, too old for this module).
- **Connection:** VS Code Remote-SSH → code lives on the VM, edited from the Mac. All git/build/test commands run **in the VM shell**, never on the Mac.
- **Root access:** see §0.2. Privileged steps must be handed to Rom, never attempted.

Verify environment facts with `uname -a` / `hostname -I` when it matters — the written values here are the fastest-rotting part of this doc. **After any VM rebuild or migration, re-run the M0 checks rather than trusting the checkboxes** — installed libraries (`libclang1-18`) do not imply installed tools (`clang`); use `command -v clang` and `go version`.

### 4.5 Repo conventions

- `bpf/*.o` and the built `argus` binary are gitignored — never commit build output.
- Any new build-output location (a future `Makefile`'s `bin/`/`dist/`) needs its own `.gitignore` entry added **proactively**.
- `go.mod`'s `go` directive is deliberately pinned to match `ci.yml`'s `actions/setup-go` version — keep both aligned (see `KNOWLEDGE_BASE.md`). Currently **1.27** in three places: the VM's `/usr/local/go`, `go.mod`, and `ci.yml`.
- `bpf/event.h` is the kernel/user-space contract (§6.2). Its `_Static_assert`s deliberately break the build when the layout changes — that is a prompt to update the Go decoder and DB schema, never something to silence. **Append fields, never insert**, so existing offsets stay stable.

---

## 5. Engineering principles

Treat these as acceptance criteria, not suggestions. Deliberately scaled to a solo, ~1-month, single-host project.

**5.1 Modular Go packages.** As the code grows past the current stub, keep responsibilities separated: BPF object loading, event decoding, and persistence each get their own package. `main.go` stays thin — wiring and lifecycle only, never business logic.

**5.2 Write eBPF for the verifier.** The kernel verifier rejects anything it cannot statically prove terminates or stays in bounds. Keep BPF programs simple and flat: bounded loops only (or none), no unbounded iteration, no deep pointer chasing without explicit bounds checks. Always compile with `-O2` — unoptimized BPF frequently fails verification.

**5.3 CO-RE over kernel headers.** Use BTF/`vmlinux.h` CO-RE style, not `linux-headers-$(uname -r)`. Re-adding kernel headers must be a deliberate, justified choice (see `KNOWLEDGE_BASE.md`), never a default copy-pasted from a non-CO-RE tutorial.

**5.4 DRY within reason.** Shared types and helpers live in one place. But a little duplication beats a wrong abstraction — if extracting something would couple two unrelated things, don't.

**5.5 Tests for each stage.** Every milestone delivers tests appropriate to its layer: Go unit tests for decoding/parsing logic; integration tests for the DB layer once M2 lands; the sensor itself validated end-to-end via manual QA. Tests must run from a single documented command and pass before a milestone is marked done. **How those tests get written is governed by §14 — this project is test-first.**

**5.6 No speculative abstraction.** Do **not** wrap dependencies (`cilium/ebpf`, the DB driver) in swappable adapter interfaces "in case we switch later." At this project's size that is ceremony, not architecture. The one contract worth designing up front is §6 — because the kernel/user-space boundary is genuinely hard to change later, not because abstraction is inherently good.

---

## 6. Architecture

### 6.1 High level

**Kernel space** (`bpf/*.bpf.c`) — eBPF programs hook execve/exit/setuid tracepoints, compiled with `clang -target bpf`, CO-RE style. Events flow out through a ring buffer.

**User space, inside the VM** (`main.go` + packages) — the Go agent loads and attaches the BPF objects with `cilium/ebpf`, reads events from the ring buffer, decodes them, and writes to PostgreSQL + TimescaleDB (running in Docker Compose).

Deployment target: `docker compose up` with `--privileged` for eBPF access, plus a systemd unit for boot-time start (M3).

### 6.2 The event struct is the contract

Kernel space and user space are decoupled by exactly one thing — the **event structure** written into the ring buffer. Get this right early; everything else can be refactored freely.

- **One definition, two consumers.** The BPF C struct definition is authoritative; the Go decoding struct and the DB schema both derive from it. When it changes, all three change together — never let them drift.
- **Fixed layout, explicitly sized fields.** Use `__u32`/`__u64`-style fixed-width types and mind alignment/padding — the Go side reads raw bytes, so implicit padding differences are silent corruption.
- **Include a version/type field from day one**, even while only execve exists. Adding exit, setuid (M1) and tcp_connect (M3) hooks should mean adding event *types*, not reshaping the pipeline.
- **Producers stay dumb.** BPF programs collect and emit; they don't filter policy. Filtering (e.g. kernel threads, PPID == 2) belongs wherever it is cheapest to express correctly — prefer user space unless volume proves otherwise.
- **The ring buffer read loop is written once.** Adding a new hook must not require touching the reader or the storage layer.

---

## 7. Working process

For each milestone in `ROADMAP.md`:

1. **PLAN** — Claude Code produces a work plan: scope (in/out), ordered implementation steps, module changes, how §5's principles are satisfied, test plan, open questions.
2. **APPROVE** — Rom answers open questions, rejects, or approves. **No code before approval.**
3. **IMPLEMENT** — Claude Code implements the approved plan, adhering to §4–§6.
4. **UPDATE DOCS** — On completion: check off the sub-tasks in `ROADMAP.md`; record any new gotcha in `KNOWLEDGE_BASE.md`.
5. **SIGN-OFF** — Rom reviews. On approval, the next milestone starts.

**Rules:**
- **Approval is required at the milestone-plan level only.** Once I approve a plan, §14 governs the work inside it — you do not need approval for each Red/Green/Refactor cycle. Write the failing test and proceed.
- Keep planning and implementation concise — this process exists to bound scope, not generate paperwork.
- Never silently expand scope. Surface scope changes in the plan, and if scope must change mid-implementation, stop per §0.3.
- Prefer dedicated tools over shell one-liners; match existing code style as the codebase grows.

---

## 8. Definition of done (every milestone)

> **This checklist is the authority for milestone completion.** §14.5 defines completion for a single task inside a milestone; §0.4 defines what you hand me. Where any of them appear to conflict, this checklist wins.

- [ ] Approved plan implemented; scope matches (no silent additions)
- [ ] §5 principles honored
- [ ] Tests written for this stage **and passing** via the documented command
- [ ] `go vet ./...` clean; CI green on push
- [ ] Performance budget checked against §10 (M3 onward, once measurable)
- [ ] `ROADMAP.md` sub-tasks checked off
- [ ] Any new gotcha recorded in `KNOWLEDGE_BASE.md`
- [ ] Rom's sign-off received before the next milestone starts

---

## 9. Commands

There is no Makefile. These are the exact commands CI runs and the same ones to use locally on the VM:

```bash
# Compile all eBPF programs (run from repo root)
for f in bpf/*.bpf.c; do
  clang -O2 -g -target bpf -D__TARGET_ARCH_arm64 \
    -I/usr/include/$(uname -m)-linux-gnu -c "$f" -o "${f%.c}.o"
done

go vet ./...
go build ./...
go test ./...          # single test: go test ./... -run TestName
```

Both flags are load-bearing — see `KNOWLEDGE_BASE.md` before touching the compile command. `-I/usr/include/$(uname -m)-linux-gnu` resolves `asm/types.h`; `-D__TARGET_ARCH_arm64` is what `bpf_tracing.h` needs to pick its `PT_REGS_*` macros for kprobes (under `-target bpf` no host arch macro exists, so its fallback finds nothing and the build fails outright). It is hard-coded rather than derived from `uname -m` because `bpf/vmlinux.h` is a committed arm64 dump — CI's x86_64 runner must still compile it as arm64.

---

## 10. Performance budget (soft targets)

| Metric | Target |
|--------|--------|
| CPU (idle host) | < 1% |
| CPU (busy host) | < 5% average |
| RAM (resident) | < 100 MB |
| Event latency (kernel → DB) | < 500 ms |
| Event throughput | 1,000 events/sec without drops |
| Startup time | < 2 seconds |

Soft budget policy: targets are stated in the README, current measured values are documented in `ROADMAP.md` from M3 onward, deviations are acknowledged rather than hidden. No hard enforcement in v1.

---

## 11. Data schema plan

Deliberately deferred until Milestone 2 — the final schema should be shaped by real usage during the sensor build, not guessed up front.

**Initial approach:** normalized schema with a `processes` table (one row per process instance) and a `process_events` table (identity changes, FK to `processes`). TimescaleDB hypertable on the time-series side.

---

## 12. Distribution strategy

- **Primary audience:** recruiters and hiring engineers reading the README (95% of readers)
- **Secondary:** engineers who actually run the code (~5%)

**README-first strategy:** architecture diagram, 30–60 second demo GIF (highest-impact single asset), clear "What It Does" and "What It Does Not Do" sections, tech stack table with a rationale column, Architecture Decision Records in `docs/decisions.md`.

**Deployment:** `docker compose up` inside a Linux VM.

**No cloud deployment — deliberate.** EDR agents run on the customer's host; a cloud demo undermines the mental model.

---

## 13. Working with Rom

- **Do not skip skill files.** Always view relevant `SKILL.md` files before creating or editing code.
- **Background:** strong in C and systems programming (top grades in Data Structures and Systems Programming in C); comfortable in Node.js/TypeScript from prior full-stack work; **new to Go and eBPF**; **not strong in math** — avoid math-heavy explanations.
- **Communication style:** concise, dialogue-oriented. Prefer short responses that leave room for follow-up questions.
- **Analogies only when Rom asks for them** — do not analogize by default.
- **Explain conceptual questions plainly** — assume OS fundamentals, not much else.
- **Push back honestly** on scope creep and premature optimization. Rom has a known pattern of blank-page paralysis dressed as thoroughness — call it out when it appears.
- **Anchor to the goal:** a portfolio artifact meant to open doors at cybersecurity companies, not a production system.

---

## 14. Development Methodology — Test-Driven Development (TDD)

This project is built test-first. These rules are binding for all code you write here.

### 14.1 The core cycle — Red / Green / Refactor

For every unit of behavior, in this order:

1. **RED** — Write exactly ONE failing test that describes the desired behavior. Run it. Show me the failure output. A test that passes on the first run is a bug in the test, not a success.
2. **GREEN** — Write the minimum code that makes it pass. Ugly is fine at this stage. Do not add functionality the test does not demand.
3. **REFACTOR** — Clean up the implementation with the test as a safety net. Re-run the suite. Behavior must not change.

Then repeat for the next unit of behavior.

### 14.2 Hard rules

- **Never write implementation code before a failing test exists for it.** If I ask for a feature, your first action is a test.
- **One failing test at a time.** Do not write a batch of tests upfront.
- **Never weaken, skip, or delete a test to make the suite green.** If you believe a test is wrong, stop and tell me why before touching it.
- **Test behavior, not implementation.** Assert on the public API's inputs and outputs. Do not assert on private fields, call counts, or internal ordering unless that ordering IS the requirement.
- **No mocking of things I own unless a real one is impractical.** Prefer real structs and in-memory fakes over mock frameworks.
- Every bug fix starts with a failing test that reproduces the bug.

### 14.3 Where TDD applies in this codebase

| Layer | Approach |
|---|---|
| Go user-space logic (parsing, enrichment, filtering, event handling) | Strict TDD. Full unit coverage. |
| Storage layer (PostgreSQL / TimescaleDB) | Integration tests against a real containerized DB. Test-first, but coarser granularity. |
| eBPF / kernel-side C | Not unit-testable. Cover with integration tests: spawn a real process, assert the expected event surfaces in user space. |
| `main()`, config loading, wiring, CLI flags | Thin by design. One smoke test. Do not chase coverage here. |

Keep business logic OUT of the layers that are hard to test. If something is hard to test, that is a design signal — extract it into a pure function and test that. (This is §5.1 restated from the testing side.)

### 14.4 Go testing conventions

- Standard library `testing` package. Table-driven tests are the default shape.
- Test names describe behavior: `TestParseExecEvent_TruncatedBuffer_ReturnsError`, not `TestParse2`.
- Each table case gets a descriptive `name` field and runs under `t.Run`.
- Tests live beside the code as `*_test.go`.
- The full suite must pass with `go test ./...` before any work is considered done.
- Use `t.Parallel()` where safe; guard integration tests behind `testing.Short()`.

### 14.5 What "done" means

A single task is done when: a failing test existed first, the implementation makes it pass, the code has been refactored, and `go test ./...` is fully green. Report per §0.4. Milestone-level completion is governed by §8.

### 14.6 Coverage

Do not chase a coverage percentage. High coverage on logic, near-zero on wiring, is the correct shape. Never write a test whose only purpose is to raise a number.