package store

import (
	"context"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Shared secrets and browser sessions, persisted.
//
// These deliberately stayed in memory for a while: they hold credential
// plaintext and live cookie jars, and writing those to an unencrypted table is
// a decision to make explicitly rather than acquire by accident. The operator
// has now made it — the fleet is single-tenant on their own hardware, and a
// shared secret that evaporates on every deploy is worse than one at rest.
//
// The tables have existed since the shared-vault migration; nothing read them.

func (s *Store) UpsertSharedSecret(ctx context.Context, sec protocol.SharedSecret) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO shared_secrets(key,value,scope,note,created_by,updated_at,org_id)
         VALUES ($1,$2,$3,$4,$5,$6,$7)
         ON CONFLICT (key) DO UPDATE SET value=$2,scope=$3,note=$4,created_by=$5,updated_at=$6,org_id=$7`,
		sec.Key, sec.Value, sec.Scope, sec.Note, sec.CreatedBy, time.Now().UTC(), nullIfEmpty(sec.OrgID))
	return norm(err)
}

func (s *Store) ListSharedSecrets(ctx context.Context) ([]protocol.SharedSecret, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key,value,COALESCE(scope,'fleet'),COALESCE(note,''),COALESCE(created_by,''),updated_at,COALESCE(org_id,'')
         FROM shared_secrets ORDER BY key`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.SharedSecret{}
	for rows.Next() {
		var sec protocol.SharedSecret
		if err := rows.Scan(&sec.Key, &sec.Value, &sec.Scope, &sec.Note,
			&sec.CreatedBy, &sec.UpdatedAt, &sec.OrgID); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSharedSecret(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM shared_secrets WHERE key=$1`, key)
	return norm(err)
}

func (s *Store) UpsertSharedSession(ctx context.Context, sess protocol.SharedSession) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO shared_sessions(id,domain,title,cookies_json,local_storage_json,created_by_instance,created_at,org_id)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
         ON CONFLICT (id) DO UPDATE SET domain=$2,title=$3,cookies_json=$4,
             local_storage_json=$5,created_by_instance=$6,org_id=$8`,
		sess.ID, sess.Domain, sess.Title, sess.CookiesJSON, sess.LocalStorageJSON,
		sess.CreatedByInstance, sess.CreatedAt, nullIfEmpty(sess.OrgID))
	return norm(err)
}

func (s *Store) ListSharedSessions(ctx context.Context) ([]protocol.SharedSession, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,domain,title,cookies_json,COALESCE(local_storage_json,''),
                COALESCE(created_by_instance,''),created_at,COALESCE(org_id,'')
         FROM shared_sessions ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.SharedSession{}
	for rows.Next() {
		var sess protocol.SharedSession
		if err := rows.Scan(&sess.ID, &sess.Domain, &sess.Title, &sess.CookiesJSON,
			&sess.LocalStorageJSON, &sess.CreatedByInstance, &sess.CreatedAt, &sess.OrgID); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}
