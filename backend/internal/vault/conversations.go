package vault

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Conversations are explicit threads in fleet comms.
//
// Before this, a thread was whatever the message list happened to imply, so
// there was no way to say "these two should talk" before they had, and no way
// to put a finished thread away. A conversation is now created and deleted by
// the operator, and messages are filed into one.

// ConversationStore is the durable half. Optional: with no store the bus keeps
// conversations in memory only, exactly as it does messages.
type ConversationStore interface {
	UpsertConversation(ctx context.Context, c protocol.Conversation) error
	ListConversations(ctx context.Context) ([]protocol.Conversation, error)
	DeleteConversation(ctx context.Context, id string) error
	CompactConversation(ctx context.Context, conversationID, throughID string) error
}

// ConversationIDFor builds the deterministic ID of a two-party thread.
//
// Deterministic so that "the thread between A and B" is one thread no matter
// who opened it or which direction the first message went. Group threads get a
// random ID instead: two different groups can legitimately hold the same
// people.
func ConversationIDFor(members []string) string {
	norm := normalizeMembers(members)
	h := sha1.Sum([]byte(strings.Join(norm, "\x00")))
	return "conv-" + hex.EncodeToString(h[:])[:16]
}

// normalizeMembers sorts and de-duplicates so member order never changes
// identity.
func normalizeMembers(members []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(members))
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// KindFor classifies a member list.
func KindFor(members []string) string {
	norm := normalizeMembers(members)
	hasOperator := false
	agents := 0
	for _, m := range norm {
		if m == protocol.OperatorMemberID {
			hasOperator = true
			continue
		}
		agents++
	}
	switch {
	case hasOperator && agents == 1:
		return protocol.ConversationDirect
	case !hasOperator && agents == 2:
		return protocol.ConversationPair
	default:
		return protocol.ConversationGroup
	}
}

// AttachConversationStore reloads stored threads into the working set.
func (b *Bus) AttachConversationStore(ctx context.Context, st ConversationStore) error {
	convs, err := st.ListConversations(ctx)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.convStore = st
	for _, c := range convs {
		b.conversations[c.ID] = c
	}
	return nil
}

// CreateConversation opens a thread between the given members.
//
// For direct and pair threads this is find-or-create: asking twice for the
// conversation between the same two parties returns the same one rather than
// splitting their history across duplicates.
func (b *Bus) CreateConversation(ctx context.Context, title string, members []string) protocol.Conversation {
	norm := normalizeMembers(members)
	kind := KindFor(norm)

	id := ConversationIDFor(norm)
	if kind == protocol.ConversationGroup {
		b.mu.Lock()
		b.seq++
		id = fmt.Sprintf("conv-group-%d-%d", time.Now().UnixNano(), b.seq)
		b.mu.Unlock()
	}

	b.mu.Lock()
	if existing, ok := b.conversations[id]; ok {
		// Re-opening a direct or pair thread keeps its history. A new title is
		// still honoured; the operator renaming a thread should not be ignored.
		if title != "" && title != existing.Title {
			existing.Title = title
			b.conversations[id] = existing
		}
		st := b.convStore
		b.mu.Unlock()
		if st != nil && title != "" {
			_ = st.UpsertConversation(ctx, existing)
		}
		return existing
	}

	c := protocol.Conversation{
		ID:        id,
		Kind:      kind,
		Title:     title,
		Members:   norm,
		CreatedAt: time.Now().UTC(),
	}
	b.conversations[id] = c
	st, log := b.convStore, b.log
	b.mu.Unlock()

	if st != nil {
		if err := st.UpsertConversation(ctx, c); err != nil && log != nil {
			log.Warn("conversation not persisted", "id", c.ID, "err", err)
		}
	}
	return c
}

// DeleteConversation closes a thread. Its messages are kept but unfiled.
func (b *Bus) DeleteConversation(ctx context.Context, id string) bool {
	if id == protocol.BroadcastConversationID {
		// The broadcast channel is the fleet's only always-on channel; deleting
		// it would leave unaddressed messages with nowhere to land.
		return false
	}
	b.mu.Lock()
	_, existed := b.conversations[id]
	delete(b.conversations, id)
	for i := range b.messages {
		if b.messages[i].ConversationID == id {
			b.messages[i].ConversationID = ""
		}
	}
	st, log := b.convStore, b.log
	b.mu.Unlock()

	if existed && st != nil {
		if err := st.DeleteConversation(ctx, id); err != nil && log != nil {
			log.Warn("conversation not deleted", "id", id, "err", err)
		}
	}
	return existed
}

// ListConversations returns every thread, with the built-in broadcast channel
// first, each carrying its live message count and last activity.
func (b *Bus) ListConversations(ctx context.Context) []protocol.Conversation {
	b.mu.RLock()
	defer b.mu.RUnlock()

	broadcast := protocol.Conversation{
		ID:      protocol.BroadcastConversationID,
		Kind:    protocol.ConversationGroup,
		Title:   "Everyone",
		Members: []string{},
	}
	out := []protocol.Conversation{broadcast}
	for _, c := range b.conversations {
		out = append(out, c)
	}

	counts := map[string]int{}
	last := map[string]time.Time{}
	for _, m := range b.messages {
		if m.Compacted {
			continue
		}
		id := b.conversationOf(m)
		counts[id]++
		if m.CreatedAt.After(last[id]) {
			last[id] = m.CreatedAt
		}
	}
	for i := range out {
		out[i].MessageCount = counts[out[i].ID]
		out[i].LastMessageAt = last[out[i].ID]
		if out[i].Members == nil {
			out[i].Members = []string{}
		}
	}

	// Busiest-recently first, with pinned threads above the rest. The broadcast
	// channel stays at the very top regardless: it is the one thread that is
	// always there and always where a stray message lands.
	rest := out[1:]
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Pinned != rest[j].Pinned {
			return rest[i].Pinned
		}
		return rest[i].LastMessageAt.After(rest[j].LastMessageAt)
	})
	return out
}

// conversationOf places a message in a thread.
//
// Messages written before conversations existed, and replies from agents that
// only know how to address an instance, carry no conversation ID. Rather than
// stranding them, they are filed where they would have been created: broadcast
// if unaddressed, otherwise the deterministic two-party thread. Caller holds
// at least a read lock.
func (b *Bus) conversationOf(m protocol.PeerMessage) string {
	if m.ConversationID != "" {
		return m.ConversationID
	}
	if m.ToInstanceID == "" || m.ToInstanceID == protocol.BroadcastConversationID {
		return protocol.BroadcastConversationID
	}
	from := m.FromInstanceID
	if from == "" {
		from = protocol.OperatorMemberID
	}
	return ConversationIDFor([]string{from, m.ToInstanceID})
}

// ConversationOf is conversationOf for callers outside the bus.
func (b *Bus) ConversationOf(m protocol.PeerMessage) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.conversationOf(m)
}

// ListConversationMessages returns one thread's messages, oldest first, with
// compacted history left out.
func (b *Bus) ListConversationMessages(ctx context.Context, conversationID string, limit int) []protocol.PeerMessage {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 {
		limit = 200
	}
	out := make([]protocol.PeerMessage, 0)
	for i := len(b.messages) - 1; i >= 0 && len(out) < limit; i-- {
		m := b.messages[i]
		if m.Compacted || b.conversationOf(m) != conversationID {
			continue
		}
		out = append(out, m)
	}
	// Collected newest-first to honour the limit; a thread reads oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Summarizer turns a thread into a short summary. The LLM client supplies it;
// the bus stays free of any model dependency.
type Summarizer func(ctx context.Context, transcript string) (string, error)

// CompactConversation replaces a thread's history with one summary message.
//
// The summary is appended first and only then are the messages it covers
// marked: if the process dies in between, the thread has a redundant summary
// rather than a hole where its history was.
func (b *Bus) CompactConversation(ctx context.Context, conversationID string, summarize Summarizer) (protocol.PeerMessage, error) {
	msgs := b.ListConversationMessages(ctx, conversationID, 0)
	if len(msgs) < 2 {
		return protocol.PeerMessage{}, fmt.Errorf("nothing to compact: the thread has %d message(s)", len(msgs))
	}

	var sb strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&sb, "%s: %s\n", m.FromInstanceName, m.Content)
	}
	summary, err := summarize(ctx, sb.String())
	if err != nil {
		return protocol.PeerMessage{}, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return protocol.PeerMessage{}, fmt.Errorf("summariser returned nothing; leaving the thread intact")
	}

	through := msgs[len(msgs)-1].ID
	msg := b.SendMessageIn(ctx, conversationID, "", "Summary",
		lastRecipient(msgs), "summary", summary, map[string]any{
			"compacted_messages": len(msgs),
			"through_message_id": through,
		})

	b.mu.Lock()
	for i := range b.messages {
		if b.messages[i].ID == msg.ID {
			continue
		}
		if b.messages[i].Compacted || b.conversationOf(b.messages[i]) != conversationID {
			continue
		}
		if !b.messages[i].CreatedAt.After(msgs[len(msgs)-1].CreatedAt) {
			b.messages[i].Compacted = true
		}
	}
	st, log := b.convStore, b.log
	b.mu.Unlock()

	if st != nil {
		if err := st.CompactConversation(ctx, conversationID, through); err != nil && log != nil {
			log.Warn("compaction not persisted", "conversation", conversationID, "err", err)
		}
	}
	return msg, nil
}

// lastRecipient keeps a summary addressed the way the thread was, so it is
// visible to the same agents the messages it replaces were.
func lastRecipient(msgs []protocol.PeerMessage) string {
	if len(msgs) == 0 {
		return protocol.BroadcastConversationID
	}
	return msgs[len(msgs)-1].ToInstanceID
}

// OtherAgentMember returns the one other agent in a thread, if there is
// exactly one.
//
// Used to address a reply. In a two-agent thread the answer goes to the other
// agent, which keeps it inside the thread; anywhere else there is no single
// recipient and the caller falls back to the fleet.
func (b *Bus) OtherAgentMember(conversationID, senderID string) (string, bool) {
	if conversationID == "" || conversationID == protocol.BroadcastConversationID {
		return "", false
	}
	b.mu.RLock()
	c, ok := b.conversations[conversationID]
	b.mu.RUnlock()
	if !ok {
		return "", false
	}

	var others []string
	for _, m := range c.Members {
		if m == protocol.OperatorMemberID || m == senderID {
			continue
		}
		others = append(others, m)
	}
	if len(others) != 1 {
		return "", false
	}
	return others[0], true
}

// IsMember reports whether someone is in a thread.
func (b *Bus) IsMember(conversationID, memberID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok := b.conversations[conversationID]
	if !ok {
		return false
	}
	for _, m := range c.Members {
		if m == memberID {
			return true
		}
	}
	return false
}

// RenameConversation sets a thread's title. Naming a thread is how you find it
// again once there are more than a handful.
func (b *Bus) RenameConversation(ctx context.Context, id, title string) (protocol.Conversation, bool) {
	return b.updateConversation(ctx, id, func(c *protocol.Conversation) { c.Title = title })
}

// PinConversation pins or unpins a thread.
func (b *Bus) PinConversation(ctx context.Context, id string, pinned bool) (protocol.Conversation, bool) {
	return b.updateConversation(ctx, id, func(c *protocol.Conversation) { c.Pinned = pinned })
}

func (b *Bus) updateConversation(ctx context.Context, id string, apply func(*protocol.Conversation)) (protocol.Conversation, bool) {
	b.mu.Lock()
	c, ok := b.conversations[id]
	if !ok {
		b.mu.Unlock()
		return protocol.Conversation{}, false
	}
	apply(&c)
	b.conversations[id] = c
	st, log := b.convStore, b.log
	b.mu.Unlock()

	if st != nil {
		if err := st.UpsertConversation(ctx, c); err != nil && log != nil {
			log.Warn("conversation update not persisted", "id", id, "err", err)
		}
	}
	return c, true
}
