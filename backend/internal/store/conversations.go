package store

import (
	"context"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Conversations are the persistent side of fleet comms threads. The bus keeps
// the working set in memory; these are the write-through and the boot reload,
// so a thread the operator created survives a restart.

// UpsertConversation writes a conversation and replaces its member list.
func (s *Store) UpsertConversation(ctx context.Context, c protocol.Conversation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO conversations(id,kind,title,created_at) VALUES ($1,$2,$3,$4)
         ON CONFLICT (id) DO UPDATE SET kind=$2,title=$3`,
		c.ID, c.Kind, c.Title, c.CreatedAt); err != nil {
		return norm(err)
	}

	// Members are replaced rather than merged: the member list is the identity
	// of the thread, and a stale row would silently widen who can read it.
	if _, err := tx.Exec(ctx,
		`DELETE FROM conversation_members WHERE conversation_id=$1`, c.ID); err != nil {
		return norm(err)
	}
	for _, m := range c.Members {
		if _, err := tx.Exec(ctx,
			`INSERT INTO conversation_members(conversation_id,member_id) VALUES ($1,$2)
             ON CONFLICT DO NOTHING`, c.ID, m); err != nil {
			return norm(err)
		}
	}
	return norm(tx.Commit(ctx))
}

// ListConversations returns every stored conversation with its members.
func (s *Store) ListConversations(ctx context.Context) ([]protocol.Conversation, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,kind,title,created_at FROM conversations ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := make([]protocol.Conversation, 0)
	byID := map[string]int{}
	for rows.Next() {
		var c protocol.Conversation
		if err := rows.Scan(&c.ID, &c.Kind, &c.Title, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Members = []string{}
		byID[c.ID] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mrows, err := s.pool.Query(ctx,
		`SELECT conversation_id,member_id FROM conversation_members ORDER BY member_id`)
	if err != nil {
		return nil, norm(err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var convID, member string
		if err := mrows.Scan(&convID, &member); err != nil {
			return nil, err
		}
		if i, ok := byID[convID]; ok {
			out[i].Members = append(out[i].Members, member)
		}
	}
	return out, mrows.Err()
}

// DeleteConversation removes a thread. Its messages are kept: deleting a thread
// tidies the comms screen, and silently destroying the record of what agents
// said to each other is not something a tidy-up should do. They become
// unassigned and stop being listed.
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE peer_messages SET conversation_id=NULL WHERE conversation_id=$1`, id); err != nil {
		return norm(err)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, id)
	return norm(err)
}

// CompactConversation marks every message in a thread up to and including
// throughID as replaced by a summary. The rows are kept: the summary is a
// lossy view, and an operator asking "what did they actually say" after the
// fact must still be able to find out.
func (s *Store) CompactConversation(ctx context.Context, conversationID, throughID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE peer_messages SET compacted=TRUE
         WHERE conversation_id=$1 AND compacted=FALSE
           AND (created_at, id) <= (SELECT created_at, id FROM peer_messages WHERE id=$2)`,
		conversationID, throughID)
	return norm(err)
}
