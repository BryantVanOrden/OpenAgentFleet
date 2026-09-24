-- A notification that asks nothing -- a task finished, a window ended, a
-- task failed -- has nothing to resolve. They were stored open and never
-- closed, so "open alerts" filled with them (85 on the demo fleet) and the
-- newest hundred could push an alert that needs an answer out of the list.
-- New ones are stored resolved (store.CreateAlert); these are the old ones.
UPDATE alerts SET resolved_at = created_at
 WHERE resolved_at IS NULL AND needs_reply = FALSE
   AND kind IN ('completed', 'progress', 'failed');
