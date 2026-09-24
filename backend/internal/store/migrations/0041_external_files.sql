-- The files an agent on a PC shared with the fleet, and the version of each
-- it last had. When a colleague publishes a newer version, the agent's next
-- run writes it into the agent's folder first: a desktop's fix to a file
-- Claude Code wrote used to stay in the catalog and never reach the folder.
CREATE TABLE IF NOT EXISTS external_files (
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    item_id     TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    version     INT  NOT NULL,
    PRIMARY KEY (instance_id, item_id)
);
