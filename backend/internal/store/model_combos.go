package store

import (
	"context"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Model combinations: which model does what.

func (s *Store) UpsertModelCombo(ctx context.Context, c *protocol.ModelCombo) error {
	if c.ID == "" {
		c.ID = NewID()
		c.CreatedAt = time.Now().UTC()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO model_combos(id,name,description,created_at)
         VALUES ($1,$2,$3,COALESCE($4, now()))
         ON CONFLICT (id) DO UPDATE SET name=$2,description=$3`,
		c.ID, c.Name, c.Description, nullTime(c.CreatedAt)); err != nil {
		return norm(err)
	}

	// Roles are replaced rather than merged: the assignment is the object, and
	// a leftover row would keep routing a role to a model the operator removed.
	if _, err := tx.Exec(ctx,
		`DELETE FROM model_combo_roles WHERE combo_id=$1`, c.ID); err != nil {
		return norm(err)
	}
	for role, provider := range c.Roles {
		if role == "" || provider == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO model_combo_roles(combo_id,role,provider_id) VALUES ($1,$2,$3)`,
			c.ID, role, provider); err != nil {
			return norm(err)
		}
	}
	return norm(tx.Commit(ctx))
}

func (s *Store) ListModelCombos(ctx context.Context) ([]protocol.ModelCombo, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,name,description,created_at FROM model_combos ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.ModelCombo{}
	byID := map[string]int{}
	for rows.Next() {
		var c protocol.ModelCombo
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Roles = map[string]string{}
		byID[c.ID] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rrows, err := s.pool.Query(ctx,
		`SELECT combo_id,role,provider_id FROM model_combo_roles`)
	if err != nil {
		return nil, norm(err)
	}
	defer rrows.Close()
	for rrows.Next() {
		var comboID, role, provider string
		if err := rrows.Scan(&comboID, &role, &provider); err != nil {
			return nil, err
		}
		if i, ok := byID[comboID]; ok {
			out[i].Roles[role] = provider
		}
	}
	return out, rrows.Err()
}

func (s *Store) DeleteModelCombo(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM model_combos WHERE id=$1`, id)
	return norm(err)
}
