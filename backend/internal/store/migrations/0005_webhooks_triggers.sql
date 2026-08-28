-- Migration 0005: Event-Driven Webhook Sinks & Scheduled Cron Triggers
CREATE TABLE IF NOT EXISTS webhooks (
    id TEXT PRIMARY KEY,
    token TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    target_archetype TEXT NOT NULL DEFAULT 'fullstack_dev',
    goal_template TEXT NOT NULL,
    secret TEXT,
    active BOOLEAN NOT NULL DEFAULT true,
    last_triggered_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cron_triggers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    schedule_cron TEXT NOT NULL,
    target_archetype TEXT NOT NULL DEFAULT 'cyber_ops',
    goal_template TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
