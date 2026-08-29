-- Long-lived access keys for scripts and CI.
--
-- Until now the only way in was a password login returning a short-lived JWT,
-- so anything automated had to be given someone's password. A key belongs to a
-- user and carries that user's role, can be named so it is obvious what would
-- break when it is revoked, and records when it was last used so a key nobody
-- has touched in months is visible as such.
--
-- The secret is never stored. `id` is the public half, sent as part of the key
-- so a request finds its row without scanning; `secret_hash` is a SHA-256 of
-- the other half, compared in constant time. Losing the database therefore
-- does not hand anyone a working key.
CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    secret_hash  TEXT NOT NULL,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    -- Revoking keeps the row: a key that has been used is part of the audit
    -- trail, and deleting it would make past activity untraceable.
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS api_keys_user_idx ON api_keys(user_id);

-- Disabling a user without deleting them.
--
-- Deleting cascades their keys away and orphans everything they made; a
-- disabled user simply cannot sign in, and their keys stop working with them.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;
