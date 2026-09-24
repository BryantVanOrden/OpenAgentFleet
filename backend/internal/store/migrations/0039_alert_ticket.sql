-- An alert the ticket engine raises names its ticket, so the alert closes
-- when the ticket moves on: it starts running, finishes, is cancelled or is
-- deleted. Before this nothing closed them, and a fleet collected "Nobody
-- owns T-12" for tickets that had long since been assigned or deleted.
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ticket_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS alerts_open_ticket_idx ON alerts (ticket_id) WHERE resolved_at IS NULL;

-- Older ticket alerts carry their ticket only in the title ("T-12 waits
-- for Checker: …"). Tie them to it where the ticket still exists.
UPDATE alerts a SET ticket_id = t.id
  FROM tickets t
 WHERE a.ticket_id = '' AND a.kind = 'stalled' AND a.resolved_at IS NULL
   AND a.title ~ ('(^|[^0-9A-Za-z])T-' || t.number || '([^0-9]|$)');

-- Then close the ones whose ticket has moved on, and the ones that name a
-- ticket that no longer exists.
UPDATE alerts a SET resolved_at = now(), reply = 'Closed: the ticket has moved on.'
  FROM tickets t
 WHERE a.ticket_id = t.id AND a.resolved_at IS NULL
   AND t.status IN ('done', 'cancelled', 'in_progress');
UPDATE alerts SET resolved_at = now(), reply = 'Closed: the ticket no longer exists.'
 WHERE resolved_at IS NULL AND kind = 'stalled' AND ticket_id = ''
   AND title ~ '(^|[^0-9A-Za-z])T-[0-9]+([^0-9]|$)';
