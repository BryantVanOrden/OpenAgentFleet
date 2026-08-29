-- Migration 0019: pipelines survive a restart.
--
-- Webhooks, cron triggers, conversations and episodic memory were all made
-- durable; pipelines were not. A pipeline is the clearest case for persistence
-- — written once, wired to a schedule, expected to still be there next week —
-- and it was being lost on every deploy.
CREATE TABLE IF NOT EXISTS pipelines (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    nodes_json  TEXT NOT NULL DEFAULT '[]',
    edges_json  TEXT NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
