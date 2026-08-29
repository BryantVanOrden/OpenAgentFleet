-- Migration 0017: the consent page URL for in-app sign-in.
--
-- The device flow only needed a device endpoint. Signing in inside the app uses
-- the authorization-code flow, which needs the page a person is actually sent
-- to. Empty falls back to Google's.
ALTER TABLE providers ADD COLUMN IF NOT EXISTS oauth_auth_url TEXT NOT NULL DEFAULT '';
