-- A voice per agent.
--
-- Every agent speaking in the same voice makes a fleet unreadable by ear: with
-- several running you cannot tell which one just reported without looking.
-- Empty means "whatever the operator's default is", so existing instances are
-- unchanged.
ALTER TABLE instances
    ADD COLUMN IF NOT EXISTS voice TEXT NOT NULL DEFAULT '';
