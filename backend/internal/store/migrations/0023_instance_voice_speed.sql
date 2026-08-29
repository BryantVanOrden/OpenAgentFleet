-- How fast each agent speaks.
--
-- The speak endpoint and the TTS service have always taken a speed, but there
-- was nowhere to keep one per bot, so every agent spoke at the same rate. A
-- distinct voice is what makes a fleet legible by ear; pace is the other half
-- of that, and some voices are simply too quick or too slow to follow at 1.0.
--
-- 0 means "not set", which falls back to the operator's default rather than
-- pinning every existing bot to a speed nobody chose.
ALTER TABLE instances
    ADD COLUMN IF NOT EXISTS voice_speed DOUBLE PRECISION NOT NULL DEFAULT 0;
