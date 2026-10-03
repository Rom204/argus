-- 001_events.sql — the Argus event store.
--
-- One row per event, one column per field of struct process_event in
-- bpf/event.h (CLAUDE.md §6.2). Rows are never updated: this is the raw record,
-- and anything derived (like "processes") is a view over it.
--
-- Names are deliberately NOT schema-qualified, so the integration test can
-- apply this exact file inside a throwaway schema.

CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE events (
    -- Wall-clock time, converted from the kernel's CLOCK_MONOTONIC stamp by the
    -- agent (event.Clock).
    time          TIMESTAMPTZ NOT NULL,
    version       SMALLINT    NOT NULL,
    -- Stored by name ('EXECVE', 'EXIT', 'SETUID', 'CAPS') so queries read
    -- without a lookup table. Adding FORK later needs no schema change.
    type          TEXT        NOT NULL,
    -- pid/ppid are uint32 in the kernel but capped by pid_max (at most 2^22),
    -- so INTEGER holds them.
    pid           INTEGER     NOT NULL,
    ppid          INTEGER     NOT NULL,
    -- uid/gid are full-range uint32 (e.g. 4294967295), which overflows INTEGER.
    uid           BIGINT      NOT NULL,
    gid           BIGINT      NOT NULL,
    -- uint64 bitmask cast bit-for-bit to BIGINT. The highest capability bit is
    -- ~40, far from the sign bit, so values read back positive and exact.
    cap_effective BIGINT      NOT NULL,
    comm          TEXT        NOT NULL
);

-- Partitions events into time chunks; also creates the index on time.
SELECT create_hypertable('events', by_range('time'));

-- "Everything this pid did", newest first — the lookup the processes view and
-- most investigations need.
CREATE INDEX events_pid_time_idx ON events (pid, time DESC);

-- One row per observed process instance: each EXECVE paired with the first
-- EXIT of the same pid at or after it. Matching forward in time, instead of
-- grouping by pid, keeps a recycled pid from merging two different processes.
--
-- Known limits: a process created by fork() without exec, or started before
-- the agent, has no EXECVE and so no row here (see the fork gap in
-- KNOWLEDGE_BASE.md). exited_at is NULL while the process is still running.
CREATE VIEW processes AS
SELECT s.pid,
       s.ppid,
       s.comm,
       s.uid,
       s.gid,
       s.cap_effective,
       s.time AS started_at,
       x.time AS exited_at
FROM events AS s
LEFT JOIN LATERAL (
    SELECT e.time
    FROM events AS e
    WHERE e.pid = s.pid
      AND e.type = 'EXIT'
      AND e.time >= s.time
    ORDER BY e.time
    LIMIT 1
) AS x ON true
WHERE s.type = 'EXECVE';
