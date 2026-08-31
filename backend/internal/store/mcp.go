package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// MCP server registrations, made durable.
//
// The manager keeps its connections and tool catalogues in memory — those are
// live state and cannot be persisted — but which servers exist is
// configuration, and configuration that a restart erases is configuration the
// operator has to re-enter every deploy.

func (s *Store) UpsertMCPServer(ctx context.Context, srv protocol.MCPServer) error {
	args, err := json.Marshal(orEmptySlice(srv.Args))
	if err != nil {
		return err
	}
	// An explicit empty object, not null: the column is NOT NULL and a nil map
	// marshals to "null", which would fail the constraint on the first server
	// registered without env.
	if srv.Env == nil {
		srv.Env = map[string]string{}
	}
	env, err := json.Marshal(srv.Env)
	if err != nil {
		return err
	}
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = time.Now().UTC()
	}
	srv.UpdatedAt = time.Now().UTC()

	_, err = s.pool.Exec(ctx,
		`INSERT INTO mcp_servers(id,name,transport,command,args,env,url,tools_count,active,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
         ON CONFLICT (id) DO UPDATE SET name=$2,transport=$3,command=$4,args=$5,env=$6,
             url=$7,tools_count=$8,active=$9,updated_at=$11`,
		srv.ID, srv.Name, srv.Transport, srv.Command, string(args), string(env),
		srv.URL, srv.ToolsCount, srv.Active, srv.CreatedAt, srv.UpdatedAt)
	return norm(err)
}

func (s *Store) ListMCPServers(ctx context.Context) ([]protocol.MCPServer, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,name,transport,command,args,env,url,tools_count,active,created_at,updated_at
           FROM mcp_servers ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := make([]protocol.MCPServer, 0)
	for rows.Next() {
		var srv protocol.MCPServer
		var args, env string
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Transport, &srv.Command, &args, &env,
			&srv.URL, &srv.ToolsCount, &srv.Active, &srv.CreatedAt, &srv.UpdatedAt); err != nil {
			return nil, err
		}
		// A row whose JSON will not parse is returned without that field rather
		// than failing the whole list: one bad row should not empty the Hub.
		if args != "" {
			_ = json.Unmarshal([]byte(args), &srv.Args)
		}
		if env != "" {
			_ = json.Unmarshal([]byte(env), &srv.Env)
		}
		out = append(out, srv)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM mcp_servers WHERE id=$1`, id)
	return norm(err)
}
