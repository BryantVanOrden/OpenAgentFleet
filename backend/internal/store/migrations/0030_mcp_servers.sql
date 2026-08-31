-- MCP server registrations, which previously lived only in process memory.
--
-- An operator configured a server, it worked until the next deploy, and then the
-- MCP Hub screen was empty with no explanation. The transport, command, args,
-- env and url columns are all read now -- the in-memory version stored the same
-- fields and never looked at any of them.

CREATE TABLE IF NOT EXISTS mcp_servers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    transport   TEXT NOT NULL DEFAULT 'stdio',
    command     TEXT NOT NULL DEFAULT '',
    -- JSON arrays/objects rather than native arrays: args is ordered and env is
    -- a map, and both are handed straight to the process spawn without the
    -- store needing to understand either.
    args        JSONB NOT NULL DEFAULT '[]'::jsonb,
    env         JSONB NOT NULL DEFAULT '{}'::jsonb,
    url         TEXT NOT NULL DEFAULT '',
    tools_count INTEGER NOT NULL DEFAULT 0,
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- The env column can hold credentials -- an API key for a hosted MCP server, a
-- bearer token as an HTTP header. Noted here so it is obvious this table is
-- sensitive; the API projection redacts it on the way out.
COMMENT ON COLUMN mcp_servers.env IS
    'Process environment or HTTP headers. May contain credentials; redacted by the API.';
