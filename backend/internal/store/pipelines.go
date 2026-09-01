package store

import (
	"context"
	"encoding/json"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Pipelines and their runs, made durable.
//
// Runs used to stay in memory on the theory that a half-finished run resumed
// after a restart would have no runner behind it. That was true of the boot
// order, not of the problem: the engine gets its node runner attached at boot
// anyway, so resume just has to happen after that. What the in-memory version
// actually delivered was runs recorded as `running` forever whenever the
// orchestrator restarted, with every settled node result lost.

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

// ------------------------------------------------------------------- runs ---

func (s *Store) UpsertPipelineRun(ctx context.Context, r protocol.PipelineRun) error {
	if r.NodeResults == nil {
		r.NodeResults = map[string]string{}
	}
	if r.NodeStates == nil {
		r.NodeStates = map[string]string{}
	}
	results, err := json.Marshal(r.NodeResults)
	if err != nil {
		return err
	}
	states, err := json.Marshal(r.NodeStates)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO pipeline_runs(id,pipeline_id,status,node_results,node_states,started_at,finished_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7)
         ON CONFLICT (id) DO UPDATE SET status=$3,node_results=$4,node_states=$5,finished_at=$7`,
		r.ID, r.PipelineID, r.Status, string(results), string(states), r.StartedAt, r.FinishedAt)
	return norm(err)
}

// ListPipelineRuns returns the most recent runs, newest first.
func (s *Store) ListPipelineRuns(ctx context.Context, limit int) ([]protocol.PipelineRun, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id,pipeline_id,status,node_results,node_states,started_at,finished_at
           FROM pipeline_runs ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.PipelineRun{}
	for rows.Next() {
		var r protocol.PipelineRun
		var results, states string
		if err := rows.Scan(&r.ID, &r.PipelineID, &r.Status, &results, &states,
			&r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(results), &r.NodeResults)
		_ = json.Unmarshal([]byte(states), &r.NodeStates)
		out = append(out, r)
	}
	return out, rows.Err()
}
