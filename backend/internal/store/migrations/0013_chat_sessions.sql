-- Migration 0013: separate chats with one bot.
--
-- A bot had a single unbounded chat history: no way to start a fresh one, and
-- no way to clear a chat that had gone somewhere unhelpful without losing
-- every conversation you had ever had with that agent. Chats are now sessions.
CREATE TABLE IF NOT EXISTS chat_sessions (
    id          TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS chat_sessions_instance_idx
    ON chat_sessions (instance_id, created_at DESC);

-- Existing messages keep a NULL session and are shown as the bot's original
-- chat, so no history is stranded by this migration.
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS session_id TEXT;
CREATE INDEX IF NOT EXISTS chat_messages_session_idx ON chat_messages (session_id, created_at);

-- Pinning and naming. A chat you return to daily should not sink below one you
-- opened once, and "Earlier chat" tells you nothing about which of eleven
-- threads held the thing you are looking for.
ALTER TABLE chat_sessions ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;
