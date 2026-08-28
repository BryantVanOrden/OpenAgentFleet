-- Migration 0006: Shared Fleet Secrets, Browser Sessions, and Inter-Agent P2P Messages
CREATE TABLE IF NOT EXISTS shared_secrets (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'fleet', -- 'fleet', 'instance:<id>', 'swarm:<id>'
    note TEXT,
    created_by TEXT,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS shared_sessions (
    id TEXT PRIMARY KEY,
    domain TEXT NOT NULL,
    title TEXT NOT NULL,
    cookies_json TEXT NOT NULL,
    local_storage_json TEXT,
    created_by_instance TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS peer_messages (
    id TEXT PRIMARY KEY,
    from_instance_id TEXT NOT NULL,
    from_instance_name TEXT NOT NULL,
    to_instance_id TEXT NOT NULL, -- target instance ID or 'broadcast'
    kind TEXT NOT NULL DEFAULT 'message', -- 'message', 'question', 'report', 'delegation'
    content TEXT NOT NULL,
    data_json TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_peer_msgs_to ON peer_messages(to_instance_id);
CREATE INDEX IF NOT EXISTS idx_peer_msgs_created ON peer_messages(created_at DESC);
