-- A bot can belong to more than one department.
--
-- instances.org_id held exactly one, so a bot two teams both rely on had to be
-- filed under one of them and be invisible to the other, or duplicated. Support
-- and engineering sharing a triage bot is not an edge case, it is the normal
-- shape of a company.
--
-- The join table is the truth. The old column is copied in and then dropped
-- rather than kept in step: two places recording the same fact is how they end
-- up disagreeing, and the disagreement here decides who can see a machine.
CREATE TABLE IF NOT EXISTS instance_orgs (
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    PRIMARY KEY (instance_id, org_id)
);

CREATE INDEX IF NOT EXISTS instance_orgs_org_idx ON instance_orgs(org_id);

-- Carry every existing assignment across. NULL and '' both mean unassigned,
-- which stays unassigned: a bot in no department is visible only to a global
-- admin, and inventing a department for it would hand it to someone.
INSERT INTO instance_orgs (instance_id, org_id)
SELECT id, org_id FROM instances
 WHERE org_id IS NOT NULL AND org_id <> ''
   AND EXISTS (SELECT 1 FROM orgs o WHERE o.id = instances.org_id)
ON CONFLICT DO NOTHING;

ALTER TABLE instances DROP COLUMN IF EXISTS org_id;
