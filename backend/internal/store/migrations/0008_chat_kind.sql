-- Chat messages gain a kind so a proposed plan is distinguishable from
-- ordinary conversation.
--
-- Talking to an agent should not start work. Previously the only way to get an
-- agent to do something from chat was as_task, which begins acting
-- immediately; there was no way to say "work out how you would do this, and
-- let me look before you touch anything". A plan is stored as a normal agent
-- message carrying kind='plan', so history renders unchanged for old rows and
-- the client knows which messages to offer an Approve control on.
ALTER TABLE chat_messages
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'message';

-- Set once the operator acts on a plan, so an approved or discarded plan stops
-- offering the button on every subsequent load.
ALTER TABLE chat_messages
    ADD COLUMN IF NOT EXISTS plan_state TEXT NOT NULL DEFAULT '';
