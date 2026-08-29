-- Migration 0016: OAuth against providers other than Google.
--
-- The sign-in flow was hardcoded to Google's endpoints, which made it a Google
-- feature rather than an authentication method. A provider can now carry its
-- own device and token endpoints, so any service offering the device flow can
-- be signed into with credentials you register yourself.
ALTER TABLE providers ADD COLUMN IF NOT EXISTS oauth_device_url TEXT NOT NULL DEFAULT '';
ALTER TABLE providers ADD COLUMN IF NOT EXISTS oauth_token_url TEXT NOT NULL DEFAULT '';
ALTER TABLE providers ADD COLUMN IF NOT EXISTS oauth_scope TEXT NOT NULL DEFAULT '';
