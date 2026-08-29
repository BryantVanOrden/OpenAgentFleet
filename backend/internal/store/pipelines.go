package store

import (
	"context"
	"encoding/json"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Pipelines, made durable.
//
// Runs deliberately stay in memory: a run is execution state, the tasks it
// starts are already persisted rows, and a half-finished run resumed after a
// restart would have no runner behind it.

func (s *Store) UpsertPipeline(ctx context.Context, p protocol.WorkflowPipeline) error {
	// Marshalled explicitly rather than through orEmptySlice, which is for
	// strings. A nil slice must still store as [] so a reload gets a list.
	if p.Nodes == nil {
		p.Nodes = []protocol.PipelineNode{}
	}
	if p.Edges == nil {
		p.Edges = []protocol.PipelineEdge{}
	}
	nodes, err := json.Marshal(p.Nodes)
	if err != nil {
		return err
	}
	edges, err := json.Marshal(p.Edges)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO pipelines(id,name,description,nodes_json,edges_json,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7)
         ON CONFLICT (id) DO UPDATE SET name=$2,description=$3,nodes_json=$4,edges_json=$5,updated_at=$7`,
		p.ID, p.Name, p.Description, string(nodes), string(edges), p.CreatedAt, p.UpdatedAt)
	return norm(err)
}

func (s *Store) ListPipelines(ctx context.Context) ([]protocol.WorkflowPipeline, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,name,description,nodes_json,edges_json,created_at,updated_at
         FROM pipelines ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.WorkflowPipeline{}
	for rows.Next() {
		var p protocol.WorkflowPipeline
		var nodes, edges string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &nodes, &edges,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(nodes), &p.Nodes)
		_ = json.Unmarshal([]byte(edges), &p.Edges)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeletePipeline(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM pipelines WHERE id=$1`, id)
	return norm(err)
}
