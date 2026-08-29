-- Migration 0021: agents know who they are talking to.
--
-- Every human message arrived as role='user' with no attribution, so an agent
-- could not tell one person from another, address anyone by name, or notice
-- that the person asking now is not the person who set the task. In a fleet
-- with departments and several operators that is the difference between a
-- colleague and an anonymous prompt.
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS user_id   TEXT;
ALTER TABLE chat_messages ADD COLUMN IF NOT EXISTS user_name TEXT;

-- Peer messages already record which instance spoke; this records which person
-- did, for the ones that come from a human rather than a bot.
ALTER TABLE peer_messages ADD COLUMN IF NOT EXISTS from_user_id TEXT;

-- Memories can be about a person, not just about a machine or a task. An agent
-- that learns "this person prefers terse answers" should be able to keep that
-- against the person rather than against the world.
ALTER TABLE episodic_memories ADD COLUMN IF NOT EXISTS about_user_id TEXT;
CREATE INDEX IF NOT EXISTS memories_about_user_idx ON episodic_memories(about_user_id);
