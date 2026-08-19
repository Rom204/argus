# Project Context — Argus

> Handoff document for Claude Code continuation. This captures every decision made during the scoping phase.

## Project Identity

- **Name:** Argus
- **Tagline:** Lightweight EDR for Linux — process observability via eBPF
- **Owner:** Rom, 2nd-year CS student at Reichman University
- **Purpose:** Portfolio artifact for cybersecurity roles
- **Timeline:** ~1 month

## Vision

An Endpoint Detection & Response (EDR) agent that runs on Linux hosts, uses eBPF to observe user-space process lifecycle at the kernel level, streams telemetry to a local queryable datastore, and provides the foundation for behavioral threat detection based on MITRE ATT&CK patterns.

Positioned as a learning-oriented implementation of the same core pattern used by CrowdStrike, SentinelOne, Falco, and Tetragon.

## Scope

### In Scope (v1)

- Full lifecycle tracing of every user-space process: creation, identity/permission changes, termination
- Filtering out kernel threads (PPID == 2)
- Persistence to local PostgreSQL + TimescaleDB with a schema designed for downstream analytics
- Deployment as a systemd service from boot
- Packaging via Docker Compose for reproducible local deploy
- Runs inside a single Ubuntu 24.04 VM on Rom's Mac (via UTM)

### Out of Scope (v1)

- No response actions (process kill, network block) — detection only
- No machine learning — rule-based detection only, and even rules are deferred to a later milestone
- No Windows or macOS support
- No cloud deployment — strictly local
- No multi-endpoint / multi-tenant support in v1 (architecture should not preclude it, but not a v1 feature)
- No file event capture in early milestones (volume too high, added later with sampling)
- No network event capture until Milestone 3

## Event Detail Level ("Medium" scope)

Per-process events captured:

- Process creation (execve / fork)
- Process termination (exit)
- Identity changes during process lifetime (setuid, setgid, capabilities changes)

Not captured in early milestones:

- File open/read/write events (deferred — volume issue)
- Network connections (added in Milestone 3)
- Full system call trace (never — infeasible volume)

## Tech Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Kernel probes | eBPF (C) | Industry standard; leverages Rom's C strength |
| User-space agent | Go + cilium/ebpf library | Best library ecosystem; matches cloud-native security industry; static binary deploy |
| Storage | PostgreSQL + TimescaleDB | Relational schema for analytics + time-series optimization |
| Orchestration | Docker Compose | Reproducible local deploy; requires --privileged for eBPF (standard, like Falco) |
| Base OS | Ubuntu 24.04 LTS | Kernel 6.8, full BTF/CO-RE support, industry-standard |
| CI/CD | GitHub Actions | Build + vet + test on every push |

## Architecture (High-Level)

Kernel space:
- eBPF programs (sensor.bpf.c) hook execve/exit/setuid tracepoints
- Events flow via ring buffer / eBPF maps

User space, inside VM:
- Go agent (main.go) reads events from ring buffer
- Writes to PostgreSQL + TimescaleDB (running in Docker Compose)

All components run inside the Ubuntu VM. Development happens on Mac via VS Code + Remote-SSH into the VM.

## Data Schema (Deferred)

Deliberately deferred until Milestone 2, after skeleton code exists. Initial approach: normalized schema with a `processes` table (one row per process instance) and a `process_events` table (identity changes, etc., FK to processes). Final schema will be shaped by real-world usage during the sensor build.

## Performance Budget (Soft targets)

| Metric | Target |
|--------|--------|
| CPU (idle host) | < 1% |
| CPU (busy host) | < 5% average |
| RAM (resident) | < 100 MB |
| Event latency (kernel → DB) | < 500 ms |
| Event throughput | 1,000 events/sec without drops |
| Startup time | < 2 seconds |

Soft budget policy: targets stated in README, current measured values documented, deviations acknowledged. No hard enforcement in v1.

Measurement plumbing to be added in Milestone 3 (`benchmarks/` directory).

## Distribution Strategy

- **Primary target audience:** recruiters and hiring engineers reading the README
- **Secondary:** engineers who actually run the code (~5% of readers)

README-first strategy:

- Architecture diagram
- 30-60 second demo GIF (highest-impact single asset)
- Clear "What It Does" and "What It Does Not Do" sections
- Tech stack table with rationale column
- Architecture Decision Records in `docs/decisions.md`

Deployment: `docker compose up` inside a Linux VM. Bonus later: Vagrantfile for one-command VM creation.

**No cloud deployment** — deliberate. EDR agents run on the customer's host; cloud demo undermines the mental model. Local-first is the right choice here.

## Development Environment

- **Editor:** VS Code on Mac
- **Runtime:** Ubuntu 24.04 VM on Mac (UTM)
- **Connection:** VS Code Remote-SSH extension → code lives on the VM, edited from Mac
- **Terminal:** VS Code integrated terminal (VM shell)

## Roadmap (Milestones — dates TBD)

- **M1: eBPF sensor tracing process lifecycle** — sensor.bpf.c hooks execve/exit/setuid; Go loader reads ring buffer, prints to stdout
- **M2: Persistence layer** — PostgreSQL schema design; Go agent writes events to DB; basic query capability
- **M3: Extended event types + benchmarks** — add network events (tcp_connect); add performance measurement scripts
- **M4: Detection rule engine (or defer)** — decide based on remaining time; rule-based, MITRE-tagged
- **M5: Web UI (or defer)** — event timeline, alerts; React-based

Priority order if compressed: sensor working > persistence working > events queryable > everything else.

## Testing Strategy

- Unit tests for Go code (small scope)
- **Atomic Red Team** (Red Canary's open-source library of MITRE-mapped attack simulations) for end-to-end validation once detection rules exist
- Manual `for i in {1..10000}; do /bin/true; done` for throughput testing

## Open Questions for Later

1. Detection engine: build custom or adopt Sigma rule format?
2. Multi-endpoint support: worth adding before job search wraps up?
3. Web UI vs CLI-only for v1 demo?

## Guidance for Claude Code

- **Do not skip skill files.** Always view relevant SKILL.md files before creating or editing code.
- **Rom's technical background:** strong in C and systems (top grades in Data Structures and Systems Programming in C); comfortable in Node.js/TypeScript from prior full-stack work; **new to Go and eBPF**; **not strong in math** — avoid math-heavy explanations.
- **Communication style:** concise, analogy-friendly, dialogue-oriented. Prefer short responses that leave room for follow-up questions.
- **When Rom asks a conceptual question, explain terms plainly** (assume OS fundamentals but not much else).
- **Push back honestly** on scope creep or premature optimization. Rom has a known pattern of blank-page paralysis dressed as thoroughness — call it out if it appears.
- **Anchor to the goal:** this is a portfolio artifact to open doors at cybersecurity companies, not a production system.

## Current Status

- [x] Scoping complete
- [x] Tech stack locked
- [x] Performance budget set
- [x] CI/CD config drafted (`.github/workflows/ci.yml`)
- [ ] GitHub repo created, VS Code + Claude Code extension being set up
- [ ] Ubuntu VM setup verified (kernel >= 5.8, eBPF support)
- [ ] Next: Milestone 1 kickoff — Go module skeleton, first eBPF hello-world