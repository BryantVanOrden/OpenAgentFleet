package store

import (
	"context"
	"time"
)

// Chats with a single bot.
//
// One flat history per instance meant there was no way to start a fresh chat
// or clear one that had gone wrong without destroying every conversation you
// had ever had with that agent.

// ChatSession is one chat thread with a bot.
type ChatSession struct {
	ID         string    `json:"id"`
	InstanceID string    `json:"instance_id"`
	Title      string    `json:"title"`
	Pinned     bool      `json:"pinned"`
	CreatedAt  time.Time `json:"created_at"`

	// Filled in on read, not stored.
	MessageCount  int       `json:"message_count"`
	LastMessageAt time.Time `json:"last_message_at,omitzero"`
}

// DefaultChatSessionID is the chat that messages recorded before sessions
// existed belong to. It is not a row: it stands for "session_id IS NULL", so
// old history stays reachable without a backfill that would have to guess
// where one conversation ended and the next began.
const DefaultChatSessionID = "default"

func (s *Store) CreateChatSession(ctx context.Context, instanceID, title string) (ChatSession, error) {
	c := ChatSession{
		ID:         NewID(),
		InstanceID: instanceID,
		Title:      title,
		CreatedAt:  time.Now().UTC(),
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_sessions(id,instance_id,title,created_at) VALUES ($1,$2,$3,$4)`,
		c.ID, c.InstanceID, c.Title, c.CreatedAt)
	return c, norm(err)
}

// ListChatSessions returns a bot's chats, newest activity first, always
// including the default chat so earlier history is never orphaned.
func (s *Store) ListChatSessions(ctx context.Context, instanceID string) ([]ChatSession, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT s.id, s.instance_id, s.title, s.pinned, s.created_at,
                COALESCE(m.n, 0), COALESCE(m.last_at, s.created_at)
         FROM chat_sessions s
         LEFT JOIN (
             SELECT session_id, count(*) AS n, max(created_at) AS last_at
             FROM chat_messages WHERE session_id IS NOT NULL GROUP BY session_id
         ) m ON m.session_id = s.id
         WHERE s.instance_id=$1
         ORDER BY s.pinned DESC, COALESCE(m.last_at, s.created_at) DESC`, instanceID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []ChatSession{}
	for rows.Next() {
		var c ChatSession
		if err := rows.Scan(&c.ID, &c.InstanceID, &c.Title, &c.Pinned, &c.CreatedAt,
			&c.MessageCount, &c.LastMessageAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The default chat is listed only when it actually holds something, so a
	// bot created after this migration does not show an empty phantom chat.
	var n int
	var last *time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*), max(created_at) FROM chat_messages
         WHERE instance_id=$1 AND session_id IS NULL`, instanceID).Scan(&n, &last); err != nil {
		return nil, norm(err)
	}
	if n > 0 {
		d := ChatSession{
			ID:           DefaultChatSessionID,
			InstanceID:   instanceID,
			Title:        "Earlier chat",
			MessageCount: n,
		}
		if last != nil {
			d.CreatedAt, d.LastMessageAt = *last, *last
		}
		out = append(out, d)
	}
	return out, nil
}

// DeleteChatSession removes a chat and the messages in it.
//
// Unlike a fleet conversation, this is the operator's own chat with one bot
// and "delete" is asked for explicitly: keeping the messages would mean the
// chat you just deleted still shaping what the agent says next.
func (s *Store) DeleteChatSession(ctx context.Context, instanceID, sessionID string) error {
	if sessionID == DefaultChatSessionID {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM chat_messages WHERE instance_id=$1 AND session_id IS NULL`, instanceID)
		return norm(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM chat_messages WHERE session_id=$1`, sessionID); err != nil {
		return norm(err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM chat_sessions WHERE id=$1 AND instance_id=$2`,
		sessionID, instanceID); err != nil {
		return norm(err)
	}
	return norm(tx.Commit(ctx))
}

// ChatSessionExists reports whether a session belongs to this instance.
func (s *Store) ChatSessionExists(ctx context.Context, instanceID, sessionID string) (bool, error) {
	if sessionID == "" || sessionID == DefaultChatSessionID {
		return true, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM chat_sessions WHERE id=$1 AND instance_id=$2)`,
		sessionID, instanceID).Scan(&ok)
	return ok, norm(err)
}

// RenameChatSession sets a chat's name.
func (s *Store) RenameChatSession(ctx context.Context, instanceID, sessionID, title string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_sessions SET title=$3 WHERE id=$1 AND instance_id=$2`,
		sessionID, instanceID, title)
	return norm(err)
}

// PinChatSession pins or unpins a chat.
func (s *Store) PinChatSession(ctx context.Context, instanceID, sessionID string, pinned bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_sessions SET pinned=$3 WHERE id=$1 AND instance_id=$2`,
		sessionID, instanceID, pinned)
	return norm(err)
}
