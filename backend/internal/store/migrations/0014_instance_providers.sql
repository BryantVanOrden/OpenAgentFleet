-- Migration 0014: per-bot model chains.
--
-- Every agent shared one fleet-wide fallback order, so a cheap bot doing form
-- entry and a bot doing vision work could not be pointed at different models.
-- An instance can now carry its own ordered list of providers; empty means the
-- fleet default, which is what every existing bot gets.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS provider_ids TEXT;
