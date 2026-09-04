# Knowledge Base — Argus

> Engineering ledger of infrastructure and code issues encountered while building Argus. Format: **Issue → Root Cause → Resolution**, plus watch-for notes on things that work today but carry a real risk of breaking later. Concepts learned along the way are in the appendix at the end.
>
> For the project spec, milestones, and working process see `CLAUDE.md`. For per-milestone delivery notes see `WALKTHROUGH.md`.

---

## Environment setup

### Issue: VS Code refused to connect to the VM over Remote-SSH (`ssh child died`)

- **Root Cause:** standard Ubuntu Desktop installations do not include an active SSH server by default — there was nothing listening to connect to. The door has to be opened from *inside* the VM before the extension can come in.
- **Resolution:** installed `openssh-server` on the VM and enabled the service.

### Issue: `apt` failing validation — downloaded package signatures appeared to come "from the future"

- **Root Cause:** the VM's clock had drifted out of sync with real time. VM-oriented and minimal OS images often ship without non-essential background services such as `systemd-timesyncd` (standard practice for lightweight environments, to conserve host resources), so nothing was keeping the clock corrected. `apt` validates package signature timestamps, and a clock lagging behind makes freshly-signed packages look like they were signed in the future.
- **Resolution:** manually synced the system clock with `date -s`. If this recurs on a long-lived VM, installing/enabling a time-sync service is the durable fix.

### Issue: a `(yes/no)` SSH fingerprint prompt appeared to accept no keystrokes

- **Root Cause:** secure terminal input fields hide typing entirely — no characters, no asterisks. Nothing was actually broken.
- **Resolution:** typed `yes` blind and pressed Enter; the fingerprint verification passed and the password prompt followed.

### Issue: `git init` run in `/home/rom` instead of the project directory

- **Root Cause:** ran `git init` from the home directory, so git began tracking the entire home directory — every hidden system file (`.bashrc` and friends) showed up in the VS Code source control tab.
- **Resolution:** deleted the stray `.git` directory from `/home/rom` (`rm -rf .git`) and cloned the repository properly into `~/projects/argus` instead.

---

## eBPF build pipeline (Milestone 1)

### Issue: `clang -target bpf` fails with `fatal error: 'asm/types.h' file not found`

- **Root Cause:** `bpf/sensor.bpf.c` includes `<linux/types.h>`, which itself includes `<asm/types.h>`. On Debian/Ubuntu, `asm/types.h` does not live at `/usr/include/asm/` — it lives under the multiarch path (`/usr/include/aarch64-linux-gnu/asm/` on the arm64 dev VM, `/usr/include/x86_64-linux-gnu/asm/` on the x86_64 CI runner). Normal native compiles resolve this automatically; `clang -target bpf` does not, because `bpf` isn't a real host triple with multiarch mapping.
- **Resolution:** added `-I/usr/include/$(uname -m)-linux-gnu` to the compile command, in both local testing and `.github/workflows/ci.yml`. Without the flag the build fails immediately — confirmed by actually running it, not just by reading the header.
- **Why it'll bite later:** if a `Makefile` gets added (a likely step once there's more than one `.bpf.c` file), or if a build command is copy-pasted from a tutorial or blog post, this flag is easy to drop and silently reintroduce the failure. **Any new eBPF compile invocation must carry this `-I` flag**, using `$(uname -m)` rather than a hardcoded architecture.

### Issue: the "Install eBPF build dependencies" CI step hung for 7+ minutes

- **Root Cause:** the step installed `linux-headers-$(uname -r)` alongside `clang`/`llvm`/`libbpf-dev`. GitHub-hosted runners run a custom (non-stock) kernel build, so apt has to resolve a headers package tied to that exact kernel version string — a package that is often poorly mirrored and slow for apt to resolve, unlike the always-available `clang`/`llvm`/`libbpf-dev`.
- **Resolution:** removed `linux-headers-$(uname -r)` from `ci.yml` entirely. Also added `--no-install-recommends` and `-y` on `apt-get update` as minor, safe cleanup — neither was the actual fix. This is safe because the package was never load-bearing: the real missing dependency was glibc's multiarch `asm/types.h` (above), not anything from kernel headers. Traditional (non-CO-RE) eBPF and kernel module builds need kernel headers to compile against real kernel struct definitions; CO-RE eBPF using BTF + `vmlinux.h` exists specifically to avoid that dependency.
- **Watch for:** if a future `.bpf.c` genuinely needs kernel struct definitions beyond what `vmlinux.h`/BTF provides, re-adding kernel headers must be a **deliberate, justified choice** — not a default copy-pasted from a non-CO-RE tutorial.

### Watch item: dev VM (arm64) vs. CI runner (x86_64)

- **Root Cause:** the dev VM is arm64 (UTM on Apple Silicon); GitHub Actions' `ubuntu-24.04` runners are x86_64. The `clang -target bpf` compile itself is architecture-agnostic (eBPF bytecode isn't tied to host arch), but anything that shells out to `uname -m`, reads `/usr/include/<arch>-linux-gnu/...`, or generates an arch-specific `vmlinux.h` later will behave differently on the two machines.
- **Where it already matters:** the `asm/types.h` fix above uses `$(uname -m)` specifically so it resolves correctly on *both* machines. Any new command copied from local testing needs the same care — never a hardcoded path.

### Watch item (RESOLVED 2026-09-02): toolchain version drift — local clang vs. CI's clang

- **Root Cause:** the *original* Desktop VM had `clang 21.1.8` installed from a non-apt source, while the `ubuntu-24.04` GitHub-hosted runner installs whatever `apt` offers. Code could compile locally and fail in CI purely due to clang version once real CO-RE relocations or BTF-typed map definitions appeared.
- **Resolution:** on the rebuilt Server VM the toolchain was installed with **plain `apt install clang llvm libbpf-dev`** — the exact command `ci.yml` runs. Both sides are now `clang 18.1.3` / `LLVM 18.1.3` / `libbpf 1.3.0`, so the drift is closed by construction rather than by vigilance.
- **Watch for:** this only holds while the VM installs clang from apt. Installing a newer clang from LLVM's own apt repo or a tarball silently reopens the gap. If a compile ever passes locally but fails in CI with no obvious code cause, check `clang --version` on both sides first.

### Issue: `go.mod`'s `go` directive must be kept in sync with CI manually

- **Root Cause:** Go is not installed from apt (Ubuntu 24.04 only offers 1.22), so the VM's Go version is chosen independently of `ci.yml`'s `actions/setup-go` version. Nothing enforces the two matching. Running `go get` or `go mod tidy` can also bump the directive on its own if a dependency requires a newer version, creating a mismatch that surfaces as a confusing CI failure far from its actual cause.
- **Resolution:** as of 2026-09-02 all three are aligned on **1.27**: `/usr/local/go` is go1.27.1, `go.mod` says `go 1.27`, `ci.yml` says `go-version: '1.27'`.
- **Watch for:** after any `go get`/`go mod tidy`, diff `go.mod`'s `go` line against `ci.yml`'s `go-version` before pushing. This becomes live the moment `cilium/ebpf` is added in M1.9.

### Issue: build artifacts almost got committed

- **Root Cause:** `go build ./...` produces a binary literally named `argus` at the repo root (it matches the module name). It was untracked and *not* covered by the original `.gitignore`. Same story for `bpf/*.o`.
- **Resolution:** added `/argus` and `bpf/*.o` to `.gitignore`. Both are covered now — but any new build output location (e.g. a future `Makefile`'s `bin/` or `dist/` directory) needs its own `.gitignore` entry added proactively, not after it shows up in `git status`.

---

## Desktop → Server VM migration

### Issue: the Claude Code extension stopped working in VS Code Remote-SSH (loopback authentication failure)

- **Root Cause:** undetermined — never definitively diagnosed on the original Ubuntu 24.04 Desktop VM.
- **Resolution:** resolved incidentally after rebuilding the VM from scratch as Ubuntu 24.04.4 LTS Server ARM64. Debugging the extension in place inside a bloated Desktop environment was judged a worse use of time than a clean reinstall on a leaner base (which also better matches how EDR agents actually deploy — headless).
- **Honest status:** if this recurs, the underlying cause is still genuinely unknown. Treat it as "currently moot," not "root-caused."

### Issue: apt dependency conflicts installing Node.js on the Desktop VM

- **Root Cause:** undetermined — not confirmed to be an apt/Node package conflict specifically.
- **Resolution:** also resolved incidentally by the same fresh Ubuntu Server rebuild, rather than by a targeted workaround. It was originally assumed the fix was "bypass apt using NVM," but the Server rebuild is the only variable that actually changed, so NVM should not be recorded as the fix.

### Issue: all VS Code Remote-SSH extensions had to be reinstalled after the VM rebuild

- **Root Cause:** Remote-SSH extensions (Claude Code included) install onto the remote filesystem itself — they are per-host, not synced from the local VS Code config. A rebuilt VM is a new host with no extensions, even though the Mac-side config and SSH host entry look unchanged.
- **Resolution:** reinstalled the remote extensions on the new Server VM.
- **Watch for:** after any future VM rebuild, snapshot restore, or disk swap, expect to reinstall remote extensions *before* assuming a "broken extension" is a code or config bug rather than one that simply isn't installed on this host yet.

### Watch item: documentation drifting from the actual environment

- **Root Cause:** the original project doc stated "Ubuntu 24.04 LTS, Kernel 6.8" while the VM had at one point been upgraded to Ubuntu 26.04 on kernel `7.0.0-29-generic`, and later rebuilt again as 24.04.4 Server on `6.8.0-138-generic`. Nothing broke — BTF/CO-RE support was present throughout (`/sys/kernel/btf/vmlinux` exists) — but the canonical handoff doc became an unreliable answer to "what am I actually running."
- **Resolution:** the environment facts now live in exactly one place, `CLAUDE.md` §3.4, and are corrected whenever the VM changes.
- **Watch for:** OS/kernel versions, IP addresses, and VM resources are the fastest-rotting facts in these docs. Verify with `uname -a` / `hostname -I` rather than trusting the written value when it actually matters.

### Issue: GitHub SSH authentication had to be set up from scratch on the new VM

- **Root Cause:** the repository is cloned onto the VM and every `git` command executes there — not on the Mac. With SSH remotes (rather than HTTPS), GitHub authentication must exist on whichever machine actually runs `git`. The Mac's existing GitHub SSH key never leaves the Mac, so it is useless to the VM.
- **Resolution:** generated a fresh SSH keypair on the VM itself with `ssh-keygen`, and pasted the resulting **public** key into GitHub's SSH keys settings.

### Issue: one `sudo` produced 28 SETUID events

- **Root Cause:** not a bug — `sudo` genuinely makes ~28 successful `set*id` syscalls in a single invocation, repeatedly re-asserting credentials it already holds as it validates, drops, and re-acquires privilege. Argus hooks six of those syscalls (`setuid`, `setgid`, `setreuid`, `setregid`, `setresuid`, `setresgid`), so it faithfully reported all of them. Across the 28 events there were only **4 distinct uid/gid states**; the other 24 changed nothing.
- **Resolution:** deduplicate in the Go reader (`sensor/identity.go`), not in the probes. An `identityTracker` holds each process's last-known uid/gid and drops set\*id events that match it. EXECVE seeds the baseline; EXIT deletes the entry, which both bounds the map to live processes and stops a recycled PID inheriting the previous occupant's credentials. Output for one `sudo` went 28 → 4.
- **Why in user space:** `CLAUDE.md` §6.2 — producers stay dumb, filtering goes wherever it is cheapest to express correctly. Doing this in BPF would need a per-PID hash map, cleanup logic, and verifier-friendly lookups, to save ring buffer traffic that is not currently a bottleneck.
- **Watch for:** a process already running when Argus starts has no recorded baseline, so its *first* identity event is always emitted — it cannot be known to be a no-op. That is deliberate, and covered by a test.

### Issue: half the virtual disk was unallocated after the Ubuntu Server install

- **Root Cause:** the Ubuntu Server installer's guided LVM layout does not give the root logical volume the whole volume group. On a 30GB virtual disk it created a 26.9GB volume group but only a 13.5GB `ubuntu-lv` for `/`, leaving 13.4GB unused but invisible to `df`. This is default installer behaviour, not a misconfiguration — the space is reserved so snapshots or extra volumes remain possible.
- **Resolution:** `sudo lvextend -l +100%FREE /dev/ubuntu-vg/ubuntu-lv` then `sudo resize2fs /dev/ubuntu-vg/ubuntu-lv`. Root went 13.5GB → 27GB online, with the filesystem mounted and no downtime (ext4 supports online *growth*; shrinking would require unmounting and is a genuinely risky operation).
- **Watch for:** done pre-emptively at M1.5 rather than reactively at M2, because Docker images plus a growing PostgreSQL/TimescaleDB volume are what would have filled it. A full root filesystem fails in confusing ways — apt, Docker, and systemd all break with errors that don't mention disk space.

### Issue: the build toolchain was never reinstalled after the VM rebuild — and the roadmap claimed it was

- **Root Cause:** `ROADMAP.md` M0 carried a checked box reading "Toolchain verified: `clang`, `llvm`, `libbpf-dev`, Go, BTF support". That was true — of the *Desktop* VM. Rebuilding as Server produced a machine with none of it, and the checkbox was carried across the migration unverified. `dpkg -l` showed only *libraries* (`libbpf1`, `libllvm18`, `libclang1-18`) pulled in as dependencies of unrelated packages, which is easy to misread as "clang is installed" — the compiler driver itself was absent. This surfaced only when a build was actually attempted at M1.5, several doc-rewrites later.
- **Resolution:** installed `clang llvm libbpf-dev` from apt and Go 1.27.1 from the official tarball into `/usr/local/go`, with PATH set via `/etc/profile.d/go.sh`. Then re-ran every M0/M1 checkbox against the live machine instead of trusting the doc.
- **Watch for:** a checkbox records that something *was* verified, not that it *is* true. After any VM rebuild, snapshot restore, or host migration, re-run the verification rather than migrating the checkmarks — and prefer `command -v clang` / `go version` over reading `dpkg -l`, since installed libraries do not imply installed tools.

---

## Concepts learned (appendix)

**CO-RE (Compile Once – Run Everywhere).** A crucial concept in the eBPF ecosystem: a program compiled once, using BTF type information and `vmlinux.h`, can run across different host kernel versions without being recompiled per kernel. This is why Argus's tracepoint + `bpf_printk` sensor needs neither `linux-headers-$(uname -r)` nor per-kernel rebuilds — and why pulling kernel headers into a CO-RE build is usually a leftover from a copy-pasted non-CO-RE tutorial rather than a real requirement.

**File locks vs. mutexes.** The `apt` lock error (`Could not get lock /var/lib/dpkg/lock-frontend`) is mutual exclusion in the wild. A mutex protects a critical section in RAM against concurrent *threads*; a file lock (via syscalls like `flock`) protects a critical section on *disk* — here, the package database — against concurrent *processes*. Same idea, different medium, both preventing corruption from simultaneous writers.

**Minimal distributions trim background services.** VM-oriented and Server images often ship without non-essential services (e.g. `systemd-timesyncd` for automatic clock sync) to conserve host resources — standard DevOps practice for lightweight environments, but the reason the clock-drift issue above was possible at all.

**Virtualization and hypervisors.** UTM runs a complete guest operating system, with its own kernel, on top of the host OS. That guest kernel is what makes eBPF possible here at all — eBPF is a kernel technology, and macOS's kernel would never do.

**Remote-SSH is a thin client.** VS Code Remote-SSH means the local editor is *only* a user interface: all files, compilation, and terminal commands physically happen on the remote Linux VM. The project files live on the Ubuntu virtual disk, not the Mac — the Mac is a remote control and a display. Corollaries: git commands must run in the VM's terminal, the SSH keys those commands need must live on the VM (see the GitHub auth issue above), and the SSH server must be running inside the VM before the extension can connect at all.

**Linux is not Ubuntu.** Linux is the engine — the kernel. Ubuntu is the complete distribution built around it. eBPF requires the Linux *kernel* specifically; the distribution on top is a convenience, not the dependency.

**Server vs. Desktop is a deployment decision, not just a resource optimization.** Server editions idle at roughly ~500MB RAM versus ~4GB for Desktop, which matters on a 4GB VM. But more importantly, real EDR agents run headless on servers — developing on a headless Server image means the dev environment matches the deployment target. The trade-off is losing the GUI safety net: every fallback path is CLI-based from here on.
