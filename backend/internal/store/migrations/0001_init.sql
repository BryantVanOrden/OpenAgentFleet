-- OpenAgentFleet initial schema.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'operator',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Encrypted credential vault. `value` is AES-256-GCM sealed with MASTER_KEY;
-- plaintext never leaves the orchestrator except when injected into a sandbox
-- keyring at run time.
CREATE TABLE IF NOT EXISTS secrets (
    ref        TEXT PRIMARY KEY,
    value      BYTEA NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL,
    base_url    TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL,
    api_key_ref TEXT NOT NULL DEFAULT '',
    vision      BOOLEAN NOT NULL DEFAULT TRUE,
    temperature DOUBLE PRECISION NOT NULL DEFAULT 0.2,
    max_tokens  INTEGER NOT NULL DEFAULT 1024,
    priority    INTEGER NOT NULL DEFAULT 100,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS providers_priority_idx ON providers (enabled, priority);

CREATE TABLE IF NOT EXISTS instances (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    owner_id     TEXT NOT NULL DEFAULT '',
    tier         TEXT NOT NULL,
    driver       TEXT NOT NULL,
    state        TEXT NOT NULL,
    runtime_id   TEXT NOT NULL DEFAULT '',
    profile      JSONB NOT NULL,
    override     JSONB,
    vnc_url      TEXT NOT NULL DEFAULT '',
    stream_url   TEXT NOT NULL DEFAULT '',
    agentd_url   TEXT NOT NULL DEFAULT '',
    egress       JSONB NOT NULL DEFAULT '{}'::jsonb,
    shell_access BOOLEAN NOT NULL DEFAULT FALSE,
    labels       JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS instances_state_idx ON instances (state);

CREATE TABLE IF NOT EXISTS skills (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    params        JSONB NOT NULL DEFAULT '[]'::jsonb,
    steps         JSONB NOT NULL DEFAULT '[]'::jsonb,
    markdown      TEXT NOT NULL DEFAULT '',
    source_run_id TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
    id          TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    owner_id    TEXT NOT NULL DEFAULT '',
    goal        TEXT NOT NULL,
    skill_id    TEXT NOT NULL DEFAULT '',
    params      JSONB NOT NULL DEFAULT '{}'::jsonb,
    state       TEXT NOT NULL,
    step        INTEGER NOT NULL DEFAULT 0,
    max_steps   INTEGER NOT NULL DEFAULT 60,
    provider_id TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    result      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at  TIMESTAMPTZ,
    ended_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS tasks_instance_idx ON tasks (instance_id, created_at DESC);
CREATE INDEX IF NOT EXISTS tasks_state_idx ON tasks (state);

CREATE TABLE IF NOT EXISTS task_steps (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    step            INTEGER NOT NULL,
    action          JSONB NOT NULL,
    observation_key TEXT NOT NULL DEFAULT '',
    outcome         TEXT NOT NULL DEFAULT '',
    duration_ms     BIGINT NOT NULL DEFAULT 0,
    prompt_tokens   INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS task_steps_task_idx ON task_steps (task_id, step);

CREATE TABLE IF NOT EXISTS alerts (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,
    severity      TEXT NOT NULL DEFAULT 'info',
    instance_id   TEXT NOT NULL DEFAULT '',
    task_id       TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL,
    body          TEXT NOT NULL DEFAULT '',
    screenshot_id TEXT NOT NULL DEFAULT '',
    needs_reply   BOOLEAN NOT NULL DEFAULT FALSE,
    reply         TEXT NOT NULL DEFAULT '',
    resolved_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS alerts_open_idx ON alerts (resolved_at, created_at DESC);

-- Push targets registered by the Flutter companion app.
CREATE TABLE IF NOT EXISTS devices (
    token      TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    platform   TEXT NOT NULL, -- android | ios
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS devices_user_idx ON devices (user_id);

-- Chat turns between an operator and an instance-scoped agent.
CREATE TABLE IF NOT EXISTS chat_messages (
    id          TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    task_id     TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL, -- user | agent | system
    body        TEXT NOT NULL,
    image_key   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS chat_instance_idx ON chat_messages (instance_id, created_at);
