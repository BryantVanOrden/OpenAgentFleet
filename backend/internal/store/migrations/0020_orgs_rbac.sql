-- Migration 0020: organisations, departments, and per-bot permissions.
--
-- Access was three global roles: auditor, operator, admin. Every operator
-- could see, drive and delete every bot, and read every shared credential.
-- That is workable for one person and wrong for an organisation, where the
-- interesting questions are "which bots" and "how much", not "which of three
-- ranks".
--
-- An org is also a department: they nest by naming rather than by hierarchy,
-- which keeps membership resolution a single lookup instead of a tree walk.
CREATE TABLE IF NOT EXISTS orgs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Membership carries a role, which supplies a default permission set. A
-- per-bot grant can widen or narrow it for one bot.
CREATE TABLE IF NOT EXISTS org_members (
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_role   TEXT NOT NULL DEFAULT 'member', -- owner | admin | member | viewer
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX IF NOT EXISTS org_members_user_idx ON org_members(user_id);

-- Per-bot permission grants, stored as an explicit list rather than a bitmask
-- so a row is readable in psql during an incident.
CREATE TABLE IF NOT EXISTS bot_grants (
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    instance_id TEXT NOT NULL,
    -- JSON array of permission names. Empty means "explicitly nothing", which
    -- is how a bot is hidden from someone who can otherwise see their org.
    permissions TEXT NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, instance_id)
);
CREATE INDEX IF NOT EXISTS bot_grants_instance_idx ON bot_grants(instance_id);

-- Bots, secrets and sessions belong to an org. NULL means unassigned, which
-- only a global admin sees — so nothing becomes invisible the moment this
-- migration lands on a fleet that predates orgs.
ALTER TABLE instances       ADD COLUMN IF NOT EXISTS org_id TEXT;
ALTER TABLE shared_secrets  ADD COLUMN IF NOT EXISTS org_id TEXT;
ALTER TABLE shared_sessions ADD COLUMN IF NOT EXISTS org_id TEXT;

CREATE INDEX IF NOT EXISTS instances_org_idx       ON instances(org_id);
CREATE INDEX IF NOT EXISTS shared_secrets_org_idx  ON shared_secrets(org_id);
CREATE INDEX IF NOT EXISTS shared_sessions_org_idx ON shared_sessions(org_id);
