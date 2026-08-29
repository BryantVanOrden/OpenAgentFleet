-- Migration 0010: pin a webhook or cron trigger to one instance.
--
-- 0005 could only name a target archetype, which the dispatcher resolves to
-- whichever matching instance happens to be running. That is the right default
-- for "any fullstack bot can take this", but an operator wiring a webhook to a
-- specific machine had no way to say so, and a fleet with two bots of the same
-- archetype would send the work to an arbitrary one of them.
--
-- Nullable and unset by default, so every existing trigger keeps resolving by
-- archetype exactly as before. No foreign key: deleting an instance must not
-- delete the trigger's configuration, it should surface as a dispatch error
-- the operator can see and repoint.
ALTER TABLE webhooks
    ADD COLUMN IF NOT EXISTS target_instance_id TEXT NOT NULL DEFAULT '';

ALTER TABLE cron_triggers
    ADD COLUMN IF NOT EXISTS target_instance_id TEXT NOT NULL DEFAULT '';
