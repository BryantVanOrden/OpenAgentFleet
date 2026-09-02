-- Repair pipeline_runs on deployments that predate 0034.
--
-- 0007 already created pipeline_runs, with `current_node_id` and
-- `node_results_json`. 0034 then declared the durable shape the store actually
-- queries -- `node_results` and `node_states` -- as CREATE TABLE IF NOT
-- EXISTS, which on any database that had run 0007 is a no-op. Its two CREATE
-- INDEX statements did run, so the migration looked applied while the columns
-- it existed to add were never there.
--
-- The store selects and upserts those two columns, so every pipeline read
-- failed with `column "node_results" does not exist` (SQLSTATE 42703). That
-- surfaced once at boot as "pipelines not loaded; they stay in-memory" and
-- left the feature 0034 was written to deliver -- runs surviving a restart --
-- silently off on every existing install.
--
-- ALTER ... IF NOT EXISTS so this is a no-op on a database created after 0034,
-- where the table already has the right shape.
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS node_results TEXT NOT NULL DEFAULT '{}';
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS node_states  TEXT NOT NULL DEFAULT '{}';

-- Carry forward anything 0007's column recorded rather than stranding it.
-- Guarded: node_results_json only exists where 0007 created the table, and a
-- database created after 0034 has no such column for the UPDATE to reference.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
         WHERE table_name = 'pipeline_runs' AND column_name = 'node_results_json'
    ) THEN
        UPDATE pipeline_runs
           SET node_results = node_results_json
         WHERE node_results = '{}'
           AND node_results_json IS NOT NULL
           AND node_results_json <> '{}';
    END IF;
END $$;
