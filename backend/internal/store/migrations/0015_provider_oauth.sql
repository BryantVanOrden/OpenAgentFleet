-- Migration 0015: signing in to a provider with an account.
--
-- A provider could only authenticate with a pasted API key, which does not
-- exist for subscription products where the entitlement belongs to an account
-- rather than to a key. A provider can now hold an OAuth client and a sealed
-- refresh token instead.
ALTER TABLE providers ADD COLUMN IF NOT EXISTS auth_mode TEXT NOT NULL DEFAULT 'api_key';
ALTER TABLE providers ADD COLUMN IF NOT EXISTS oauth_client_id TEXT NOT NULL DEFAULT '';
-- Names the vault entry holding the refresh token and client secret. As with
-- api_key_ref, the secret itself is never in this table.
ALTER TABLE providers ADD COLUMN IF NOT EXISTS oauth_token_ref TEXT NOT NULL DEFAULT '';
