package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// These tests cover the shared fleet vault (secrets), the browser session
// handoff blobs, and the inter-agent peer message log.
//
// A note on what the peer "bus" actually is, because the name oversells it:
// vault.Bus is not a delivery bus. SendMessage appends to a slice and
// ListMessages filters that slice on read. There is no subscription, no
// delivery, no push and no acknowledgement. "Peer A messages peer B" therefore
// means "the record becomes visible to a reader filtering on B", and that is
// what these tests assert.

func ctx() context.Context { return context.Background() }

// visibleTo reports whether a message with the given id shows up in the log
// when read from instanceID's point of view.
func visibleTo(t *testing.T, b *Bus, instanceID, msgID string) bool {
	t.Helper()
	for _, m := range b.ListMessages(ctx(), instanceID, 500) {
		if m.ID == msgID {
			return true
		}
	}
	return false
}

// -------------------------------------------------- A. inter-agent comms ---

func TestDirectPeerMessageIsVisibleOnlyToTheEndpoints(t *testing.T) {
	b := NewBus()
	msg := b.SendMessage(ctx(), "inst-a", "Alpha", "inst-b", "message", "ping", nil)

	if !visibleTo(t, b, "inst-b", msg.ID) {
		t.Error("addressee inst-b cannot see the message addressed to it")
	}
	// The sender sees its own outbound traffic: ListMessages matches on
	// FromInstanceID as well as ToInstanceID.
	if !visibleTo(t, b, "inst-a", msg.ID) {
		t.Error("sender inst-a cannot see its own outbound message")
	}
	// An uninvolved third party must not.
	if visibleTo(t, b, "inst-c", msg.ID) {
		t.Error("uninvolved inst-c can see a message addressed to inst-b")
	}
}

func TestBroadcastReachesEveryPeerIncludingTheSender(t *testing.T) {
	b := NewBus()
	msg := b.SendMessage(ctx(), "inst-a", "Alpha", "broadcast", "message", "all hands", nil)

	// Recorded behaviour: ListMessages short-circuits on
	// ToInstanceID == "broadcast", so a broadcast is visible to every peer and
	// the sender is NOT excluded from its own broadcast.
	for _, peer := range []string{"inst-a", "inst-b", "inst-c", "never-registered"} {
		if !visibleTo(t, b, peer, msg.ID) {
			t.Errorf("broadcast not visible to %q", peer)
		}
	}
}

func TestEmptyInstanceFilterSeesTheWholeLog(t *testing.T) {
	b := NewBus()
	direct := b.SendMessage(ctx(), "inst-a", "Alpha", "inst-b", "message", "private", nil)
	bcast := b.SendMessage(ctx(), "inst-b", "Bravo", "broadcast", "report", "public", nil)

	if !visibleTo(t, b, "", direct.ID) || !visibleTo(t, b, "", bcast.ID) {
		t.Error(`the operator view (instanceID "") must see every message`)
	}
}

func TestMessageToUnregisteredPeerIsAcceptedWithoutErrorOrPanic(t *testing.T) {
	b := NewBus()

	// There is no peer registry to validate against: Bus never learns which
	// instance ids exist. Sending to a nonexistent peer is accepted and stored,
	// it does not error, drop, panic or block. This is the real behaviour and
	// it is worth pinning: an agent that hallucinates a peer_id gets silence,
	// not a failure it could react to.
	done := make(chan protocol.PeerMessage, 1)
	go func() {
		done <- b.SendMessage(ctx(), "inst-a", "Alpha", "no-such-instance", "message", "hello?", nil)
	}()

	select {
	case msg := <-done:
		if msg.ID == "" {
			t.Fatal("send to unknown peer returned a zero message")
		}
		if msg.ToInstanceID != "no-such-instance" {
			t.Errorf("ToInstanceID = %q, want the unknown id preserved verbatim", msg.ToInstanceID)
		}
		if !visibleTo(t, b, "no-such-instance", msg.ID) {
			t.Error("message to unknown peer was not retained in the log")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendMessage to an unknown peer hung")
	}
}

func TestListMessagesReturnsNewestFirstAndHonoursTheLimit(t *testing.T) {
	b := NewBus()
	for i := 0; i < 10; i++ {
		b.SendMessage(ctx(), "inst-a", "Alpha", "inst-b", "message", fmt.Sprintf("m%d", i), nil)
	}

	got := b.ListMessages(ctx(), "inst-b", 3)
	if len(got) != 3 {
		t.Fatalf("limit 3 returned %d messages", len(got))
	}
	if got[0].Content != "m9" {
		t.Errorf("newest-first expected m9 at index 0, got %q", got[0].Content)
	}

	// A non-positive limit falls back to the 50 default rather than returning
	// nothing.
	if n := len(b.ListMessages(ctx(), "inst-b", 0)); n != 10 {
		t.Errorf("limit 0 returned %d messages, want all 10 via the default of 50", n)
	}
}

func TestConcurrentSendAndListLosesNoMessage(t *testing.T) {
	const senders = 8
	const perSender = 100
	const readers = 4

	b := NewBus()
	var writeWG, readWG sync.WaitGroup
	stop := make(chan struct{})

	// Readers run concurrently with the writers purely to put the RWMutex under
	// contention; -race is the real assertion here.
	for i := 0; i < readers; i++ {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = b.ListMessages(ctx(), "inst-b", 50)
					_ = b.ListMessages(ctx(), "", 10)
					runtime.Gosched()
				}
			}
		}()
	}

	for s := 0; s < senders; s++ {
		writeWG.Add(1)
		go func(s int) {
			defer writeWG.Done()
			for i := 0; i < perSender; i++ {
				b.SendMessage(ctx(), fmt.Sprintf("inst-%d", s), "Sender", "inst-b",
					"message", fmt.Sprintf("%d-%d", s, i), nil)
			}
		}(s)
	}

	writeWG.Wait()
	close(stop)
	readWG.Wait()

	all := b.ListMessages(ctx(), "inst-b", senders*perSender+10)
	if len(all) != senders*perSender {
		t.Fatalf("got %d messages, want %d — messages were lost under concurrency",
			len(all), senders*perSender)
	}

	// Every (sender, sequence) pair must appear exactly once.
	seen := make(map[string]int, senders*perSender)
	for _, m := range all {
		seen[m.Content]++
	}
	for s := 0; s < senders; s++ {
		for i := 0; i < perSender; i++ {
			k := fmt.Sprintf("%d-%d", s, i)
			if seen[k] != 1 {
				t.Fatalf("message %q appeared %d times, want exactly 1", k, seen[k])
			}
		}
	}
}

// ------------------------------------------------ B. shared fleet vault ---

func TestSharedSecretRoundTrip(t *testing.T) {
	b := NewBus()

	put := b.PutSecret(ctx(), "stripe.key", "sk_live_abc123", "fleet", "billing", "inst-a")
	if put.Key != "stripe.key" || put.Value != "sk_live_abc123" {
		t.Fatalf("PutSecret returned %+v", put)
	}
	if put.UpdatedAt.IsZero() {
		t.Error("PutSecret left UpdatedAt zero")
	}

	got, ok := b.GetSecret(ctx(), "stripe.key")
	if !ok {
		t.Fatal("GetSecret did not find the secret just written")
	}
	if got.Value != "sk_live_abc123" {
		t.Errorf("round-trip value = %q", got.Value)
	}
	if got.Note != "billing" || got.CreatedBy != "inst-a" {
		t.Errorf("round-trip lost metadata: %+v", got)
	}

	if list := b.ListSecrets(ctx()); len(list) != 1 {
		t.Fatalf("ListSecrets returned %d entries, want 1", len(list))
	}

	b.DeleteSecret(ctx(), "stripe.key")
	if _, ok := b.GetSecret(ctx(), "stripe.key"); ok {
		t.Error("secret still retrievable after delete")
	}
	if list := b.ListSecrets(ctx()); len(list) != 0 {
		t.Errorf("ListSecrets returned %d entries after delete, want 0", len(list))
	}
}

func TestEmptyScopeDefaultsToFleet(t *testing.T) {
	b := NewBus()
	sec := b.PutSecret(ctx(), "k", "v", "", "", "")
	if sec.Scope != "fleet" {
		t.Errorf("Scope = %q, want the %q default", sec.Scope, "fleet")
	}
	stored, _ := b.GetSecret(ctx(), "k")
	if stored.Scope != "fleet" {
		t.Errorf("stored Scope = %q, want the default applied before storage", stored.Scope)
	}
}

func TestMissingSecretIsACleanNotFound(t *testing.T) {
	b := NewBus()

	sec, ok := b.GetSecret(ctx(), "never-created")
	if ok {
		t.Fatal("GetSecret reported ok for a key that was never written")
	}
	// Not-found must be a genuine miss, not an empty success: the zero struct
	// must not look like a real secret with a blank value.
	if sec.Key != "" || sec.Value != "" || sec.Scope != "" || !sec.UpdatedAt.IsZero() {
		t.Errorf("miss returned a populated struct: %+v", sec)
	}

	b.PutSecret(ctx(), "temp", "v", "fleet", "", "")
	b.DeleteSecret(ctx(), "temp")
	if sec, ok := b.GetSecret(ctx(), "temp"); ok {
		t.Errorf("deleted key still reports ok: %+v", sec)
	}

	// Deleting something that is not there is a no-op, not a panic.
	b.DeleteSecret(ctx(), "never-created")
}

func TestConcurrentUpdatesToOneKeyLandOnAWholeValue(t *testing.T) {
	const writers = 16
	const rounds = 50

	b := NewBus()
	written := make(map[string]bool, writers)
	for w := 0; w < writers; w++ {
		written[fmt.Sprintf("value-from-writer-%d", w)] = true
	}

	var writeWG, readWG sync.WaitGroup
	stop := make(chan struct{})

	// Concurrent readers, so -race sees both sides of the lock.
	for i := 0; i < 4; i++ {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if sec, ok := b.GetSecret(ctx(), "hot.key"); ok && !written[sec.Value] {
						t.Errorf("read a torn value: %q", sec.Value)
						return
					}
					_ = b.ListSecrets(ctx())
					runtime.Gosched()
				}
			}
		}()
	}

	for w := 0; w < writers; w++ {
		writeWG.Add(1)
		go func(w int) {
			defer writeWG.Done()
			val := fmt.Sprintf("value-from-writer-%d", w)
			for i := 0; i < rounds; i++ {
				b.PutSecret(ctx(), "hot.key", val, "fleet", "", fmt.Sprintf("inst-%d", w))
			}
		}(w)
	}
	writeWG.Wait()
	close(stop)
	readWG.Wait()

	final, ok := b.GetSecret(ctx(), "hot.key")
	if !ok {
		t.Fatal("hot.key vanished after concurrent writes")
	}
	if !written[final.Value] {
		t.Fatalf("final value %q is not one of the values any writer wrote — it is torn", final.Value)
	}
	// Last write wins on a single key: no duplicate entries accumulate.
	if n := len(b.ListSecrets(ctx())); n != 1 {
		t.Errorf("ListSecrets returned %d entries for one key", n)
	}
}

// --------------------------------------- C. browser session / cookie handoff ---

// The Go orchestrator does not parse cookies. SharedSession.CookiesJSON is an
// opaque string that the agent exports and another agent imports verbatim —
// there is no cookie struct, no expiry handling and no merge logic anywhere in
// backend/. These tests therefore pin the property that actually exists:
// the blob survives export -> import -> export byte-for-byte, including the
// empty set, unicode, and embedded expiry timestamps.

func TestSessionBlobRoundTripsUnchanged(t *testing.T) {
	cases := []struct {
		name    string
		cookies string
		local   string
	}{
		{
			name:    "empty set",
			cookies: `[]`,
			local:   ``,
		},
		{
			name:    "unicode values",
			cookies: `[{"name":"greeting","value":"héllo — 世界 🌍","domain":"example.com"}]`,
			local:   `{"user":"Ünïcødé Ω"}`,
		},
		{
			name:    "expiry timestamps",
			cookies: `[{"name":"sid","value":"abc","expires":1893456000,"expirationDate":1893456000.5,"secure":true,"httpOnly":true,"sameSite":"Lax"}]`,
			local:   `{"exp":"2029-12-31T23:59:59Z"}`,
		},
		{
			name:    "quotes and backslashes survive",
			cookies: `[{"name":"raw","value":"a\"b\\c\nd"}]`,
			local:   `{}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBus()

			// export -> import
			saved := b.SaveSession(ctx(), "example.com", tc.name, tc.cookies, tc.local, "inst-a")
			if saved.ID == "" {
				t.Fatal("SaveSession returned an empty id")
			}

			got, ok := b.GetSession(ctx(), saved.ID)
			if !ok {
				t.Fatal("GetSession could not find the session just saved")
			}
			if got.CookiesJSON != tc.cookies {
				t.Errorf("cookie blob changed across round-trip:\n got %q\nwant %q", got.CookiesJSON, tc.cookies)
			}
			if got.LocalStorageJSON != tc.local {
				t.Errorf("localStorage blob changed:\n got %q\nwant %q", got.LocalStorageJSON, tc.local)
			}

			// -> export again: a second hop must be identical to the first.
			again := b.SaveSession(ctx(), got.Domain, got.Title, got.CookiesJSON, got.LocalStorageJSON, got.CreatedByInstance)
			final, ok := b.GetSession(ctx(), again.ID)
			if !ok {
				t.Fatal("second-hop session missing")
			}
			if final.CookiesJSON != tc.cookies {
				t.Errorf("export->import->export drifted:\n got %q\nwant %q", final.CookiesJSON, tc.cookies)
			}

			// The blob is still valid JSON after the trip, so a real cookie jar
			// on the far side can parse it.
			var parsed any
			if err := json.Unmarshal([]byte(final.CookiesJSON), &parsed); err != nil {
				t.Errorf("round-tripped cookie blob is no longer valid JSON: %v", err)
			}
		})
	}
}

func TestListSessionsFiltersByDomain(t *testing.T) {
	b := NewBus()
	b.SaveSession(ctx(), "example.com", "a", `[]`, "", "inst-a")
	b.SaveSession(ctx(), "example.com", "b", `[]`, "", "inst-a")
	b.SaveSession(ctx(), "other.test", "c", `[]`, "", "inst-b")

	if n := len(b.ListSessions(ctx(), "example.com")); n != 2 {
		t.Errorf("example.com returned %d sessions, want 2", n)
	}
	if n := len(b.ListSessions(ctx(), "")); n != 3 {
		t.Errorf("empty domain filter returned %d sessions, want all 3", n)
	}
	if n := len(b.ListSessions(ctx(), "absent.test")); n != 0 {
		t.Errorf("unknown domain returned %d sessions, want 0", n)
	}
}

func TestMissingSessionIsACleanNotFound(t *testing.T) {
	b := NewBus()
	sess, ok := b.GetSession(ctx(), "sess-nope-1")
	if ok {
		t.Fatal("GetSession reported ok for an id that was never saved")
	}
	if sess.ID != "" || sess.CookiesJSON != "" {
		t.Errorf("miss returned a populated struct: %+v", sess)
	}
}

// ------------------------------------------------------- leak containment ---

// A secret value must not be reachable from anything that gets rendered into an
// error, a log line, or an API payload. The precedent this guards against is an
// API key that reached a task's persisted error column through a URL.
func TestSecretValueIsNotExposedByFormattingTheStructs(t *testing.T) {
	const secret = "sk_live_SUPERSECRET_ABC123"

	b := NewBus()
	b.PutSecret(ctx(), "stripe.key", secret, "fleet", "billing key", "inst-a")

	// The API projection is the thing that must be clean. Marshalling the raw
	// storage record is expected to contain the value — that is why
	// internal/httpapi never serialises protocol.SharedSecret directly, which
	// TestSharedSecretValueNeverReachesTheAPI in that package enforces.
	sec, _ := b.GetSecret(ctx(), "stripe.key")
	if sec.Value != secret {
		t.Fatal("precondition: in-process read must still yield the plaintext")
	}

	// A peer message about a secret carries the key, never the value — assert
	// that the surrounding plumbing does not smuggle it in.
	msg := b.SendMessage(ctx(), "inst-a", "Alpha", "inst-b", "message",
		"use the shared secret stripe.key", map[string]any{"secret_key": "stripe.key"})
	blob, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), secret) {
		t.Errorf("peer message payload leaked the secret value: %s", blob)
	}

	// Sessions likewise: the id is derived from the domain and a timestamp, so
	// it can never embed credential material.
	sess := b.SaveSession(ctx(), "example.com", "t", `[]`, "", "inst-a")
	if strings.Contains(sess.ID, secret) {
		t.Errorf("session id leaked the secret value: %s", sess.ID)
	}
}
