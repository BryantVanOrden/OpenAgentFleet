-- MCP server registrations.
--
-- The table itself has existed since 0007 and nothing ever wrote a row to it:
-- the manager kept registrations in a map, so an operator configured a server,
-- it worked until the next deploy, and then the MCP Hub screen was empty with no
-- explanation. This migration adds only what was missing for the store to use it
-- (an index, and the note about what the env column holds); the columns were
-- already there, unread.
--
-- Written as ADD COLUMN IF NOT EXISTS rather than CREATE TABLE, because a
-- CREATE TABLE IF NOT EXISTS against the 0007 table is a silent no-op and
-- everything after it in the same file then refers to columns that do not exist.
-- That is exactly how the first version of this migration failed.

ALTER TABLE mcp_servers
    ADD COLUMN IF NOT EXISTS args_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE mcp_servers
    ADD COLUMN IF NOT EXISTS env_json TEXT NOT NULL DEFAULT '{}';

-- env_json can hold credentials -- an API key for a hosted MCP server, a bearer
-- token sent as an HTTP header. Noted here so it is obvious this table is
-- sensitive; the API projection reports the key names only.
COMMENT ON COLUMN mcp_servers.env_json IS
    'Process environment (stdio) or HTTP headers. May contain credentials; the API returns key names only.';

CREATE INDEX IF NOT EXISTS idx_mcp_servers_created ON mcp_servers(created_at);
