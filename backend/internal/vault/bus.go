package vault

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// PeerStore is the persistence a Bus writes its P2P messages through to.
//
// It is an interface rather than *store.Store so the bus keeps working with no
// database at all -- which is how every test constructs it, and how the
// package-level GlobalBus behaves until the server attaches a store on boot.
type PeerStore interface {
	InsertPeerMessage(ctx context.Context, m protocol.PeerMessage) error
	ListPeerMessages(ctx context.Context, instanceID string, limit int) ([]protocol.PeerMessage, error)
}

// peerHydrateLimit is how much conversation history is pulled back into memory
// on boot. Deep history stays in the database and is not what an agent asking
// "what did anyone say to me" needs.
const peerHydrateLimit = 500

// Bus manages the shared fleet secrets vault, browser sessions, and inter-agent P2P messages.
type Bus struct {
	mu       sync.RWMutex
	secrets  map[string]protocol.SharedSecret
	sessions map[string]protocol.SharedSession
	messages []protocol.PeerMessage
	// seq disambiguates messages sent within the same nanosecond tick, for the
	// same reason memory.Engine carries one: a timestamp is not an identity,
	// and a burst is exactly when collisions happen.
	seq uint64

	// store is optional. When nil the bus is in-memory only.
	store PeerStore
	log   *slog.Logger
}

var GlobalBus = NewBus()

func NewBus() *Bus {
	return &Bus{
		secrets:  make(map[string]protocol.SharedSecret),
		sessions: make(map[string]protocol.SharedSession),
		messages: make([]protocol.PeerMessage, 0),
	}
}

// AttachStore makes the bus durable: recent peer messages are loaded back into
// the in-memory working set, and everything sent afterwards is written through.
//
// Shared secrets and browser sessions deliberately stay in memory. Their tables
// exist, but they hold credential plaintext and live cookie jars, and writing
// those to an unencrypted table is a decision for the operator to make
// explicitly rather than something persistence should acquire by accident --
// the encrypted vault (internal/vault.Vault) is the place for them.
func (b *Bus) AttachStore(ctx context.Context, st PeerStore, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	history, err := st.ListPeerMessages(ctx, "", peerHydrateLimit)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.store = st
	b.log = log
	// ListPeerMessages returns oldest first, which is the order this slice is
	// appended in and the order ListMessages walks backwards from.
	b.messages = append(history, b.messages...)
	return nil
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
	b.seq++
	msg := protocol.PeerMessage{
		ID:               fmt.Sprintf("peer-msg-%d-%d", time.Now().UnixNano(), b.seq),
		FromInstanceID:   fromID,
		FromInstanceName: fromName,
		ToInstanceID:     toID,
		Kind:             kind,
		Content:          content,
		Data:             data,
		CreatedAt:        time.Now().UTC(),
	}
	b.messages = append(b.messages, msg)
	st, log := b.store, b.log
	b.mu.Unlock()

	// Written outside the lock: a database round trip must not block every
	// other agent's reads. A failed write costs durability for this one
	// message, never the delivery -- the message is already in the working set.
	if st != nil {
		if err := st.InsertPeerMessage(ctx, msg); err != nil {
			log.Warn("peer message not persisted", "id", msg.ID, "err", err)
		}
	}
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
