-- Migration 0002: Prime Agent Concepts (Continual Refinement & Recursive Sub-Agents)

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS parent_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS auto_refine BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE skills ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS refinement_notes TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS tasks_parent_task_idx ON tasks (parent_task_id);
