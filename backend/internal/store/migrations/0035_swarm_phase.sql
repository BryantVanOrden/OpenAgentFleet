-- Swarm phases, enforced.
--
-- The phase field on a message ("planning", "execution", ...) was recorded and
-- displayed and nothing gated on it. A swarm now carries its own phase: created
-- with plan_first, its members are asked to publish a plan first, non-plan
-- artifacts are refused while planning, and the execution tasks are held until
-- every member's plan is in or an operator advances the phase by hand.

ALTER TABLE swarms
    ADD COLUMN IF NOT EXISTS phase TEXT NOT NULL DEFAULT 'execution';
ALTER TABLE swarms
    ADD COLUMN IF NOT EXISTS plan_first BOOLEAN NOT NULL DEFAULT FALSE;
