package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Swarm missions, made durable.
//
// The coordinator kept everything in a map: a mission, its blackboard and its
// peer reviews all vanished on the next deploy. A review that does not survive a
// restart is not a review, and the artifact it approved goes back to being
// unverified without anyone being told.

func (s *Store) UpsertSwarm(ctx context.Context, sw protocol.SwarmTeam) error {
	// orEmptyJSON throughout: the columns are NOT NULL, and a nil slice
	// marshals to "null", which fails the constraint on the first swarm created
	// with no artifacts — which is every swarm, at creation.
	members, err := orEmptyJSON(sw.Members)
	if err != nil {
		return err
	}
	messages, err := orEmptyJSON(sw.Messages)
	if err != nil {
		return err
	}
	artifacts, err := orEmptyJSON(sw.Artifacts)
	if err != nil {
		return err
	}

	if sw.CreatedAt.IsZero() {
		sw.CreatedAt = time.Now().UTC()
	}
	sw.UpdatedAt = time.Now().UTC()

	_, err = s.pool.Exec(ctx,
		`INSERT INTO swarms(id,name,mission,status,phase,plan_first,members,messages,artifacts,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
         ON CONFLICT (id) DO UPDATE SET name=$2,mission=$3,status=$4,phase=$5,plan_first=$6,
             members=$7,messages=$8,artifacts=$9,updated_at=$11`,
		sw.ID, sw.Name, sw.Mission, string(sw.Status), sw.Phase, sw.PlanFirst,
		members, messages, artifacts, sw.CreatedAt, sw.UpdatedAt)
	return norm(err)
}

func (s *Store) ListSwarms(ctx context.Context) ([]protocol.SwarmTeam, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,name,mission,status,COALESCE(phase,'execution'),COALESCE(plan_first,false),
                members,messages,artifacts,created_at,updated_at
           FROM swarms ORDER BY created_at DESC`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := make([]protocol.SwarmTeam, 0)
	for rows.Next() {
		var sw protocol.SwarmTeam
		var status, members, messages, artifacts string
		if err := rows.Scan(&sw.ID, &sw.Name, &sw.Mission, &status, &sw.Phase, &sw.PlanFirst,
			&members, &messages, &artifacts, &sw.CreatedAt, &sw.UpdatedAt); err != nil {
			return nil, err
		}
		sw.Status = protocol.SwarmStatus(status)
		// A row with one unreadable column comes back missing that column
		// rather than failing the whole list: one bad swarm should not empty
		// the screen.
		_ = json.Unmarshal([]byte(members), &sw.Members)
		_ = json.Unmarshal([]byte(messages), &sw.Messages)
		_ = json.Unmarshal([]byte(artifacts), &sw.Artifacts)
		out = append(out, sw)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSwarm(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM swarms WHERE id=$1`, id)
	return norm(err)
}

// orEmptyJSON marshals a slice, turning nil into [] rather than null.
func orEmptyJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return "[]", nil
	}
	return string(raw), nil
}
