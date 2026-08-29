package vault

import (
	"encoding/json"
	"testing"
)

// A nil slice marshals to JSON null, which clients that expect a list fail to
// cast -- this is what crashed the mobile Vault page with "type 'Null' is not
// a subtype of type 'List<dynamic>'". The empty case has to serialise as [].
func TestListMessagesSerialisesAsEmptyArrayNotNull(t *testing.T) {
	b := NewBus()

	got := b.ListMessages(ctx(), "", 50)
	if got == nil {
		t.Fatal("ListMessages returned a nil slice; it must be empty but non-nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected no messages, got %d", len(got))
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != "[]" {
		t.Fatalf("serialised as %s, want []", raw)
	}
}

// Same guarantee when messages exist but none match the filter, which is the
// more common way to reach the empty case in practice.
func TestListMessagesSerialisesAsEmptyArrayWhenFilterMatchesNothing(t *testing.T) {
	b := NewBus()
	b.SendMessage(ctx(), "inst-a", "Alpha", "inst-b", "message", "hello", nil)

	raw, err := json.Marshal(b.ListMessages(ctx(), "inst-zzz", 50))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != "[]" {
		t.Fatalf("serialised as %s, want []", raw)
	}
}
