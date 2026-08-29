-- Persist the read-only desktop endpoint.
--
-- VNCViewURL was set when a bot was provisioned and then never stored: there
-- was no column for it. So it survived until the orchestrator restarted and
-- was empty from then on, and the read-only desktop proxy -- which fails
-- closed, correctly -- refused every auditor with "this instance predates the
-- read-only desktop". The feature had simply never worked across a restart,
-- and the message blamed the instance's age rather than the missing column.
--
-- Found because reconcile started repairing sandbox addresses and logged the
-- same "fix" every thirty seconds forever: it was writing a value that had
-- nowhere to go.
ALTER TABLE instances
    ADD COLUMN IF NOT EXISTS vnc_view_url TEXT NOT NULL DEFAULT '';
