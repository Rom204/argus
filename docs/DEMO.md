# DEMO.md — running Argus in front of someone

Open this in a VS Code tab during the demo. Commands are copy-pasteable. Every step says what you
should see, and what to do when you don't.

---

## 0. Does this need internet? No.

Verified 2026-10-04. Nothing in the demo path touches the network:

| | Why it's safe offline |
|---|---|
| The page | References zero external hosts — no CDN, no fonts, no framework. All CSS and JS are inline. |
| The API and agent | Both connect to `postgres://...@127.0.0.1:5432` — an IP, so no DNS lookup. |
| The database | The `timescale/timescaledb:2.30.1-pg17` image is already on the VM's disk. |
| The binaries | `argus` and `argus-api` are already compiled. |

**The `192.168.64.x` network is not your WiFi.** It's a virtual interface macOS creates for UTM.
Joining a different WiFi, an iPhone hotspot, or no network at all does not affect it.

**So: turn WiFi off before you start.** If it works with WiFi off at home, it works anywhere.

### Never run these on demo day

`docker compose pull` · `apt update` / `apt upgrade` · `go mod download` / `go mod tidy` ·
`git pull` · `docker compose down -v` (that last one wipes the database)

---

## 0.5 The one command that tells you where you are

Paste this any time you're unsure what's up. It answers every "is X running?" question at once.

```bash
cd ~/projects/argus
echo "--- processes ---";  pgrep -a argus || echo "  NEITHER is running"
echo "--- port 8080 ---";  ss -tlnp | grep 8080 || echo "  nothing listening"
echo "--- database ---";   docker compose ps --format '  {{.Service}}: {{.Status}}'
echo "--- data ---";       docker compose exec -T db psql -U argus -d argus -t \
  -c "SELECT count(*) || ' rows, newest ' || (now() - max(time))::text || ' ago' FROM events;"
```

A healthy demo looks like:

```
--- processes ---
428763 ./argus
428960 ./argus-api
--- port 8080 ---
LISTEN 0 4096 *:8080 *:*  users:(("argus-api",pid=428960,fd=7))
--- database ---
  db: Up 3 hours (healthy)
--- data ---
   7262 rows, newest 00:00:01 ago
```

The two numbers that matter: **both processes listed**, and **newest a second or two ago**.

---

## 1. Pre-flight — the night before, and again 15 minutes before

```bash
# On the Mac: turn WiFi OFF. Disable sleep. Then boot the VM in UTM.
```

Then, in one VM terminal:

```bash
cd ~/projects/argus
docker compose ps                 # want: Up (healthy)
ls -l argus argus-api             # want: both exist
docker compose exec -T db psql -U argus -d argus -c "SELECT count(*) FROM events;"
```

Want: a container that is **healthy**, both binaries present, and a non-zero count. A non-zero
count is your safety net — if eBPF fails to load on the day, the API and the page still work,
because they read the database and not the agent.

---

## 2. Startup, in order

Three terminals in VS Code. Do them in this order — each depends on the one before.

### Step 1 — the VM (UTM)

Boot it in UTM and wait for the login prompt in the UTM window.

- **Check:** the UTM window shows `romubuntuvm login:`.
- **If it won't boot:** give it a minute; the first boot after a Mac restart is slow.

### Step 2 — SSH / VS Code

Open VS Code and let Remote-SSH reconnect to `rom@192.168.64.5`.

- **Check:** bottom-left of VS Code says `SSH: 192.168.64.5`, and a terminal opens on the VM.
- **If it can't connect — the VM's IP may have changed.** It's assigned by DHCP, not static. Click
  into the **UTM window**, log in there directly, and run:

  ```bash
  hostname -I
  ```

  Use whatever address that prints in VS Code's Remote-SSH target instead of `.5`.

### Step 3 — the database

```bash
cd ~/projects/argus
docker compose ps
```

- **Check:** `STATUS` reads `Up ... (healthy)`. **Healthy**, not just `Up`.
- **If it's missing or exited:** `docker compose up -d`, then wait ~10 seconds and re-check. The
  container has `restart: unless-stopped`, so it normally comes back by itself after a reboot.
- **If it says `Up` but never `healthy`:** wait another 15 seconds. Postgres is still starting.

Confirm you can actually talk to it:

```bash
docker compose exec -T db psql -U argus -d argus -c "SELECT count(*) FROM events;"
```

- **Check:** a number comes back.

### Step 4 — terminal 1: the agent (needs sudo)

```bash
cd ~/projects/argus
sudo ./argus
```

- **Check:** `argus: probes attached, writing events to the database (Ctrl+C to stop)`, then event
  lines start scrolling.
- **If it says "verifier said:"** — the kernel rejected a probe. Don't debug it live. Ctrl+C,
  **skip this terminal**, and demo from the data already in the database. Say: "the collector
  isn't loading on this kernel right now — here's the telemetry it already shipped." Everything
  else still works.
- **If it says "connect to database"** — go back to Step 3.

**Leave this terminal running.**

### Step 5 — terminal 2: the API (no sudo)

```bash
cd ~/projects/argus
./argus-api
```

- **Check:** `argus-api: listening on http://0.0.0.0:8080 (Ctrl+C to stop)`.
- **If it says "address already in use":** an old copy is still running. `pkill argus-api`, then
  start it again.
- **Worth saying out loud:** "no sudo here — only the collector needs root."

**Leave this terminal running.**

### Step 6 — the browser, on the Mac

Open **http://localhost:8080** in Chrome.

This works because VS Code Remote-SSH automatically forwards the port over the SSH connection. You
can see it in VS Code's **PORTS** tab, next to TERMINAL.

- **Check:** a dark table with rows, and the status text reads `100 events · updated HH:MM:SS`.
- **If the page is blank or won't connect:**
  1. Check VS Code's **PORTS** tab lists 8080. If not, add it there manually (Forward a Port → 8080).
  2. Confirm the server is alive from inside the VM: `curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/` — want `200`.
  3. Fall back to the firewall route below.

**Firewall route** (works with VS Code closed — needed if you ever demo from Warp alone):

```bash
sudo ufw allow 8080/tcp comment 'argus api'
```

then open **http://192.168.64.5:8080** instead.

### Step 7 — terminal 3: make something happen

```bash
whoami
ls /tmp
sudo id          # do this one — see below
```

- **Check:** each command shows up in the browser table within ~2 seconds, at the top.
- **If the table doesn't change:** the status line tells you which half is broken — "cannot reach
  the API" means terminal 2 died; a growing count with no new rows means terminal 1 died.

**Run `sudo id` early, and here's why it matters.** The agent cannot see its own launch: by the
time the probes attach, the `sudo` that started it has already exec'd. So there are **no `sudo`
events in the database at all** unless you run a `sudo` command *while the agent is already
running*. Two things depend on this:

- Example query 2, the recursive process tree under the most recent `sudo`, returns **0 rows**
  without it. That is your best SQL to show, so don't let it come up empty.
- `sudo id` is also what produces SETUID and CAPS events, so it demonstrates the identity-tracking
  probes rather than just exec/exit.

### What all the other rows are — have an answer ready

The table will not be quiet. On this VM the busiest process names are `cat`, `sh`, `cpuUsage.sh`,
`sed` and `git` — that is **VS Code's own remote server** polling the machine — plus `runc` and
`pg_isready` every 2 seconds, which is the database container's health check probing itself.

Don't apologise for it, use it:

> "Most of this is the editor's remote server polling the box, and the database container running
> its own health check. That's what a real host looks like — mostly background noise. Which is the
> argument for querying it rather than watching it scroll."

If you want fewer rows on screen, set the type filter to **EXECVE only**. Your own commands always
appear at the top, because the table is newest-first.

---

## 3. "The newest row is old" — two different causes

```bash
docker compose exec -T db psql -U argus -d argus -t \
  -c "SELECT (now() - max(time))::text || ' since the last event' FROM events;"
```

If that is more than a couple of seconds, it is one of two things.

### Cause 1 (common): the agent isn't running

Closing a VS Code terminal kills whatever was running in it, so the agent stops the moment that
tab goes away — silently, as far as the browser is concerned. The page keeps working and keeps
showing the same rows, which looks like a frozen UI rather than a stopped collector.

```bash
pgrep -a argus        # no "./argus" line = it's not running
```

Fix: start it again in terminal 1 with `sudo ./argus`, and **don't close that tab.**

### Cause 2 (subtle): the Mac slept while the agent was running

The agent converts the kernel's boot-relative timestamps to wall-clock time using an offset it
measures once, at startup. Linux's monotonic clock does not advance during suspend, so after a
resume that offset is stale and new events are written with timestamps from *before* the sleep.
The table sorts newest-first, so fresh events get buried under old ones and the demo looks dead
even though everything is running.

Tell this apart from Cause 1: `pgrep -a argus` **does** show the agent, but events keep arriving
with times that never catch up to `date -u`.

Fix: Ctrl+C terminal 1 and run `sudo ./argus` again — that re-measures the offset.

**Prevention: disable sleep on the Mac before you start.**

---

## 4. Troubleshooting by symptom

| Symptom | Most likely cause | Fix |
|---|---|---|
| VS Code won't connect over SSH | VM still booting, or its DHCP address changed | Wait; else `hostname -I` in the UTM window and use that address |
| `docker compose ps` shows nothing | Daemon not up yet after reboot | `docker compose up -d`, wait 10 s |
| `Up` but never `healthy` | Postgres still starting | Wait 15 s |
| Agent: `connect to database` | DB not running | Step 3 |
| Agent: `verifier said:` | Probe rejected by this kernel | Skip the agent, demo from stored data |
| API: `address already in use` | Old copy running | `pkill argus-api` |
| Browser won't connect | Port not forwarded | VS Code PORTS tab, or the ufw route |
| Table loads but never updates | Agent stopped — often a closed VS Code terminal | `pgrep -a argus`; restart it (§3, cause 1) |
| Events arrive but timestamps never catch up | Mac slept while the agent ran | Restart the agent (§3, cause 2) |
| `sudo` asks for a password repeatedly | It caches per-terminal | Run `sudo -v` once in each terminal that needs it |
| Example query 2 returns 0 rows | No `sudo` ran while the agent was up | Run `sudo id` in terminal 3, then re-run the query |

---

## 5. Shutdown

Ctrl+C terminal 2, then terminal 1. Both should exit with no stack trace — the clean exit is part
of the demo. Leave the database running.

---

## 6. The demo beats (detail in the plan)

1. One sentence on what it is.
2. `bpf/sensor.bpf.c` — the kernel hook. 56-byte struct into a ring buffer.
3. Start the agent — nine probes, all-or-nothing.
4. Start the API — **no sudo**.
5. The browser table.
6. **Type a command, point at the row appearing.** The moment that lands.
7. `http://localhost:8080/api/events?type=EXECVE&limit=5` — the raw JSON the page consumes.
8. `psql` — the `processes` view, and why it matches forward in time (pid reuse).
9. One honest limitation: `fork()` without `exec` produces no start event.
