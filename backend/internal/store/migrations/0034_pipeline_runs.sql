-- Pipeline runs, made durable.
--
-- Pipelines persisted; a run in flight did not. An orchestrator restart
-- mid-run left the run recorded as `running` forever -- the executor's state
-- was process memory -- and every node result that had already landed was lost
-- with it. Now the run is written through on every node transition, and boot
-- resumes interrupted runs from their last settled node instead of forgetting
-- them.

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id            TEXT PRIMARY KEY,
    pipeline_id   TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'running',
    -- JSON maps of node id -> result string / state string. TEXT holding JSON,
    -- like the pipelines table's own columns: the store never queries inside.
    node_results  TEXT NOT NULL DEFAULT '{}',
    node_states   TEXT NOT NULL DEFAULT '{}',
    started_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipe ON pipeline_runs(pipeline_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_status ON pipeline_runs(status);
