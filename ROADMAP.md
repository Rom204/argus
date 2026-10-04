# Roadmap — Argus

> Milestone-by-milestone checklist. Open this at the start of every work session. Check off sub-tasks as they complete. When a milestone is fully checked, move on.
>
> For the spec see `CLAUDE.md`. For solved bugs see `KNOWLEDGE_BASE.md`.

---

## How to use this file

Each milestone below has:

- **Goal** — one sentence describing what "done" looks like
- **Context to load** — the exact opening message to paste into a new Claude Code chat for that milestone
- **Sub-tasks** — ordered checkboxes. Do them in order unless there's an explicit reason not to.
- **Done when** — the acceptance test for the whole milestone

The rhythm: open a fresh conversation per milestone (or per sub-task if a milestone is long), paste the "Context to load" line, work through the sub-tasks, check them off, get Rom's sign-off, move on.

---

## Current status

> **Scope change, 2026-10-03.** The project was re-scoped for an interview demo on 2026-10-07.
> Milestones 3, 4 and 5 below are **cancelled, not pending** — see "Deliberately out of scope".
> The deliverable is now M0–M2 plus Milestone D: collect process events, store them, show them in
> a web table.

**Where we are right now (2026-10-04):** the pipeline runs end to end, verified by hand for the
first time. `sudo ./argus` writes events to Postgres; `./argus-api` serves them as JSON and serves
the page; the page renders them in a browser on the Mac and refreshes every 2 seconds. 32 tests
pass, `go vet` is clean, `gofmt` is clean.

M1 complete (2026-09-17). **M2 complete and finally QA'd (2026-10-04)** — the manual QA below had
never been run, and when it was, the `events` table turned out to have been empty the whole time
(see `KNOWLEDGE_BASE.md`). It now holds real rows. Milestone D complete apart from the firewall step.

**Next actionable sub-task:** Milestone D.8 — `sudo ufw allow 8080/tcp`, so the demo URL
`http://192.168.64.5:8080` works without VS Code running. Then Tuesday's rehearsal.

**Known gap, unchanged:** fork-without-exec produces no creation event (note under M1.13).

---

## Milestone 0 — Environment & development setup ✅

**Goal:** a Linux VM running with the eBPF toolchain, editable from the Mac via VS Code Remote-SSH.

- [x] UTM VM created — Ubuntu 24.04.4 LTS Server ARM64, 4 vCPU / 4GB / 30GB disk (root filesystem extended to 27GB; the installer had left half the volume group unallocated)
- [x] `openssh-server` installed and enabled on the VM (via `ssh.socket` — `ssh.service` reads "disabled" under socket activation, which is normal on 24.04 and not a fault)
- [x] System updated, git identity configured
- [x] SSH keypair generated on the VM, public key added to GitHub — verified live with `ssh -T git@github.com`
- [x] Repo cloned to `~/projects/argus`, VS Code Remote-SSH connected
- [x] Toolchain installed and verified **on this VM** (2026-09-02): `clang 18.1.3`, `LLVM 18.1.3`, `libbpf-dev 1.3.0` (all from apt, identical to CI), Go 1.27.1 in `/usr/local/go`, BTF present (`/sys/kernel/btf/vmlinux`)

**Done when:** `ssh rom@192.168.64.5` works, the repo is cloned in the VM, and `clang --version` + `go version` both succeed. ✅

> **Re-verified 2026-09-02.** These boxes were originally checked against the *Desktop* VM and carried across the rebuild unverified — the toolchain one turned out to be false. Every box above has now been re-run against the live Server VM. See `KNOWLEDGE_BASE.md`, "the build toolchain was never reinstalled after the VM rebuild".

---

## Milestone 1 — eBPF sensor tracing process lifecycle ✅

**Goal:** running the Go agent on the VM prints a structured line for every process create / identity-change / exit on the host, with kernel threads filtered out.

**Context to load (paste into a new Claude Code chat):**
> Read `CLAUDE.md`, `ROADMAP.md`, and `KNOWLEDGE_BASE.md`. We are working on Milestone 1. Continue from the next unchecked sub-task in the M1 section of the roadmap. Follow the working process in `CLAUDE.md` §7 — plan first, wait for my approval, then implement.

### Sub-tasks

- [x] Create `bpf/` directory and Go module skeleton (`github.com/Rom204/argus` — must match the GitHub remote, since it is the import prefix for every package)
- [x] Write `bpf/sensor.bpf.c` as a hello-world `sys_enter_execve` tracepoint hook using `bpf_printk`
- [x] Set up GitHub Actions CI (compile BPF, `go vet`/`go build`/`go test`)
- [x] **M1.5** — Define the event struct in a shared header (`bpf/event.h`): fixed-width types, version/type field, room for execve/exit/setuid variants (see `CLAUDE.md` §6.2) — 48 bytes, zero padding, layout locked by `_Static_assert`; verified by compiling for the BPF target and by printing real `offsetof` values
- [x] **M1.6** — Add a BPF ring buffer map to `sensor.bpf.c`, emit an execve event through it (replace `bpf_printk`) — 256KB ringbuf; hook moved from `sys_enter_execve` to `sched/sched_process_exec` so `comm` is the *new* binary and failed execs are not reported. Verified: executing a renamed copy of `/bin/true` produced exactly one EXECVE with `comm=argustest`
- [x] **M1.7** — Add `sched_process_exit` tracepoint hook, emit exit events — the tracepoint fires per *task*, so the hook emits only when `pid == tgid` (the thread group leader), which is what "a process exited" means. Verified with an 8-thread fixture: 9 tasks terminated, exactly 1 EXIT event, on the leader's pid
- [x] **M1.8** — Add setuid/setgid hooks, emit identity-change events — six `sys_exit_set*id` tracepoints over one shared helper, emitting only on `ret == 0` so failed privilege attempts are not reported as successes. Repeated no-op calls are suppressed in the Go reader (`sensor/identity.go`): one `sudo` went from 28 events to the 4 real transitions
- [x] **M1.8b** — *(deferred from M1.8)* Capabilities tracking — a `kprobe/commit_creds` reading `cred->cap_effective`, plus a `cap_effective` field appended to the event contract (48 → 56 bytes, version 1 → 2). Every event type now carries the caps in force, not just the ones that change them. The kprobe overlaps the set\*id tracepoints on purpose — it also catches `capset()` and the file capabilities a binary gains at exec — and the duplicate is collapsed in user space, where `identityTracker` now dedups on the `(uid, gid, caps)` triple across event types. Two build consequences: `bpf_tracing.h` forced `-D__TARGET_ARCH_arm64` into the compile command, and `kernel_cap_t` is a plain `u64` only since kernel 6.3. Manual QA then forced two corrections: the probe now compares the incoming cred against the task's current one and stays silent when nothing changed (exec calls `commit_creds`, which was costing 211 redundant events out of 226 execs), and the Go tracker reports capability *gains* only, since `sudo` and `ping` legitimately raise and drop privilege several times per run
- [x] **M1.9** — Add `cilium/ebpf` to `go.mod`; write the Go loader in a `sensor/` package: load object → attach programs → read ring buffer → decode → print structured events to stdout — attachment is driven entirely by each program's `SEC()` name (no hook list in Go), so adding a probe needs no change to `sensor/`. Verified: all 8 programs attach and all three event types reach stdout; a single failed attach aborts startup, so a clean start proves every program loaded
- [x] **M1.10** — Kernel-thread filtering (PPID == 2) — implemented in user space per §6.2 as `isKernelThread` in `sensor/filter.go`, matching kthreadd itself and its children; covered by `sensor/filter_test.go`
- [x] **M1.11** — Graceful shutdown: signal handling (SIGINT/SIGTERM), detach BPF programs on exit — `signal.NotifyContext` in `main.go`; cancelling the context closes the ring buffer reader, which unblocks `Run`, and `Sensor.Close` detaches every link
- [x] **M1.12** — Unit tests for the event decoder (fed known byte layouts, expects correct struct output) — `event/event_test.go`: full record, comm filling the field with no NUL, wrong size rejected, wrong version rejected, type rendering
- [x] **M1.13** — Manual QA: run agent, spawn processes (`sleep 1 &`, `su -c 'id'`, etc.), verify correct structured output; run kernel workload (`stress-ng`), verify no kernel threads leak through — *2026-09-17: EXECVE/EXIT/CAPS correct for `true`, `id`, `ping` → `caps=0x2000`, `sudo` → `true` as uid 0; `stress-ng --cpu 4` under capture produced 0 `kworker`/`ksoftirqd`/`kthreadd`/`migration` lines.*

**Known gap, deferred by decision (2026-09-17):** `fork()` without `exec` produces no creation event — such processes appear only at EXIT. §4.3 lists fork as in scope; fix is an additive `sched_process_fork` probe + `FORK` event type. See `KNOWLEDGE_BASE.md`. Revisit when planning M2's `processes` table.

**Done when:** on the VM, `sudo ./argus` prints one correctly-structured line per user-space process create/exit/setuid event, kernel threads are absent from output, and Ctrl+C shuts down cleanly.

---

## Milestone 2 — Persistence layer ✅

**Goal:** events land in a queryable database instead of stdout, and can be queried back with correct data.

**Context to load:**
> Read `CLAUDE.md`, `ROADMAP.md`, and `KNOWLEDGE_BASE.md`. We are working on Milestone 2. Milestone 1 is complete — the agent prints structured events to stdout. Now we replace stdout with PostgreSQL + TimescaleDB. Follow `CLAUDE.md` §7.

### Sub-tasks

- [x] Write `docker-compose.yml` bringing up PostgreSQL + TimescaleDB with a persistent volume — `timescale/timescaledb:2.30.1-pg17`, loopback-only port, migration mounted as a single file (see `KNOWLEDGE_BASE.md`)
- [x] Design schema per `CLAUDE.md` §11 — one `events` hypertable + `processes` view (decision changed from the normalized plan, see §11); `storage/migrations/001_events.sql`, applied on first `docker compose up`
- [x] Add DB driver dependency to `go.mod` — `jackc/pgx/v5` v5.11.0
- [x] Create a `storage/` package: connection setup, batched writes to avoid blocking the ring buffer reader — `row.go`, `writer.go` (500 rows / 100 ms), `postgres.go` (`COPY`); 6 unit tests race-clean + 2 integration tests
- [x] Wire the storage package into `main.go` — DB write added **alongside** the stdout print (Rom's call, keeps the live demo); DSN from `ARGUS_DB_URL`, defaulting to the compose DB so `sudo ./argus` works
- [x] Write 3–5 example queries — `docs/example-queries.sql`: recent execs, process tree under the latest `sudo`, privilege gains, top binaries, short-lived processes
- [x] Integration test: event in → row out (real DB, local compose + CI service container) — field-for-field read-back, plus `processes` view pairing with PID reuse (mutation-checked: removing the time condition from the view makes it fail). CI run unconfirmed until Rom pushes
- [ ] Manual QA: run agent, spawn processes, run example queries, verify correct rows

**Done when:** the agent runs against a live DB, processes executed on the host produce correct rows, and the example queries return sensible results. Integration test passes.

---

## Milestone D — Demo API + UI ✅

**Goal:** the stored events are readable over a REST API and visible in a browser table, as three
separate processes: collector (root), API (unprivileged), browser.

### Sub-tasks

- [x] **D.1** Verify the agent actually writes to the database — never done before 2026-10-04. 186 rows on the first run; `timeout` sends SIGTERM, so this also proved the final batch flushes on shutdown
- [x] **D.2** Readability rename across C, Go and SQL — `AS s`/`AS x` → `start_ev`/`exit_ev`, `e` → `event`, `b` → `raw`, `ve` → `verifierErr`, `l` → `probeLink`, `coll` → `collection`, `ev` → `evt`. Method receivers left short, which is Go convention. Pure renaming; compiler + 22 existing tests were the safety net. Also fixed a pre-existing `gofmt` failure in `main.go` (unsorted imports)
- [x] **D.3** `storage/query.go` — `RecentEvents` with parameterised SQL (`$1`, `$2`) and explicit `::text` casts, because Postgres cannot infer a bare parameter's type from `$1 = ''`. Returns a non-nil empty slice so the JSON is `[]`, not `null`
- [x] **D.4** `api/query.go` — `parseEventQuery`, a pure function over `url.Values`. `limit` 1–1000, `type` matched against an allow-list of the four contract values. 15 table-driven cases, no HTTP and no database
- [x] **D.5** `api/api.go` — `EventStore` interface declared by the consumer, so the 7 handler tests run against a hand-written fake with no Docker. Separate `eventJSON` wire type so Go field names and JSON keys can move independently. Capabilities serialised as hex
- [x] **D.6** `web/index.html` + `web/web.go` — 179 lines, no CDN, no framework, no build step. Table, type filter, Pause, 2-second refresh. Embedded in the binary; the embed lives in `web/` because `//go:embed` cannot reach a parent directory
- [x] **D.7** `cmd/argus-api/main.go` — binds `0.0.0.0:8080`, graceful shutdown via `signal.NotifyContext` + `Shutdown`, `ReadHeaderTimeout` set. Needs no root
- [ ] **D.8** `sudo ufw allow 8080/tcp` — *Rom, needs sudo.* Without it the Mac browser hangs with no error on `192.168.64.5:8080`. VS Code Remote-SSH's automatic port forwarding makes `localhost:8080` work as a second route, but only while VS Code is open
- [ ] **D.9** Rehearsal (Tuesday): twice, from a cold VM boot, Mac Wi-Fi off, inside 5 minutes; write `docs/DEMO.md`

**Done when:** `docker compose up -d`, `sudo ./argus`, `./argus-api`, then
`http://192.168.64.5:8080` on the Mac shows a populating table, and a command typed in an SSH
session appears within ~2 seconds.

**Verified 2026-10-04:** `GET /` → 200 text/html; `GET /api/events` → 200 JSON with real rows;
`?type=exit` filters; `?limit=0` → 400 with a JSON error; unknown path → 404; `POST` → 405;
Ctrl+C exits clean.

---

## Deliberately out of scope

Cancelled by the 2026-10-03 re-scope. Kept below as a record of what was planned, not a to-do list.

| Cancelled | Why |
|---|---|
| M3 network events (`tcp_connect`) | New probe, new event type, contract version bump. No demo value. |
| M3 systemd unit | Running the agent by hand in a terminal is a better demo. |
| M3 benchmarks | Budget stays a stated target in the README; not measured. |
| M4 detection rule engine / MITRE | A whole new layer. Not what the role asks for. |
| M5 React web UI | Replaced by Milestone D's single static page — no framework, no build step, and every line explainable. |
| Replay mode | Unnecessary: the API reads the database, not the agent, so the demo survives the sensor failing. The persistent volume is the fallback. |

---

## ~~Milestone 3 — Extended event types + benchmarks~~ ❌ CANCELLED 2026-10-03

**Goal:** prove breadth (network events flow through the same pipeline) and credibility (measured overhead numbers in the README).

**Context to load:**
> Read `CLAUDE.md`, `ROADMAP.md`, and `KNOWLEDGE_BASE.md`. We are working on Milestone 3 — the final v1 milestone. Milestone 2 is complete: events are persisted to PostgreSQL. Now we add network events and measure performance. Follow `CLAUDE.md` §7.

### Sub-tasks

- [ ] Add `tcp_connect` kprobe/tracepoint hook to the sensor as a new event type via the §6.2 contract (no reshaping of the pipeline — adding a hook should be additive)
- [ ] Update event struct and DB schema for network events; migration file
- [ ] Verify network events flow end-to-end via manual QA (`curl example.com` produces a row)
- [ ] Create `benchmarks/` directory with measurement scripts:
  - [ ] `bench-cpu-idle.sh` — measure agent CPU over 60s idle
  - [ ] `bench-cpu-busy.sh` — measure agent CPU under a `stress-ng` workload
  - [ ] `bench-ram.sh` — read `/proc/<pid>/status` for RSS
  - [ ] `bench-latency.sh` — timestamp-in-kernel vs timestamp-in-DB, report p50/p95/p99
  - [ ] `bench-throughput.sh` — `for i in {1..10000}; do /bin/true; done`, verify no drops
  - [ ] `bench-startup.sh` — `systemd-analyze` on the agent's unit
- [ ] Write `systemd/argus.service` unit file; document install path in README
- [ ] Fill the measured column in `CLAUDE.md` §10 performance budget table
- [ ] Update README with the measured numbers

**Done when:** network + process events flow through the same pipeline, all six performance budget rows have real measured numbers, systemd unit works from boot. **★ v1 shippable.**

---

## ~~Milestone 4 — Detection rule engine~~ ❌ CANCELLED 2026-10-03

**Goal:** turn telemetry into MITRE-tagged alerts.

**Context to load:**
> Read `CLAUDE.md`, `ROADMAP.md`, and `KNOWLEDGE_BASE.md`. We are working on Milestone 4 (optional). v1 (M1-M3) is complete. Now we add rule-based detection. First open question to resolve: build a custom rule format, or adopt Sigma? Discuss trade-offs before implementing. Follow `CLAUDE.md` §7.

### Sub-tasks

- [ ] **Decision:** custom rule format vs Sigma — document the choice in `docs/decisions.md` with rationale
- [ ] Create `detection/` package: rule loader, event matcher, alert emitter
- [ ] Write 5–8 initial rules covering these MITRE techniques (start here — adjust based on what actually fires cleanly):
  - [ ] T1059 — shell spawning another shell
  - [ ] T1071 — process from `/tmp` opening outbound network connection
  - [ ] T1548 — setuid transition on an unexpected binary
  - [ ] T1053 — cron/systemd persistence write
  - [ ] T1105 — curl/wget followed by execve of the downloaded file
- [ ] Alert output: log file + structured JSON stream to stdout (UI-agnostic)
- [ ] Install Atomic Red Team on the VM
- [ ] For each rule, run the matching Atomic test, verify the alert fires
- [ ] Document detection coverage in `docs/detection-coverage.md`: which MITRE techniques are covered, which Atomic tests pass

**Done when:** running the mapped Atomic Red Team tests triggers correctly-tagged alerts for each of the implemented rules.

---

## ~~Milestone 5 — Web UI~~ ❌ CANCELLED 2026-10-03 — superseded by Milestone D

**Goal:** a visual demo asset for the README GIF.

**Context to load:**
> Read `CLAUDE.md`, `ROADMAP.md`, and `KNOWLEDGE_BASE.md`. We are working on Milestone 5 (optional). Now we add a React UI over the M2 datastore. Follow `CLAUDE.md` §7.

### Sub-tasks

- [ ] Add a minimal HTTP API to the agent (or a separate `api/` binary): endpoints for event timeline, alert list, drill-down
- [ ] Scaffold a React app in `ui/` (Vite + TypeScript)
- [ ] Timeline view: events on a scrollable time axis, filter by PID/process name
- [ ] Alerts view: table of alerts with MITRE tags, click to drill down to the events that triggered
- [ ] Docker Compose integration: UI served as a static bundle behind the API
- [ ] Record the demo GIF (30–60s): spawn a process → see it appear → run an Atomic test → see the alert
- [ ] Embed the GIF in the README

**Done when:** the UI renders live event data from the DB, alerts appear as they fire, and the recorded GIF is embedded in the README.

---

## ~~Priority order if time compresses~~ — superseded by the re-scope

Sensor working (M1) > persistence working (M2) > events queryable (part of M2) > benchmarks (M3) > everything else.

If a milestone is falling behind schedule, drop the *next* optional milestone rather than cutting corners on the current one. A shipped M2 with a great README beats a half-done M4.