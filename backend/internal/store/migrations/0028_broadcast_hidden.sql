-- Let the built-in everyone-channel be deleted once another one exists.
--
-- It was refused outright because unaddressed traffic had to land somewhere
-- and it was the only channel that was always there. That stopped being true
-- when operators could open everyone-channels of their own: with a second one
-- present, there is somewhere else for a message to land, and refusing to
-- delete the first is just refusing.
--
-- The built-in channel is implicit -- it has no row unless it has been renamed
-- or pinned -- so "deleted" needs somewhere to live. A row with hidden = true
-- is that record: the channel stops being listed and stops being the default
-- landing place, and nothing has to invent a tombstone table for one flag.
ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS hidden BOOLEAN NOT NULL DEFAULT FALSE;
