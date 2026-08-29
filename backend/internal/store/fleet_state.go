package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// This file backs the two fleet-wide subsystems that used to live only in
// process memory: the inter-agent message bus (vault.Bus) and episodic memory
// (memory.Engine). Both keep their in-process working set for speed; these
// methods are the write-through and the rehydration on boot, so a restart no
// longer erases everything the fleet learned or said.

// ----------------------------------------------------------- peer messages ---

func (s *Store) InsertPeerMessage(ctx context.Context, m protocol.PeerMessage) error {
	var data any
	if len(m.Data) > 0 {
		blob, err := json.Marshal(m.Data)
		if err != nil {
			return err
		}
		data = string(blob)
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO peer_messages(id,from_instance_id,from_instance_name,to_instance_id,kind,content,data_json,created_at,conversation_id,compacted)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
         ON CONFLICT (id) DO NOTHING`,
		m.ID, m.FromInstanceID, m.FromInstanceName, m.ToInstanceID, m.Kind, m.Content, data, m.CreatedAt,
		nullIfEmpty(m.ConversationID), m.Compacted)
	return norm(err)
}

// ListPeerMessages returns the most recent messages visible to instanceID
// (empty means the whole fleet), oldest first.
//
// Oldest-first matters: the caller replays them into an append-ordered slice
// and then walks it backwards for "most recent N", so reversing here would
// hand back the oldest messages labelled as the newest.
func (s *Store) ListPeerMessages(ctx context.Context, instanceID string, limit int) ([]protocol.PeerMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id,from_instance_id,from_instance_name,to_instance_id,kind,content,COALESCE(data_json,''),created_at,COALESCE(conversation_id,''),compacted
          FROM peer_messages`
	args := []any{limit}
	if instanceID != "" {
		q += ` WHERE to_instance_id='broadcast' OR to_instance_id=$2 OR from_instance_id=$2`
		args = append(args, instanceID)
	}
	q = `SELECT * FROM (` + q + ` ORDER BY created_at DESC, id DESC LIMIT $1) recent ORDER BY created_at, id`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := make([]protocol.PeerMessage, 0)
	for rows.Next() {
		var m protocol.PeerMessage
		var data string
		if err := rows.Scan(&m.ID, &m.FromInstanceID, &m.FromInstanceName, &m.ToInstanceID,
			&m.Kind, &m.Content, &data, &m.CreatedAt, &m.ConversationID, &m.Compacted); err != nil {
			return nil, err
		}
		if data != "" {
			_ = json.Unmarshal([]byte(data), &m.Data)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ------------------------------------------------------- episodic memories ---

// UpsertMemory persists one episodic memory, embedding included.
//
// The embedding is stored as JSON rather than recomputed on load: it is a
// hashed bag-of-words vector, so a change to the hashing would otherwise leave
// old and new records in incompatible spaces without anything saying so. If
// the column is empty or unreadable the engine recomputes on load instead.
func (s *Store) UpsertMemory(ctx context.Context, m protocol.MemoryRecord) error {
	tags, err := json.Marshal(orEmptySlice(m.Tags))
	if err != nil {
		return err
	}
	embedding, err := json.Marshal(m.Embedding)
	if err != nil {
		return err
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO episodic_memories(id,namespace,title,content,tags,embedding,source_task_id,source_instance_id,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
         ON CONFLICT (id) DO UPDATE SET namespace=$2,title=$3,content=$4,tags=$5,embedding=$6,
             source_task_id=$7,source_instance_id=$8`,
		m.ID, m.Namespace, m.Title, m.Content, string(tags), string(embedding),
		m.SourceTaskID, m.SourceInstanceID, m.CreatedAt)
	return norm(err)
}

// ListMemories returns the most recent memories, newest first. An empty
// namespace means every namespace.
func (s *Store) ListMemories(ctx context.Context, namespace string, limit int) ([]protocol.MemoryRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT id,namespace,title,content,COALESCE(tags,''),COALESCE(embedding,''),
             COALESCE(source_task_id,''),COALESCE(source_instance_id,''),created_at
          FROM episodic_memories`
	args := []any{limit}
	if namespace != "" {
		q += ` WHERE namespace=$2`
		args = append(args, namespace)
	}
	q += ` ORDER BY created_at DESC LIMIT $1`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := make([]protocol.MemoryRecord, 0)
	for rows.Next() {
		var m protocol.MemoryRecord
		var tags, embedding string
		if err := rows.Scan(&m.ID, &m.Namespace, &m.Title, &m.Content, &tags, &embedding,
			&m.SourceTaskID, &m.SourceInstanceID, &m.CreatedAt); err != nil {
			return nil, err
		}
		if tags != "" {
			_ = json.Unmarshal([]byte(tags), &m.Tags)
		}
		if embedding != "" {
			_ = json.Unmarshal([]byte(embedding), &m.Embedding)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM episodic_memories WHERE id=$1`, id)
	return norm(err)
}

// nullIfEmpty keeps optional text columns NULL rather than empty-string, so
// "unassigned" is one value in the database instead of two.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
