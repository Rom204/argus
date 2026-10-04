# Argus

**Process telemetry for Linux, from the kernel up.** An eBPF sensor watches every process that
starts and stops on a host, a Go agent decodes those events and batches them into PostgreSQL, and a
small REST API serves them to a browser.

Built as a learning project to understand how production endpoint agents — CrowdStrike, Falco,
Tetragon — actually get data out of the kernel. It is a portfolio artifact, not a product.

<!-- TODO: record a 30-60s terminal+browser GIF of the demo and embed it here. It is the single
     highest-impact thing that could be added to this page. -->

---

## What it does

- Hooks kernel tracepoints with **eBPF** to observe process lifecycle: exec, exit, and credential
  changes (`setuid`/`setgid` family, plus a `commit_creds` kprobe that also catches `capset()` and
  file capabilities).
- Passes events to user space through a **BPF ring buffer** — shared memory, no syscall per event.
- Decodes them in Go against a **fixed, versioned 56-byte struct** that is the only contract between
  kernel and user space.
- Filters kernel threads and collapses redundant credential events.
- Batches rows into **PostgreSQL** with `COPY`, under back-pressure that protects the host rather
  than the telemetry.
- Serves them over a **REST API** as JSON, and renders them in a live-refreshing table.

## What it does not do

Scope was cut deliberately; these are decisions, not omissions.

- **No response actions.** Observation only — it never kills a process or blocks a connection.
- **No detection rules, no alerting, no ML.** The schema is built so rules *could* sit on top.
- **No network or file events.** Process lifecycle only.
- **Linux only**, single host, no multi-tenancy, no cloud deployment. An endpoint agent runs on the
  endpoint; a cloud demo would misrepresent the model.
- **No authentication.** The API binds a local port on a VM and the database is bound to loopback.

---

## Architecture

Three processes, split along a privilege boundary: only the collector needs root.

```
 ┌─ kernel space ─────────────────────────────────────────┐
 │  sched_process_exec / sched_process_exit tracepoints   │
 │  6 × sys_exit_set*id  ·  kprobe/commit_creds           │
 │        │                                               │
 │        └─> struct process_event (56 bytes, versioned)  │
 │              └─> BPF ring buffer (256 KB)              │
 └────────────────────────┬───────────────────────────────┘
                          │  mmap'd memory — no syscall per event
 ┌─ argus  (root) ────────┴───────────────────────────────┐
 │  load + attach probes   →  decode  →  filter           │
 │  →  batch 500 rows / 100 ms  →  COPY                   │
 └────────────────────────┬───────────────────────────────┘
                          │  TCP 127.0.0.1:5432
                   ┌──────┴───────────────┐
                   │  PostgreSQL 17       │   events     (hypertable)
                   │  + TimescaleDB       │   processes  (view)
                   └──────┬───────────────┘
                          │  read-only queries
 ┌─ argus-api  (no root) ─┴───────────────────────────────┐
 │  net/http  →  JSON  ·  serves the embedded page        │
 └────────────────────────┬───────────────────────────────┘
                          │  HTTP :8080
                   ┌──────┴───────┐
                   │   browser    │   fetch() → table, refreshed every 2 s
                   └──────────────┘
```

The agent only writes; the API only reads. Nothing downstream of the ring buffer knows that eBPF
exists — which is why the UI keeps working if the sensor is stopped.

---

## Tech stack

| Component | Choice | Why this one |
|---|---|---|
| Kernel probes | eBPF (C), CO-RE | Event-driven rather than polling: a process that lives 5 ms is invisible to anything that samples `/proc`. CO-RE via BTF means no kernel headers at build time. |
| Verification | Kernel BPF verifier | Statically proves the programs terminate and stay in bounds *before* load. This is why the probes have no loops. |
| User-space agent | Go + `cilium/ebpf` | Best library for loading BPF from user space; static binary; the language most of this industry's tooling is written in. |
| Kernel↔user transport | BPF ring buffer | Shared memory, so no syscall and no copy per event. Bounded at 256 KB on purpose: a stalled reader makes the kernel drop events instead of growing kernel memory. |
| Database | PostgreSQL 17 | The data is genuinely relational and the interesting questions are joins and aggregates. |
| Time-series | TimescaleDB extension | An *extension*, not a different database, so the SQL stays ordinary SQL. `events` is a hypertable, so it partitions by time and old chunks drop cheaply. At demo scale plain Postgres would do; the design targets months of telemetry. |
| DB driver | `jackc/pgx` | Native `COPY` support, which is the fastest bulk-insert path Postgres offers. |
| API | Go standard library `net/http` | No framework needed for two routes, and nothing to explain that isn't in the standard library. |
| UI | One static HTML file | No framework, no build step, no CDN — the demo has to run with no network, and every line should be explainable. |
| Local deploy | Docker Compose | One file to get a reproducible Postgres with the schema already applied. |
| CI | GitHub Actions | Compiles the eBPF, then `vet`, `build` and `test` against a real TimescaleDB service container on every push. |

Three direct Go dependencies: `cilium/ebpf`, `jackc/pgx`, `golang.org/x/sys`.

---

## Quickstart

**Requires Linux** with **kernel 6.3 or newer** and BTF enabled (`/sys/kernel/btf/vmlinux` must
exist), plus `clang`, Go 1.27+, and Docker. Developed and run on Ubuntu 24.04 ARM64, kernel 6.8.

Why 6.3 specifically: the ring buffer needs 5.8, but the capability probe reads
`cred->cap_effective.val`, and `kernel_cap_t` only became a plain `u64` in 6.3 — before that it was
`u32 cap[2]` and the field does not exist under that name.

```bash
git clone git@github.com:Rom204/argus.git && cd argus

# 1. Compile the eBPF programs. Both flags are load-bearing — see CLAUDE.md §9.
#    -D__TARGET_ARCH_arm64 is hard-coded, not derived from `uname -m`, because
#    bpf/vmlinux.h is a committed ARM64 BTF dump. On x86_64, regenerate it first:
#      bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/vmlinux.h
#    and change the flag to -D__TARGET_ARCH_x86.
for f in bpf/*.bpf.c; do
  clang -O2 -g -target bpf -D__TARGET_ARCH_arm64 \
    -I/usr/include/$(uname -m)-linux-gnu -c "$f" -o "${f%.c}.o"
done

# 2. Build both binaries. Note the -o: `go build ./...` discards executables
#    when its pattern matches more than one package.
go build -o argus .
go build -o argus-api ./cmd/argus-api

# 3. Start the event store (the schema is applied on first start).
docker compose up -d

# 4. Collector — needs root, because loading BPF programs is privileged.
sudo ./argus

# 5. API + UI — in another terminal. No root.
./argus-api
```

Open **http://localhost:8080**. Run a few commands in a third terminal and they appear in the table
within two seconds.

> On a VM, opening the page from the host needs the port allowed through the guest firewall
> (`sudo ufw allow 8080/tcp`), or an SSH tunnel.

### API

```
GET /api/events?limit=100&type=EXECVE
```

`limit` is 1–1000 (default 100). `type` is one of `EXECVE`, `EXIT`, `SETUID`, `CAPS`, matched
against an allow-list — an unrecognised value is rejected, not sanitised.

```json
{
  "count": 1,
  "events": [
    {
      "time": "2026-10-04T15:02:11.482Z",
      "type": "EXECVE",
      "pid": 4821, "ppid": 4820,
      "uid": 1000, "gid": 1000,
      "comm": "ls",
      "caps": "0x0"
    }
  ]
}
```

---

## Design notes

The three decisions worth the most explanation.

<details>
<summary><b>One versioned struct is the whole kernel↔user-space contract</b></summary>

`bpf/event.h` defines a 56-byte struct with fixed-width fields ordered widest-first, so there is no
padding anywhere. Go reads those bytes positionally at documented offsets, and the schema has one
column per field.

There is no serialisation format, because a BPF program has no allocator, no libraries, and a hard
instruction budget. The cost of that is that three places must agree on the layout by hand, so the
header carries `_Static_assert`s on its own size and field offsets — change a field and the **build
breaks** with an explanatory message, rather than silently producing convincing nonsense. Every
event also carries a version number, which the decoder rejects if it does not match.

New event types are new enum values and new fields are appended, never inserted, so existing byte
offsets stay stable.
</details>

<details>
<summary><b>Back-pressure lands on telemetry, not on the host</b></summary>

Events are batched: 500 rows or 100 ms, whichever comes first, inserted with `COPY` in one round
trip. The queue between the ring-buffer reader and the writer is bounded at 8192 rows.

When that queue fills, the enqueue blocks, the reader stops draining the ring buffer, and the kernel
begins dropping events. That ordering is the point — if the database is slow, the monitoring
degrades and the machine being monitored is untouched. For an agent that runs on someone else's
production host, that is the correct priority.
</details>

<details>
<summary><b>Raw events are append-only; derived shapes are views</b></summary>

`events` is one append-only table, one row per event, with no foreign keys and no primary key. A new
event type is a new `type` value rather than a migration.

The schema began as normalised `processes` + `process_events` tables with a foreign key, and testing
killed it: events routinely arrive for processes that were never observed starting — `fork()` without
`exec`, and anything already running when the agent started. A strict foreign key needs placeholder
rows and upsert logic for those.

So "a process" is a **view** instead. `processes` pairs each `EXECVE` with the first `EXIT` for the
same pid *at or after it*, using `LEFT JOIN LATERAL`. Matching forward in time rather than grouping
by pid matters because Linux recycles pids — grouping would merge two unrelated processes that
happened to reuse a number. `exited_at` is `NULL` while the process is still alive.

See [`docs/example-queries.sql`](docs/example-queries.sql) for five queries over this, including a
recursive process tree.
</details>

---

## Layout

```
bpf/        eBPF programs (C) + the event contract + a committed BTF type dump
event/      decoding raw ring-buffer bytes; monotonic → wall-clock conversion
sensor/     loading and attaching probes, the read loop, filtering
storage/    row mapping, batching writer, pgx pool, the SQL migration
api/        REST handlers and query-parameter validation
web/        the single-page UI, embedded into the binary
cmd/        argus-api (the agent's main lives at the repo root)
docs/       example queries, demo runbook
```

## Tests

```bash
go test ./...                 # unit tests; the database tests skip

# everything, including the storage integration tests
ARGUS_TEST_DSN='postgres://argus:argus@127.0.0.1:5432/argus?sslmode=disable' go test ./...
```

34 test functions, many table-driven. The shape is deliberate: heavy on logic, near zero on wiring.

- **Decoding, filtering, credential dedup, query validation** — unit tested, no I/O. The piece that
  decides *how* to attach a probe is a pure function precisely so it can be tested without root.
- **HTTP handlers** — tested against a hand-written in-memory store, so they need no database.
- **Storage** — integration tested against a real containerised Postgres, each test in its own
  throwaway schema with the real migration file applied, so the tests cannot drift from production
  DDL.
- **Kernel code** — not unit-testable. Covered by manual end-to-end QA, and by the compile-time
  layout assertions on the contract.

---

## Known limitations

Written down rather than hidden.

- **`fork()` without `exec` produces no creation event**, so such a process appears only at exit.
  The fix is additive (a `sched_process_fork` probe and a new type value); shipping the pipeline
  end to end came first.
- **A failed database write is logged and dropped, not retried.** Retrying needs bounded local
  buffering plus a policy for a database that stays down — the right next piece of work, and
  deliberately not built badly.
- **`comm` is the kernel's 16-byte task name, not a full path or argv.** It is what the kernel
  provides for free; capturing the executable path and arguments is more work in the probe.
- **The performance budget is a target, not a measurement.** The intended envelope is <1% CPU idle,
  <100 MB resident, <500 ms kernel→database, 1000 events/sec without drops. Benchmarking was cut
  from scope, so no figures are claimed here.
- **CI proves the code compiles, not that it runs.** `bpf/vmlinux.h` is an ARM64 BTF dump and the
  runner is x86_64, so the probes are compiled as ARM64 there and only executed on the dev VM.

## Further reading

| File | Contents |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | The spec: locked decisions, engineering principles, the architecture contract |
| [`ROADMAP.md`](ROADMAP.md) | Milestones, what was completed, and what was cancelled with reasons |
| [`KNOWLEDGE_BASE.md`](KNOWLEDGE_BASE.md) | Engineering ledger — every problem hit, its root cause, and the fix |
| [`docs/DEMO.md`](docs/DEMO.md) | Operational runbook for running the whole thing live |

## License

MIT — see [LICENSE](LICENSE).
