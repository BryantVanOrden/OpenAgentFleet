package vault

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Bus manages the shared fleet secrets vault, browser sessions, and inter-agent P2P messages.
type Bus struct {
	mu       sync.RWMutex
	secrets  map[string]protocol.SharedSecret
	sessions map[string]protocol.SharedSession
	messages []protocol.PeerMessage
}

var GlobalBus = NewBus()

func NewBus() *Bus {
	return &Bus{
		secrets:  make(map[string]protocol.SharedSecret),
		sessions: make(map[string]protocol.SharedSession),
		messages: make([]protocol.PeerMessage, 0),
	}
}

// -------------------------------------------------------------- Shared Secrets ---

func (b *Bus) PutSecret(ctx context.Context, key, value, scope, note, createdBy string) protocol.SharedSecret {
	b.mu.Lock()
	defer b.mu.Unlock()

	sec := protocol.SharedSecret{
		Key:       key,
		Value:     value,
		Scope:     scope,
		Note:      note,
		CreatedBy: createdBy,
		UpdatedAt: time.Now().UTC(),
	}
	if sec.Scope == "" {
		sec.Scope = "fleet"
	}
	b.secrets[key] = sec
	return sec
}

func (b *Bus) GetSecret(ctx context.Context, key string) (protocol.SharedSecret, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	sec, ok := b.secrets[key]
	return sec, ok
}

func (b *Bus) ListSecrets(ctx context.Context) []protocol.SharedSecret {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]protocol.SharedSecret, 0, len(b.secrets))
	for _, s := range b.secrets {
		out = append(out, s)
	}
	return out
}

func (b *Bus) DeleteSecret(ctx context.Context, key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.secrets, key)
}

// ------------------------------------------------------------- Shared Sessions ---

func (b *Bus) SaveSession(ctx context.Context, domain, title, cookiesJSON, localStorageJSON, createdBy string) protocol.SharedSession {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := fmt.Sprintf("sess-%s-%d", domain, time.Now().UnixNano())
	sess := protocol.SharedSession{
		ID:                id,
		Domain:            domain,
		Title:             title,
		CookiesJSON:       cookiesJSON,
		LocalStorageJSON:  localStorageJSON,
		CreatedByInstance: createdBy,
		CreatedAt:         time.Now().UTC(),
	}
	b.sessions[id] = sess
	return sess
}

func (b *Bus) GetSession(ctx context.Context, id string) (protocol.SharedSession, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	sess, ok := b.sessions[id]
	return sess, ok
}

func (b *Bus) ListSessions(ctx context.Context, domain string) []protocol.SharedSession {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]protocol.SharedSession, 0, len(b.sessions))
	for _, s := range b.sessions {
		if domain == "" || s.Domain == domain {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------- Inter-Agent P2P ---

func (b *Bus) SendMessage(ctx context.Context, fromID, fromName, toID, kind, content string, data map[string]any) protocol.PeerMessage {
	b.mu.Lock()
	defer b.mu.Unlock()

	msg := protocol.PeerMessage{
		ID:               fmt.Sprintf("peer-msg-%d", time.Now().UnixNano()),
		FromInstanceID:   fromID,
		FromInstanceName: fromName,
		ToInstanceID:     toID,
		Kind:             kind,
		Content:          content,
		Data:             data,
		CreatedAt:        time.Now().UTC(),
	}
	b.messages = append(b.messages, msg)
	return msg
}

func (b *Bus) ListMessages(ctx context.Context, instanceID string, limit int) []protocol.PeerMessage {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	// Empty, not nil: a nil slice marshals to JSON null, and clients that
	// reasonably expect a list then fail on the cast rather than showing
	// "no messages".
	out := make([]protocol.PeerMessage, 0)
	for i := len(b.messages) - 1; i >= 0; i-- {
		m := b.messages[i]
		if instanceID == "" || m.ToInstanceID == "broadcast" || m.ToInstanceID == instanceID || m.FromInstanceID == instanceID {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
