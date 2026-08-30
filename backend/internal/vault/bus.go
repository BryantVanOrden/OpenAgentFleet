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

	UpsertSharedSecret(ctx context.Context, s protocol.SharedSecret) error
	ListSharedSecrets(ctx context.Context) ([]protocol.SharedSecret, error)
	DeleteSharedSecret(ctx context.Context, key string) error

	UpsertSharedSession(ctx context.Context, s protocol.SharedSession) error
	ListSharedSessions(ctx context.Context) ([]protocol.SharedSession, error)
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
	// conversations are the named threads messages are filed into. The
	// broadcast channel is implicit and never held here.
	conversations map[string]protocol.Conversation
	// seq disambiguates messages sent within the same nanosecond tick, for the
	// same reason memory.Engine carries one: a timestamp is not an identity,
	// and a burst is exactly when collisions happen.
	seq uint64

	// store is optional. When nil the bus is in-memory only.
	store     PeerStore
	convStore ConversationStore
	log       *slog.Logger
}

var GlobalBus = NewBus()

func NewBus() *Bus {
	return &Bus{
		secrets:  make(map[string]protocol.SharedSecret),
		sessions: make(map[string]protocol.SharedSession),
		messages: make([]protocol.PeerMessage, 0),

		conversations: make(map[string]protocol.Conversation),
	}
}

// AttachStore makes the bus durable: recent peer messages are loaded back into
// the in-memory working set, and everything sent afterwards is written through.
//
// Shared secrets and browser sessions are persisted too, in plaintext, at the
// operator's explicit direction. That is a real trade: anyone with database
// access reads every fleet credential and live cookie jar. It was made because
// this fleet is single-tenant on the operator's own hardware and a shared
// secret that evaporates on every deploy is worse than one at rest. For a
// deployment where that is not true, the encrypted vault
// (internal/vault.Vault) is the place for them.
func (b *Bus) AttachStore(ctx context.Context, st PeerStore, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	history, err := st.ListPeerMessages(ctx, "", peerHydrateLimit)
	if err != nil {
		return err
	}

	secrets, err := st.ListSharedSecrets(ctx)
	if err != nil {
		return err
	}
	sessions, err := st.ListSharedSessions(ctx)
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
	for _, s := range secrets {
		b.secrets[s.Key] = s
	}
	for _, s := range sessions {
		b.sessions[s.ID] = s
	}
	return nil
}

// -------------------------------------------------------------- Shared Secrets ---

func (b *Bus) PutSecret(ctx context.Context, key, value, scope, note, createdBy, orgID string) protocol.SharedSecret {
	b.mu.Lock()
	defer b.mu.Unlock()

	sec := protocol.SharedSecret{
		Key:       key,
		Value:     value,
		Scope:     scope,
		Note:      note,
		CreatedBy: createdBy,
		OrgID:     orgID,
		UpdatedAt: time.Now().UTC(),
	}
	if sec.Scope == "" {
		sec.Scope = "fleet"
	}
	b.secrets[key] = sec
	st, log := b.store, b.log
	if st != nil {
		// Written under the lock, unlike peer messages: a secret is written
		// rarely and read constantly, so the round trip costs nothing here,
		// and a caller that gets the secret back must not then find it missing.
		if err := st.UpsertSharedSecret(ctx, sec); err != nil && log != nil {
			log.Warn("shared secret not persisted", "key", key, "err", err)
		}
	}
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
	if b.store != nil {
		if err := b.store.DeleteSharedSecret(ctx, key); err != nil && b.log != nil {
			b.log.Warn("shared secret not deleted", "key", key, "err", err)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.secrets, key)
}

// ------------------------------------------------------------- Shared Sessions ---

func (b *Bus) SaveSession(ctx context.Context, domain, title, cookiesJSON, localStorageJSON, createdBy, orgID string) protocol.SharedSession {
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
		OrgID:             orgID,
		CreatedAt:         time.Now().UTC(),
	}
	b.sessions[id] = sess
	if b.store != nil {
		if err := b.store.UpsertSharedSession(ctx, sess); err != nil && b.log != nil {
			b.log.Warn("shared session not persisted", "id", id, "err", err)
		}
	}
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

// SendMessage posts a message and files it in whichever thread it belongs to.
// Use SendMessageIn to place it in a specific one.
func (b *Bus) SendMessage(ctx context.Context, fromID, fromName, toID, kind, content string, data map[string]any) protocol.PeerMessage {
	return b.SendMessageIn(ctx, "", fromID, fromName, toID, kind, content, data)
}

// SendMessageIn posts a message into a named conversation.
//
// An empty conversationID means "work it out": broadcast when unaddressed,
// otherwise the two-party thread between sender and recipient. That is what
// makes agents talking to each other appear in their own thread without the
// agents themselves knowing conversations exist.
func (b *Bus) SendMessageIn(ctx context.Context, conversationID, fromID, fromName, toID, kind, content string, data map[string]any) protocol.PeerMessage {
	return b.sendFrom(ctx, conversationID, fromID, fromName, "", toID, kind, content, data)
}

// SendMessageAs is SendMessageIn with the person behind the message recorded,
// for the human turns. An agent replying to "the operator" cannot tell two
// colleagues apart without it.
func (b *Bus) SendMessageAs(ctx context.Context, conversationID, fromID, fromName, fromUserID, toID, kind, content string, data map[string]any) protocol.PeerMessage {
	return b.sendFrom(ctx, conversationID, fromID, fromName, fromUserID, toID, kind, content, data)
}

func (b *Bus) sendFrom(ctx context.Context, conversationID, fromID, fromName, fromUserID, toID, kind, content string, data map[string]any) protocol.PeerMessage {
	b.mu.Lock()
	// A thread can be deleted between an agent being asked something and its
	// answer arriving -- an agent takes a minute or two to reply, and the
	// operator does not wait. Filing the answer to a conversation that no
	// longer exists writes it into a void: it belongs to nothing, is listed
	// nowhere, and is only found by reading the database. Send it where an
	// unaddressed message goes instead.
	if conversationID != "" && conversationID != protocol.BroadcastConversationID {
		if _, ok := b.conversations[conversationID]; !ok {
			conversationID = b.defaultChannelLocked()
		}
	}
	b.seq++
	msg := protocol.PeerMessage{
		ID:               fmt.Sprintf("peer-msg-%d-%d", time.Now().UnixNano(), b.seq),
		ConversationID:   conversationID,
		FromInstanceID:   fromID,
		FromInstanceName: fromName,
		FromUserID:       fromUserID,
		ToInstanceID:     toID,
		Kind:             kind,
		Content:          content,
		Data:             data,
		CreatedAt:        time.Now().UTC(),
	}
	autoFiled := false
	if msg.ConversationID == "" {
		msg.ConversationID = b.conversationOf(msg)
		autoFiled = msg.ConversationID != protocol.BroadcastConversationID
	}
	b.messages = append(b.messages, msg)
	st, log := b.store, b.log
	b.mu.Unlock()

	// Give an auto-filed message a thread to belong to. Filing one under an id
	// with no conversation behind it hides the whole exchange from the comms
	// list, which is how two agents can talk at length and appear silent.
	// Outside the lock because CanonicalThread takes it.
	if autoFiled {
		from := fromID
		if from == "" {
			from = protocol.OperatorMemberID
		}
		b.CanonicalThread(ctx, []string{from, toID})
	}

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
		if m.Compacted {
			// Compacted history is replaced by its summary, which is itself a
			// message in the thread and is returned normally.
			continue
		}
		if instanceID == "" || m.ToInstanceID == "broadcast" || m.ToInstanceID == instanceID || m.FromInstanceID == instanceID {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
