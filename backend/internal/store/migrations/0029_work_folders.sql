-- Put the catalog into folders.
--
-- Everything an agent published landed at the top level, so a fleet that had
-- worked on eight small apps showed fifty-two entries in one flat list: an app
-- called "dice" sitting beside dice-spec, dice-defects-v1, dice-defects-v2,
-- dice-review-findings and four more, in alphabetical order, with nothing to
-- say they were the same piece of work.
--
-- The catalog already had the mechanism -- parent_id, and a "workspace" kind --
-- and nothing ever used it.
--
-- The rule is the one a person would use reading the list: a file belongs to
-- an app when its name starts with the app's, followed by a separator.
-- dice-spec goes with dice; convtest_review.md goes with convtest; "convert"
-- and "convtest" stay apart because neither is a prefix of the other at a
-- separator boundary. Where more than one app could claim a file the longest
-- name wins, so an app called "dice-roller" would take dice-roller-findings
-- ahead of "dice".
--
-- Apps with nothing filed against them stay where they are. A folder holding
-- one item is a click, not an organisation.

-- Which app, if any, owns each top-level file.
CREATE TEMP TABLE work_owner ON COMMIT DROP AS
WITH apps AS (
    SELECT id, name, org_id
      FROM work_items
     WHERE kind = 'app'
       AND COALESCE(parent_id, '') = ''
)
SELECT DISTINCT ON (child.id)
       child.id   AS child_id,
       app.id     AS app_id,
       app.name   AS app_name,
       app.org_id AS org_id
  FROM work_items child
  JOIN apps app
    ON child.id <> app.id
   AND left(child.name, length(app.name)) = app.name
   AND substr(child.name, length(app.name) + 1, 1) IN ('-', '_', '.', ' ')
 WHERE child.kind <> 'workspace'
   AND COALESCE(child.parent_id, '') = ''
 ORDER BY child.id, length(app.name) DESC;

-- One workspace per app that actually has something filed against it.
CREATE TEMP TABLE work_folder ON COMMIT DROP AS
SELECT gen_random_uuid()::text AS folder_id,
       o.app_id,
       o.app_name,
       o.org_id
  FROM (SELECT DISTINCT app_id, app_name, org_id FROM work_owner) o;

INSERT INTO work_items (id, name, kind, description, content, mime,
                        created_by, created_by_name, org_id, parent_id,
                        version, created_at, updated_at)
SELECT f.folder_id,
       f.app_name,
       'workspace',
       'Everything published for ' || f.app_name || '.',
       '',
       'text/plain',
       COALESCE((SELECT created_by      FROM work_items WHERE id = f.app_id), ''),
       COALESCE((SELECT created_by_name FROM work_items WHERE id = f.app_id), ''),
       f.org_id,
       NULL,
       1,
       COALESCE((SELECT created_at FROM work_items WHERE id = f.app_id), now()),
       now()
  FROM work_folder f;

-- The app goes in its own folder, with everything filed against it.
UPDATE work_items w
   SET parent_id = f.folder_id, updated_at = now()
  FROM work_folder f
 WHERE w.id = f.app_id;

UPDATE work_items w
   SET parent_id = f.folder_id, updated_at = now()
  FROM work_owner o
  JOIN work_folder f ON f.app_id = o.app_id
 WHERE w.id = o.child_id;
