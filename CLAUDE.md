# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

Argus is a lightweight EDR (Endpoint Detection & Response) proof-of-concept for Linux: an eBPF sensor observes process lifecycle events (execve/exit/setuid) at the kernel level, and a Go userspace agent will read those events and persist them for later rule-based threat detection. It's a portfolio artifact (not a production system) built by a CS student learning Go and eBPF — see `PROJECT_CONTEXT.md` for the full scope/roadmap/tech-stack writeup and `KNOWN_ISSUES.md` for a running list of build gotchas.

**Current state (early Milestone 1):** `bpf/sensor.bpf.c` is a hello-world tracepoint hook on `sys_enter_execve` that only calls `bpf_printk`. `main.go` is an empty stub. There is no ring buffer/map, no Go-side BPF loader, and no persistence layer yet — the CI pipeline currently only compiles the C side and builds/vets/tests the (empty) Go side.

## Commands

There is no Makefile; these are the exact commands CI runs (`.github/workflows/ci.yml`), and the same ones to use locally on Linux with `clang`/`llvm`/`libbpf-dev` installed:

```bash
# Compile all eBPF programs (must run from repo root; requires clang, llvm, libbpf-dev)
for f in bpf/*.bpf.c; do
  clang -O2 -g -target bpf -I/usr/include/$(uname -m)-linux-gnu -c "$f" -o "${f%.c}.o"
done

go vet ./...
go build ./...
go test ./...          # run a single test: go test ./... -run TestName
```

`bpf/*.o` and the built `argus` binary are gitignored — don't commit them.

## Architecture

- **Kernel space** (`bpf/*.bpf.c`): eBPF C programs, compiled with `clang -target bpf` (CO-RE style, no bpf2go/generated bindings yet). Each hooks a syscall tracepoint (currently just `sys_enter_execve`). Planned: exit and setuid hooks, and pushing events out via a ring buffer/map instead of `bpf_printk`.
- **User space** (`main.go`): intended to load the compiled BPF object with `cilium/ebpf` (named in the tech-stack plan but **not yet added to `go.mod`** — `go.mod` currently has zero dependencies), read the ring buffer, and write events to PostgreSQL + TimescaleDB. None of this exists yet — `main.go` is an empty stub.
- **Planned deployment**: whole stack (agent + Postgres/TimescaleDB) inside one Ubuntu Linux VM via Docker Compose (`--privileged` for eBPF access); no Docker/Compose files exist yet.
- **Roadmap** (see `PROJECT_CONTEXT.md` for detail): M1 eBPF sensor + Go loader printing to stdout (in progress) → M2 Postgres persistence → M3 extended events (e.g. tcp_connect) + benchmarks → M4 detection rule engine (maybe) → M5 web UI (maybe).

## Build gotchas (from KNOWN_ISSUES.md)

- `clang -target bpf` fails with `fatal error: 'asm/types.h' file not found` unless `-I/usr/include/$(uname -m)-linux-gnu` is passed — this is a multiarch header path issue, not missing kernel headers.
- Do **not** add `linux-headers-$(uname -r)` to CI apt installs — it previously hung the GitHub-hosted runner for 7+ minutes and isn't needed for this CO-RE/BTF-based approach (tracepoints + `bpf_printk`).
- `go.mod`'s `go 1.23` directive is deliberately pinned to match `ci.yml`'s `actions/setup-go` version. Running `go get`/`go mod tidy` can silently bump it out of sync — keep both aligned if you touch either.
- Dev VM is arm64 (UTM/Apple Silicon), CI runner is x86_64. eBPF bytecode is arch-agnostic, but anything using `uname -m` or arch-specific header paths needs to work on both.

## Working with Rom

- Rom is strong in C and systems programming, comfortable in Node/TypeScript, but new to Go and eBPF, and not math-heavy — explain new Go/eBPF concepts plainly, skip math-heavy framing.
- Prefer concise, dialogue-oriented responses that leave room for follow-up, over long upfront explanations.
- Push back on scope creep or premature optimization — this is a portfolio project on a ~1 month timeline, not a production system. Rom has flagged a tendency toward "blank-page paralysis dressed as thoroughness"; call it out if a request seems headed there.
