package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The shared fleet vault handlers work off the vault.GlobalBus package global,
// so each test cleans up the keys it writes.
func withSecret(t *testing.T, key, value, scope, note string) {
	t.Helper()
	vault.GlobalBus.PutSecret(context.Background(), key, value, scope, note, "inst-a", "")
	t.Cleanup(func() { vault.GlobalBus.DeleteSecret(context.Background(), key) })
}

const canarySecret = "sk_live_CANARY_9f8e7d6c5b4a"

// ------------------------------------------------------- secret redaction ---

// GET /api/vault/secrets is gated at roleAny, which includes the read-only
// auditor role. It must never put credential plaintext on the wire.
func TestSharedSecretValueNeverReachesTheAPI(t *testing.T) {
	withSecret(t, "test.stripe.key", canarySecret, "fleet", "billing")

	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleListSharedSecrets(rec, asAdmin(httptest.NewRequest(http.MethodGet, "/api/vault/secrets", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, canarySecret) {
		t.Fatalf("GET /api/vault/secrets leaked the secret value in its body:\n%s", body)
	}

	// The metadata still has to be there, or the endpoint is useless.
	var got []sharedSecretView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not a JSON array of secret views: %v", err)
	}
	var found *sharedSecretView
	for i := range got {
		if got[i].Key == "test.stripe.key" {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatal("the secret's metadata is missing from the listing")
	}
	if found.Scope != "fleet" || found.Note != "billing" || found.CreatedBy != "inst-a" {
		t.Errorf("metadata lost in the projection: %+v", *found)
	}
	if !found.HasValue {
		t.Error("has_value should report that a value is set without revealing it")
	}
}

// The PUT response body is the other place plaintext used to escape; response
// bodies land in proxy logs, so the echo is redacted too.
func TestPutSharedSecretResponseIsRedacted(t *testing.T) {
	t.Cleanup(func() { vault.GlobalBus.DeleteSecret(context.Background(), "test.echo.key") })

	body := `{"key":"test.echo.key","value":"` + canarySecret + `","scope":"fleet","note":"n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/vault/secrets", strings.NewReader(body))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	s := &Server{}
	s.handlePutSharedSecret(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), canarySecret) {
		t.Errorf("PUT echoed the plaintext back:\n%s", rec.Body.String())
	}

	// It was still stored: redaction is a wire concern, not a storage one.
	stored, ok := vault.GlobalBus.GetSecret(context.Background(), "test.echo.key")
	if !ok || stored.Value != canarySecret {
		t.Errorf("secret was not stored intact: %+v ok=%v", stored, ok)
	}
}

// The redaction must hold for every field of the projection, so that adding a
// field to protocol.SharedSecret cannot silently reintroduce the leak.
func TestRedactSharedSecretDropsOnlyTheValue(t *testing.T) {
	sec := protocol.SharedSecret{
		Key:       "k",
		Value:     canarySecret,
		Scope:     "swarm:s1",
		Note:      "note",
		CreatedBy: "inst-a",
	}
	blob, err := json.Marshal(redactSharedSecret(sec))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), canarySecret) {
		t.Fatalf("redacted view still serialises the value: %s", blob)
	}
	for _, want := range []string{`"k"`, `"swarm:s1"`, `"note"`, `"inst-a"`, `"has_value":true`} {
		if !strings.Contains(string(blob), want) {
			t.Errorf("redacted view dropped %s: %s", want, blob)
		}
	}

	// An unset value reports has_value:false rather than pretending it exists.
	sec.Value = ""
	blob, _ = json.Marshal(redactSharedSecret(sec))
	if !strings.Contains(string(blob), `"has_value":false`) {
		t.Errorf("empty value should report has_value:false: %s", blob)
	}
}

// ----------------------------------------------------- errors do not leak ---

// A rejected write must not quote the payload back. This is the same class of
// bug as the API key that reached a task's persisted error column via a URL.
func TestSharedSecretErrorsDoNotEchoTheValue(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		// Missing key, value present — the tempting thing to put in the error.
		{"missing key", `{"key":"","value":"` + canarySecret + `"}`, http.StatusBadRequest},
		// Malformed JSON with the secret embedded in the broken document.
		{"malformed json", `{"key":"a","value":"` + canarySecret + `"`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/vault/secrets", strings.NewReader(tc.body))
			req = asAdmin(req)
			rec := httptest.NewRecorder()

			s := &Server{}
			s.handlePutSharedSecret(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if strings.Contains(rec.Body.String(), canarySecret) {
				t.Errorf("error response leaked the secret value:\n%s", rec.Body.String())
			}
		})
	}
}

// ------------------------------------------------------------ peer comms ---

func TestSendPeerMessageDefaultsToBroadcast(t *testing.T) {
	body := `{"from_instance_id":"inst-a","content":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/vault/comms", strings.NewReader(body))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	s := &Server{}
	s.handleSendPeerMessage(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var msg protocol.PeerMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	// An omitted recipient becomes a fleet-wide broadcast rather than a
	// message addressed to the empty instance id.
	if msg.ToInstanceID != "broadcast" {
		t.Errorf("ToInstanceID = %q, want %q", msg.ToInstanceID, "broadcast")
	}
	if msg.FromInstanceName != "Operator" {
		t.Errorf("FromInstanceName = %q, want the %q default", msg.FromInstanceName, "Operator")
	}
}

func TestSendPeerMessageRequiresContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/vault/comms", strings.NewReader(`{"from_instance_id":"inst-a"}`))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	s := &Server{}
	s.handleSendPeerMessage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty content: status = %d, want 400", rec.Code)
	}
}

// ------------------------------------------------------- session handoff ---

func TestSaveSharedSessionRequiresDomainAndCookies(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"no domain", `{"cookies_json":"[]"}`, http.StatusBadRequest},
		{"no cookies", `{"domain":"example.com"}`, http.StatusBadRequest},
		{"ok", `{"domain":"example.com","cookies_json":"[]"}`, http.StatusCreated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/vault/sessions", strings.NewReader(tc.body))
			req = asAdmin(req)
			rec := httptest.NewRecorder()

			s := &Server{}
			s.handleSaveSharedSession(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// A cookie blob with unicode and expiry data must survive the HTTP layer
// unchanged, since the far side re-imports it verbatim.
func TestSharedSessionBlobSurvivesTheHTTPLayer(t *testing.T) {
	cookies := `[{"name":"sid","value":"héllo 世界 🌍","expires":1893456000,"secure":true}]`
	payload, err := json.Marshal(SaveSharedSessionReq{
		Domain:      "unicode.test",
		Title:       "t",
		CookiesJSON: cookies,
		CreatedBy:   "inst-a",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/vault/sessions", strings.NewReader(string(payload)))
	req = asAdmin(req)
	rec := httptest.NewRecorder()

	s := &Server{}
	s.handleSaveSharedSession(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var got protocol.SharedSession
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CookiesJSON != cookies {
		t.Errorf("cookie blob altered by the HTTP round-trip:\n got %q\nwant %q", got.CookiesJSON, cookies)
	}
}
