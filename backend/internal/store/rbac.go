package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Orgs, membership and per-bot grants.

func (s *Store) UpsertOrg(ctx context.Context, o *protocol.Org) error {
	if o.ID == "" {
		o.ID = NewID()
		o.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO orgs(id,name,description,created_at)
         VALUES ($1,$2,$3,COALESCE($4, now()))
         ON CONFLICT (id) DO UPDATE SET name=$2,description=$3`,
		o.ID, o.Name, o.Description, nullTime(o.CreatedAt))
	return norm(err)
}

// ListOrgs returns every org with its member and bot counts.
func (s *Store) ListOrgs(ctx context.Context) ([]protocol.Org, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT o.id, o.name, o.description, o.created_at,
                (SELECT count(*) FROM org_members m WHERE m.org_id = o.id),
                (SELECT count(*) FROM instance_orgs io WHERE io.org_id = o.id)
         FROM orgs o ORDER BY o.name`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.Org{}
	for rows.Next() {
		var o protocol.Org
		if err := rows.Scan(&o.ID, &o.Name, &o.Description, &o.CreatedAt,
			&o.MemberCount, &o.BotCount); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) DeleteOrg(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Bots, secrets and sessions are unassigned rather than deleted. Removing
	// a department must not destroy running machines or the credentials other
	// work depends on; they become admin-only until reassigned.
	//
	// A bot's membership lives in instance_orgs and is removed by that table's
	// cascade, which also does the right thing for a bot shared with several
	// departments: it leaves this one and keeps the others.
	for _, q := range []string{
		`UPDATE shared_secrets  SET org_id=NULL WHERE org_id=$1`,
		`UPDATE shared_sessions SET org_id=NULL WHERE org_id=$1`,
	} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return norm(err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, id); err != nil {
		return norm(err)
	}
	return norm(tx.Commit(ctx))
}

func (s *Store) UpsertOrgMember(ctx context.Context, m protocol.OrgMember) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO org_members(org_id,user_id,org_role,created_at)
         VALUES ($1,$2,$3, now())
         ON CONFLICT (org_id,user_id) DO UPDATE SET org_role=$3`,
		m.OrgID, m.UserID, string(m.OrgRole))
	return norm(err)
}

func (s *Store) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM org_members WHERE org_id=$1 AND user_id=$2`, orgID, userID)
	return norm(err)
}

// ListOrgMembers returns an org's members with their email, so the UI does not
// have to join every member against the user list itself.
func (s *Store) ListOrgMembers(ctx context.Context, orgID string) ([]protocol.OrgMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.org_id, m.user_id, COALESCE(u.email,''), m.org_role, m.created_at
         FROM org_members m
         LEFT JOIN users u ON u.id = m.user_id
         WHERE m.org_id=$1 ORDER BY u.email`, orgID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.OrgMember{}
	for rows.Next() {
		var m protocol.OrgMember
		var role string
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Email, &role, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.OrgRole = protocol.OrgRole(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetBotGrant records a per-bot exception. A nil permission list removes the
// grant, which is different from an empty one: removing falls back to the org
// default, empty explicitly allows nothing.
func (s *Store) SetBotGrant(ctx context.Context, g protocol.BotGrant) error {
	if g.Permissions == nil {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM bot_grants WHERE user_id=$1 AND instance_id=$2`,
			g.UserID, g.InstanceID)
		return norm(err)
	}
	blob, err := json.Marshal(g.Permissions)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO bot_grants(user_id,instance_id,permissions,created_at)
         VALUES ($1,$2,$3, now())
         ON CONFLICT (user_id,instance_id) DO UPDATE SET permissions=$3`,
		g.UserID, g.InstanceID, string(blob))
	return norm(err)
}

func (s *Store) ListBotGrants(ctx context.Context, instanceID string) ([]protocol.BotGrant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, instance_id, permissions, created_at
         FROM bot_grants WHERE instance_id=$1`, instanceID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.BotGrant{}
	for rows.Next() {
		var g protocol.BotGrant
		var blob string
		if err := rows.Scan(&g.UserID, &g.InstanceID, &blob, &g.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(blob), &g.Permissions)
		if g.Permissions == nil {
			g.Permissions = []protocol.Permission{}
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// AccessFor resolves everything needed to answer permission questions for one
// user, in two queries rather than one per check.
func (s *Store) AccessFor(ctx context.Context, userID string, globalAdmin bool) (protocol.Access, error) {
	acc := protocol.Access{
		UserID:      userID,
		GlobalAdmin: globalAdmin,
		OrgRoles:    map[string]protocol.OrgRole{},
		Grants:      map[string][]protocol.Permission{},
	}

	rows, err := s.pool.Query(ctx,
		`SELECT org_id, org_role FROM org_members WHERE user_id=$1`, userID)
	if err != nil {
		return acc, norm(err)
	}
	for rows.Next() {
		var orgID, role string
		if err := rows.Scan(&orgID, &role); err != nil {
			rows.Close()
			return acc, err
		}
		acc.OrgRoles[orgID] = protocol.OrgRole(role)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return acc, err
	}

	grows, err := s.pool.Query(ctx,
		`SELECT instance_id, permissions FROM bot_grants WHERE user_id=$1`, userID)
	if err != nil {
		return acc, norm(err)
	}
	defer grows.Close()
	for grows.Next() {
		var instanceID, blob string
		if err := grows.Scan(&instanceID, &blob); err != nil {
			return acc, err
		}
		var perms []protocol.Permission
		_ = json.Unmarshal([]byte(blob), &perms)
		if perms == nil {
			// An empty grant is meaningful — it hides the bot — so it must not
			// decay into "no grant", which would fall back to the org default.
			perms = []protocol.Permission{}
		}
		acc.Grants[instanceID] = perms
	}
	return acc, grows.Err()
}

// SetInstanceOrgs replaces the set of departments a bot belongs to.
//
// The whole set at once, in one transaction: applying a diff row by row leaves
// a window where the bot is in neither the old department nor the new one, and
// anything checking permissions in that window gets the wrong answer.
func (s *Store) SetInstanceOrgs(ctx context.Context, instanceID string, orgIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM instance_orgs WHERE instance_id=$1`, instanceID); err != nil {
		return norm(err)
	}
	for _, orgID := range orgIDs {
		if strings.TrimSpace(orgID) == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO instance_orgs(instance_id,org_id) VALUES ($1,$2)
			 ON CONFLICT DO NOTHING`, instanceID, orgID); err != nil {
			return norm(err)
		}
	}
	return norm(tx.Commit(ctx))
}

// InstanceOrgs returns the departments each of the given bots belongs to.
//
// One query for the whole fleet rather than one per bot: every list endpoint
// resolves this before filtering, and a query per row would make membership
// the slowest thing in the API.
func (s *Store) InstanceOrgs(ctx context.Context) (map[string][]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT instance_id, org_id FROM instance_orgs ORDER BY org_id`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var instanceID, orgID string
		if err := rows.Scan(&instanceID, &orgID); err != nil {
			return nil, err
		}
		out[instanceID] = append(out[instanceID], orgID)
	}
	return out, rows.Err()
}
