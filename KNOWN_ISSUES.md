# Known Issues & Risks — Argus

> Running list of things that work today but carry a real risk of breaking later, discovered while building. Not bugs to fix immediately — things to remember before they bite.

## 1. Dev VM (arm64) vs CI runner (x86_64)

This VM is arm64 (UTM on Apple Silicon). GitHub Actions' `ubuntu-24.04` runners are x86_64. The `clang -target bpf` compile itself is architecture-agnostic (eBPF bytecode isn't tied to host arch), but anything that shells out to `uname -m`, reads `/usr/include/<arch>-linux-gnu/...`, or generates arch-specific `vmlinux.h` later will behave differently on the two machines.

**Where it already matters:** the CI fix in item 2 below uses `$(uname -m)` specifically so it resolves correctly on *both* arm64 (here) and x86_64 (CI) — but any new command copied from local testing needs the same care, not a hardcoded path.

## 2. `clang -target bpf` doesn't auto-find `asm/types.h`

`bpf/sensor.bpf.c` includes `<linux/types.h>`, which itself includes `<asm/types.h>`. On Debian/Ubuntu, `asm/types.h` doesn't live at `/usr/include/asm/` — it's under the multiarch path (`/usr/include/aarch64-linux-gnu/asm/` here, `/usr/include/x86_64-linux-gnu/asm/` on CI). Normal native compiles find it automatically; `clang -target bpf` does not, because `bpf` isn't a real host triple with multiarch mapping.

**Fix applied:** added `-I/usr/include/$(uname -m)-linux-gnu` to the compile command in both this session's local testing and `.github/workflows/ci.yml`. Without this flag the build fails immediately with `fatal error: 'asm/types.h' file not found` — confirmed by actually running it, not just reading the header.

**Why it'll bite later:** if a `Makefile` gets added (a likely next step once there's more than one `.bpf.c` file), or if any new build command is copy-pasted from a tutorial/blog post, it's easy to drop this flag and silently reintroduce the failure. Any new eBPF compile invocation must carry this `-I` flag.

## 3. Toolchain version drift: local clang 21 vs CI's clang

This VM has `clang 21.1.8`. The `ubuntu-24.04` GitHub-hosted runner image ships an older clang (historically in the 14–18 range depending on image refresh). A plain `bpf_printk` hello-world is far too simple to expose any version-specific behavior, so this is low risk *today*. It stops being low risk once real CO-RE relocations, BTF-typed map definitions, or newer clang-only BPF features show up — something could compile locally and fail (or vice versa) in CI purely due to clang version, not code.

**Watch for:** if a future compile passes locally but fails in CI (or the reverse) with no obvious code cause, check `clang --version` on both sides before assuming it's a logic bug.

## 4. `PROJECT_CONTEXT.md` documents a stale OS/kernel version

`PROJECT_CONTEXT.md` states "Ubuntu 24.04 LTS, Kernel 6.8." This VM is actually running Ubuntu 26.04 ("Resolute Raccoon") on kernel `7.0.0-29-generic`. BTF/CO-RE support is confirmed present either way (`/sys/kernel/btf/vmlinux` exists), so nothing is broken — but the doc is now inaccurate as a reference for "what am I actually running," which matters since it's the canonical handoff doc.

**Suggested fix (not done yet):** update the Tech Stack table in `PROJECT_CONTEXT.md` to reflect the real VM version, or explicitly note the VM has since been upgraded past the original scoping assumption.

## 5. Go module `go` directive must be manually kept in sync with CI

This VM has Go 1.26.0 installed; `go.mod` is pinned to `go 1.23` to match `ci.yml`'s `actions/setup-go` version. This pin was set deliberately (`go mod edit -go=1.23`) — nothing enforces it automatically going forward. Running `go get` or `go mod tidy` later on this machine could silently bump the `go` directive back up toward 1.26 if a dependency requires a newer version, creating a mismatch with CI's pinned `1.23` that shows up as a confusing CI failure far from its actual cause.

**Watch for:** after any `go get`/`go mod tidy`, diff `go.mod`'s `go` line against `ci.yml`'s `go-version` before pushing.

## 6. Build artifacts almost got committed once already

`go build ./...` produces a binary literally named `argus` at the repo root (matches the module name) — it was untracked and *not* covered by the original `.gitignore` until this session added `/argus` explicitly. Same story for `bpf/*.o`. Both are gitignored now, but it's a reminder: any new build output location (e.g. a future `Makefile`'s `bin/` or `dist/` dir) needs its own `.gitignore` entry added proactively, not after it shows up in `git status`.

## 7. `linux-headers-$(uname -r)` caused CI to hang ~7 minutes — removed, wasn't needed

The original "Install eBPF build dependencies" CI step installed `linux-headers-$(uname -r)` alongside `clang`/`llvm`/`libbpf-dev`. On the GitHub-hosted `ubuntu-24.04` runner this step stalled for 7+ minutes. Root cause: hosted runners run a custom (non-stock) kernel build, so apt has to resolve a headers package tied to that exact kernel version string — a package that's often poorly mirrored or slow for apt to resolve, unlike the always-available `clang`/`llvm`/`libbpf-dev`.

**Fix applied:** removed `linux-headers-$(uname -r)` from `ci.yml` entirely (also added `--no-install-recommends` and `-y` on `apt-get update` as minor, safe cleanup — neither was the actual fix). This is safe because it was never actually load-bearing: `sensor.bpf.c`'s real missing dependency (item 2 above) was glibc's multiarch `asm/types.h`, not anything from the kernel headers package. Pure CO-RE eBPF (BTF + `vmlinux.h`, tracepoints/`bpf_printk`) doesn't need kernel headers the way traditional kprobe-based eBPF or kernel modules do.

**Watch for:** if a future `.bpf.c` genuinely needs real kernel struct definitions beyond what `vmlinux.h`/BTF provides, re-adding kernel headers should be a deliberate, justified choice — not a default copy-pasted from a non-CO-RE tutorial.
