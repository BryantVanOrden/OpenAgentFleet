-- Oaf sessions, devices and device jobs: the fleet chat as an agent.
--
-- A session is one named conversation with Oaf, scoped like a Claude Code
-- session: a device (the operator's PC running `fleetctl host`, or the phone)
-- and a working folder on it. Its messages live in fleet comms under the
-- conversation id "oaf:<session id>", so history, compaction and both clients'
-- renderers are shared with every other thread.
CREATE TABLE IF NOT EXISTS oaf_sessions (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL,
    name        TEXT NOT NULL,
    device_id   TEXT,
    cwd         TEXT NOT NULL DEFAULT '',
    provider_id TEXT,
    pinned      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS oaf_sessions_owner ON oaf_sessions(owner_id, updated_at DESC);

-- A device is something Oaf can act on outside the sandboxes: a PC or a phone
-- that connected itself and polls for jobs. Roots is the JSON list of folders
-- the device agreed to expose; a session's cwd must fall under one of them.
CREATE TABLE IF NOT EXISTS oaf_devices (
    id           TEXT PRIMARY KEY,
    owner_id     TEXT NOT NULL,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,          -- pc | phone
    platform     TEXT NOT NULL DEFAULT '',
    roots        TEXT NOT NULL DEFAULT '[]',
    auto_approve BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen    TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS oaf_devices_owner ON oaf_devices(owner_id);

-- One unit of work handed to a device: run this, read that. The device polls,
-- executes with its own approval prompt, and posts the result. Kept as rows so
-- a job survives an orchestrator restart mid-flight and so the audit trail of
-- what Oaf did on someone's machine is durable.
CREATE TABLE IF NOT EXISTS device_jobs (
    id          TEXT PRIMARY KEY,
    device_id   TEXT NOT NULL,
    session_id  TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL,
    args        TEXT NOT NULL DEFAULT '{}',
    state       TEXT NOT NULL DEFAULT 'pending',   -- pending | running | done | failed | denied
    result      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS device_jobs_pending ON device_jobs(device_id, state, created_at);

-- Goals and loops: work Oaf keeps doing after the message that asked for it.
-- A goal ticks until Oaf says it is done; a loop runs its text every
-- every_sec seconds until stopped.
CREATE TABLE IF NOT EXISTS oaf_jobs (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    kind        TEXT NOT NULL,             -- goal | loop
    text        TEXT NOT NULL,
    every_sec   INTEGER NOT NULL DEFAULT 0,
    state       TEXT NOT NULL DEFAULT 'running',  -- running | done | stopped | failed
    progress    TEXT NOT NULL DEFAULT '',
    runs        INTEGER NOT NULL DEFAULT 0,
    next_at     TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS oaf_jobs_due ON oaf_jobs(state, next_at);
