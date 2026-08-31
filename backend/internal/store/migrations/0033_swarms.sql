-- Multi-agent swarm missions, which previously lived only in process memory.
--
-- Creating a swarm appended one message to a map and did nothing else: no
-- tasks, no instances started, and nothing that survived a restart. The mission,
-- the blackboard and the artifacts are all worth keeping -- a peer review that
-- disappears on the next deploy is not a review.
--
-- Members, messages and artifacts are JSONB rather than three side tables. They
-- are only ever read and written as a whole swarm, never queried across
-- missions, so normalising them would buy joins nobody performs.
CREATE TABLE IF NOT EXISTS swarms (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    mission    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'initializing',
    members    JSONB NOT NULL DEFAULT '[]'::jsonb,
    messages   JSONB NOT NULL DEFAULT '[]'::jsonb,
    artifacts  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_swarms_created ON swarms(created_at DESC);
