-- The org chart, external agents, budgets, and work as durable tickets.
--
-- Until now the relay that hands work from one bot to the next lived in
-- process memory and was reconstructed from what bots said in chat: every
-- relay bug of September 2026 was a misread sentence or a restart that lost
-- the job. A ticket is the work itself, stored: one assignee, a parent it
-- exists for, and the tickets it waits on. A hand-off is a blocker finishing.

-- ---------------------------------------------------------------- agents ---

-- The agent CLIs a PC running `fleetctl host` has installed (claude, codex,
-- hermes), reported when it registers, so an external agent can only be
-- pointed at a device that can actually run it.
ALTER TABLE oaf_devices ADD COLUMN IF NOT EXISTS runtimes TEXT NOT NULL DEFAULT '[]';


-- kind: desktop (a sandbox we provision) or an external runtime that runs
-- somewhere else and is driven through an adapter: claude_code, codex, hermes
-- (local CLIs on a PC running `fleetctl host`), openclaw (its gateway) or
-- webhook (anything that accepts a POST and calls back).
ALTER TABLE instances ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'desktop';
-- reports_to: the agent this one reports to. NULL reports to the operator.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS reports_to TEXT;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '';
-- capabilities: "when I'm useful", one short paragraph other agents read to
-- decide who to ask.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS capabilities TEXT NOT NULL DEFAULT '';
-- connection: how an external agent is reached (device, folder, model, URL,
-- the vault reference of its token). JSON; never holds a secret itself.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS connection TEXT NOT NULL DEFAULT '{}';
-- A monthly spend ceiling in dollars, 0 for none, and the percentage at which
-- the operator is warned first.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS budget_month_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS budget_warn_pct INTEGER NOT NULL DEFAULT 80;
-- trust: standard, or low for an agent that reads hostile input. What a
-- low-trust agent writes reaches other agents only fenced as data.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS trust TEXT NOT NULL DEFAULT 'standard';
-- hold: why the agent is not being given work ('' when it is). 'budget' is set
-- when it reaches its ceiling and cleared when the ceiling is raised or the
-- month turns.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS hold TEXT NOT NULL DEFAULT '';

-- A run belongs to the ticket it was started for, so its cost rolls up.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS ticket_id TEXT;
CREATE INDEX IF NOT EXISTS tasks_ticket ON tasks(ticket_id);

-- --------------------------------------------------------------- tickets ---

CREATE SEQUENCE IF NOT EXISTS ticket_number_seq;

CREATE TABLE IF NOT EXISTS tickets (
    id                   TEXT PRIMARY KEY,
    -- A short number for people: T-42.
    number               BIGINT NOT NULL DEFAULT nextval('ticket_number_seq'),
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    -- work | review | verify | unblock
    kind                 TEXT NOT NULL DEFAULT 'work',
    -- backlog | todo | in_progress | in_review | blocked | done | cancelled
    status               TEXT NOT NULL DEFAULT 'todo',
    priority             INTEGER NOT NULL DEFAULT 0,
    -- The ticket this one exists for. Structure, not dependency: a parent
    -- waits on a child only through a blocker edge.
    parent_id            TEXT,
    -- For review, verify and unblock tickets: the ticket being reviewed,
    -- verified or unblocked.
    target_id            TEXT,
    assignee_id          TEXT,
    assignee_user_id     TEXT,
    -- Who reviews this ticket's work when it finishes, and who checks this
    -- ticket's whole subtree when it comes to rest.
    reviewer_id          TEXT,
    verifier_id          TEXT,
    verified_fingerprint TEXT NOT NULL DEFAULT '',
    -- The last stopped state the operator was told about, so a stall is
    -- reported once rather than every minute.
    stall_fingerprint    TEXT NOT NULL DEFAULT '',
    created_by_id        TEXT,
    created_by_user_id   TEXT,
    owner_id             TEXT NOT NULL DEFAULT '',
    -- The fleet comms thread the work belongs to, and the request it came from.
    thread               TEXT NOT NULL DEFAULT '',
    origin               TEXT NOT NULL DEFAULT '',
    -- design | build | test | review, when the fleet chat divided the work.
    stage                TEXT NOT NULL DEFAULT '',
    -- The live run holding the ticket. Set atomically on checkout and cleared
    -- only by the run that holds it.
    task_id              TEXT,
    attempts             INTEGER NOT NULL DEFAULT 0,
    rounds               INTEGER NOT NULL DEFAULT 0,
    wakes                INTEGER NOT NULL DEFAULT 0,
    verdict              TEXT NOT NULL DEFAULT '',
    result               TEXT NOT NULL DEFAULT '',
    blocked_reason       TEXT NOT NULL DEFAULT '',
    budget_usd           DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL,
    started_at           TIMESTAMPTZ,
    done_at              TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS tickets_number ON tickets(number);
CREATE INDEX IF NOT EXISTS tickets_status ON tickets(status, updated_at);
CREATE INDEX IF NOT EXISTS tickets_assignee ON tickets(assignee_id, status);
CREATE INDEX IF NOT EXISTS tickets_parent ON tickets(parent_id);
CREATE INDEX IF NOT EXISTS tickets_target ON tickets(target_id);
CREATE UNIQUE INDEX IF NOT EXISTS tickets_task ON tickets(task_id) WHERE task_id IS NOT NULL;

-- ticket_id waits on blocker_id.
CREATE TABLE IF NOT EXISTS ticket_blockers (
    ticket_id  TEXT NOT NULL,
    blocker_id TEXT NOT NULL,
    PRIMARY KEY (ticket_id, blocker_id)
);
CREATE INDEX IF NOT EXISTS ticket_blockers_blocker ON ticket_blockers(blocker_id);

CREATE TABLE IF NOT EXISTS ticket_comments (
    id             TEXT PRIMARY KEY,
    ticket_id      TEXT NOT NULL,
    author_id      TEXT,
    author_user_id TEXT,
    author_name    TEXT NOT NULL DEFAULT '',
    -- comment | system | result | verdict
    kind           TEXT NOT NULL DEFAULT 'comment',
    body           TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS ticket_comments_ticket ON ticket_comments(ticket_id, created_at);

-- A budget warning is sent once per agent, month and threshold.
CREATE TABLE IF NOT EXISTS budget_notices (
    instance_id TEXT NOT NULL,
    month       TEXT NOT NULL,
    level       TEXT NOT NULL,     -- warn | stop
    created_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, month, level)
);
