-- Migration 0004: Persistent Episodic Vector Memory
CREATE TABLE IF NOT EXISTS episodic_memories (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL DEFAULT 'global',
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    tags TEXT,
    embedding TEXT,
    source_task_id TEXT,
    source_instance_id TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_memories_namespace ON episodic_memories(namespace);
CREATE INDEX IF NOT EXISTS idx_memories_created_at ON episodic_memories(created_at DESC);
