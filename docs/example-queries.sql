-- Example queries over the Argus event store.
--
-- Run all of them:
--   docker compose exec -T db psql -U argus -d argus < docs/example-queries.sql
-- Or open a shell and paste one:
--   docker compose exec db psql -U argus -d argus

-- 1. The 20 most recent process executions.
SELECT time, pid, ppid, uid, comm
FROM events
WHERE type = 'EXECVE'
ORDER BY time DESC
LIMIT 20;

-- 2. Process tree under the most recent sudo: everything it spawned, and
--    what those spawned, as seen in the last hour.
--    Matching children by ppid alone would pull in unrelated processes that
--    reused a pid, so each child must also start while its parent is alive.
--    (The anchor is parenthesised: ORDER BY/LIMIT inside one arm of a UNION
--    is a syntax error otherwise.)
WITH RECURSIVE tree AS (
    (SELECT p.*, 0 AS depth
     FROM processes AS p
     WHERE p.comm = 'sudo'
       AND p.started_at > now() - interval '1 hour'
     ORDER BY p.started_at DESC
     LIMIT 1)
  UNION ALL
    SELECT c.*, t.depth + 1
    FROM processes AS c
    JOIN tree AS t
      ON c.ppid = t.pid
     AND c.started_at >= t.started_at
     AND (t.exited_at IS NULL OR c.started_at <= t.exited_at)
    WHERE t.depth < 10
)
SELECT repeat('  ', depth) || comm AS process, pid, ppid, uid, started_at, exited_at
FROM tree
ORDER BY started_at;

-- 3. Privilege gains in the last hour: identity events that ended as root or
--    holding any capability. This is the raw material for M4's T1548 rule.
SELECT time, type, pid, comm, uid, gid, to_hex(cap_effective) AS caps
FROM events
WHERE type IN ('SETUID', 'CAPS')
  AND (uid = 0 OR cap_effective <> 0)
  AND time > now() - interval '1 hour'
ORDER BY time DESC;

-- 4. Top 10 binaries by exec count in the last hour, in 1-minute buckets
--    collapsed to a total — time_bucket is TimescaleDB's GROUP BY for time.
SELECT comm, count(*) AS execs, count(DISTINCT time_bucket('1 minute', time)) AS active_minutes
FROM events
WHERE type = 'EXECVE'
  AND time > now() - interval '1 hour'
GROUP BY comm
ORDER BY execs DESC
LIMIT 10;

-- 5. Short-lived processes: started and exited within one second. Droppers
--    and recon one-liners look like this; so does most shell plumbing.
SELECT pid, ppid, comm, uid, started_at,
       round(extract(epoch FROM exited_at - started_at) * 1000) AS lifetime_ms
FROM processes
WHERE exited_at IS NOT NULL
  AND exited_at - started_at < interval '1 second'
  AND started_at > now() - interval '1 hour'
ORDER BY started_at DESC
LIMIT 20;
