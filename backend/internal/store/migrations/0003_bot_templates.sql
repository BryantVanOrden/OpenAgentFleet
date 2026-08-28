-- Migration 0003: Bot Templates and Archetypes
ALTER TABLE instances ADD COLUMN IF NOT EXISTS archetype_id TEXT;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS system_prompt TEXT;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS preinstalled_tools TEXT;
