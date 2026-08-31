-- Which model produced each memory's embedding.
--
-- The index scored everything with a 128-dimensional hashed bag of words. Once a
-- real embedding provider can be configured, the two schemes coexist while the
-- backfill runs -- and a cosine similarity between vectors from different models
-- is a number with no meaning. Recording the model is what lets search compare
-- like with like instead of silently ranking on noise.
--
-- Empty string, not NULL: every existing row was produced by the hashed
-- fallback, which is exactly what "" means.
ALTER TABLE episodic_memories
    ADD COLUMN IF NOT EXISTS embed_model TEXT NOT NULL DEFAULT '';

-- Recall reads the bot's own namespace plus the shared ones, and shared
-- namespaces were never written to, so this index covers the query that
-- actually runs.
CREATE INDEX IF NOT EXISTS idx_memories_namespace_created
    ON episodic_memories(namespace, created_at DESC);
