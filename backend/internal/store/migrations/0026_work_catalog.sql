-- A shared catalog the agents publish work into.
--
-- Agents could message each other and share credentials, but had nowhere to
-- put the work itself. Anything one bot produced lived in its own container
-- and died with it, so a second bot asked to build on it had to be told what
-- to rebuild rather than handed the thing.
--
-- Three kinds, which differ in what the app does with them rather than in how
-- they are stored:
--   file      -- text: notes, code, data. Shown as text.
--   app       -- a self-contained HTML document the app renders and runs.
--   workspace -- a manifest naming other items that belong together.
CREATE TABLE IF NOT EXISTS work_items (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'file',
    description TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL DEFAULT '',
    mime        TEXT NOT NULL DEFAULT 'text/plain',

    -- Who made it. An instance id for a bot, empty for the operator, so the
    -- catalog says where a thing came from without a second lookup.
    created_by      TEXT NOT NULL DEFAULT '',
    created_by_name TEXT NOT NULL DEFAULT '',

    -- Which department may see it. Empty is admin-only, matching how an
    -- unfiled secret behaves: work nobody has scoped is not everybody's.
    org_id TEXT,

    -- A workspace groups items; children point at their parent. Dropping the
    -- workspace drops what was only ever part of it.
    parent_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,

    -- Bumped on every write, so a bot can tell whether what it is looking at
    -- is what it last read. Two agents editing the same item is the normal
    -- case here, not the exception.
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS work_items_parent_idx ON work_items(parent_id);
CREATE INDEX IF NOT EXISTS work_items_org_idx ON work_items(org_id);
CREATE INDEX IF NOT EXISTS work_items_kind_idx ON work_items(kind);
