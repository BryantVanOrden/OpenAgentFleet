-- Migration 0012: named conversations in fleet comms.
--
-- Threads used to be inferred from who messaged whom, which meant they could
-- not be created before the first message or deleted after the last one. A
-- conversation is now an object the operator owns: make one between two bots
-- so they can talk, delete it when that work is done.
CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL DEFAULT 'group', -- 'direct' | 'pair' | 'group'
    title TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS conversation_members (
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    -- An instance ID, or 'operator' for the human.
    member_id TEXT NOT NULL,
    PRIMARY KEY (conversation_id, member_id)
);

CREATE INDEX IF NOT EXISTS idx_conv_members_member ON conversation_members(member_id);

-- Existing messages keep a NULL conversation_id and still render: the comms
-- screen places an unassigned message by its recipient, so no history is lost
-- by this migration.
ALTER TABLE peer_messages ADD COLUMN IF NOT EXISTS conversation_id TEXT;
CREATE INDEX IF NOT EXISTS idx_peer_msgs_conversation ON peer_messages(conversation_id);

-- Compaction: a long thread is summarised into one message and the messages it
-- replaces are marked rather than deleted. The history is still in this table
-- and still auditable; it just stops being replayed into the screen and into
-- an agent's context.
ALTER TABLE peer_messages ADD COLUMN IF NOT EXISTS compacted BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS idx_peer_msgs_compacted ON peer_messages(compacted);
