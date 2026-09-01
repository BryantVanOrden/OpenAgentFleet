package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func inst(id, orgID string) protocol.Instance {
	return protocol.Instance{ID: id, Name: id, OrgIDs: []string{orgID}}
}

// A listing must return what the caller may see, not everything with the rest
// hidden client-side — otherwise every org's bot names go over the wire.
func TestListingIsFilteredNotJustGated(t *testing.T) {
	all := []protocol.Instance{
		inst("a1", "org-a"), inst("a2", "org-a"),
		inst("b1", "org-b"),
		inst("orphan", ""),
	}

	member := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-a": protocol.OrgRoleMember},
		Grants:   map[string][]protocol.Permission{},
	}
	got := visibleInstances(member, all)
	if len(got) != 2 {
		t.Fatalf("org-a member sees %d bots, want 2: %+v", len(got), got)
	}
	for _, in := range got {
		if len(in.OrgIDs) != 1 || in.OrgIDs[0] != "org-a" {
			t.Errorf("leaked %s from %v", in.ID, in.OrgIDs)
		}
	}

	// A global admin sees everything, including the unassigned one.
	if len(visibleInstances(protocol.Access{GlobalAdmin: true}, all)) != 4 {
		t.Error("a global admin cannot see the whole fleet")
	}

	// Someone with no membership sees nothing.
	if len(visibleInstances(protocol.Access{}, all)) != 0 {
		t.Error("a user with no membership saw bots")
	}
}

// An empty grant hides one bot from someone who can otherwise see its org.
func TestGrantHidesOneBotFromAListing(t *testing.T) {
	all := []protocol.Instance{inst("a1", "org-a"), inst("a2", "org-a")}
	acc := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-a": protocol.OrgRoleOwner},
		Grants:   map[string][]protocol.Permission{"a2": {}},
	}
	got := visibleInstances(acc, all)
	if len(got) != 1 || got[0].ID != "a1" {
		t.Errorf("expected only a1, got %+v", got)
	}
}

// Credentials are whitelisted: an unfiled secret is admin-only rather than
// everyone's because nobody has assigned it yet.
func TestSecretsAreScopedToOrgs(t *testing.T) {
	all := []protocol.SharedSecret{
		{Key: "a", OrgID: "org-a"},
		{Key: "b", OrgID: "org-b"},
		{Key: "unfiled"},
	}

	// A member does not get PermSecrets at all.
	member := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-a": protocol.OrgRoleMember},
	}
	if got := visibleSecrets(member, all); len(got) != 0 {
		t.Errorf("a member saw %d secrets, want 0", len(got))
	}

	// An org admin sees their own org's and nothing else.
	admin := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-a": protocol.OrgRoleAdmin},
	}
	got := visibleSecrets(admin, all)
	if len(got) != 1 || got[0].Key != "a" {
		t.Errorf("org-a admin saw %+v, want just a", got)
	}

	if len(visibleSecrets(protocol.Access{GlobalAdmin: true}, all)) != 3 {
		t.Error("a global admin cannot see every secret")
	}
}

func TestSessionsAreScopedToOrgs(t *testing.T) {
	all := []protocol.SharedSession{
		{ID: "s1", OrgID: "org-a"},
		{ID: "s2", OrgID: "org-b"},
	}
	admin := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-b": protocol.OrgRoleAdmin},
	}
	got := visibleSessions(admin, all)
	if len(got) != 1 || got[0].ID != "s2" {
		t.Errorf("saw %+v, want just s2", got)
	}
}

// A handler reached without the auth middleware must deny, not allow. This is
// the property that makes a forgotten route safe by default.
func TestMissingAccessDeniesEverything(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/instances", nil)
	acc := accessFrom(r.Context())

	if acc.GlobalAdmin {
		t.Fatal("a request with no access resolved as admin")
	}
	for _, perm := range protocol.AllPermissions {
		if acc.Can(perm, []string{"org-a"}, "bot-1") {
			t.Errorf("a request with no access had %q", perm)
		}
	}
}

// Creating into an org you are not in is refused.
func TestCreateIsGatedOnTheTargetOrg(t *testing.T) {
	member := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-a": protocol.OrgRoleMember},
	}
	if member.CanInOrg(protocol.PermCreate, "org-a") {
		t.Error("a plain member can create bots")
	}
	orgAdmin := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-a": protocol.OrgRoleAdmin},
	}
	if !orgAdmin.CanInOrg(protocol.PermCreate, "org-a") {
		t.Error("an org admin cannot create bots in their own org")
	}
	if orgAdmin.CanInOrg(protocol.PermCreate, "org-b") {
		t.Error("an org admin can create bots in another org")
	}
}

// Watching a desktop and driving it are separate permissions, because a
// takeover is full control of a live machine.
func TestDesktopIsSeparateFromRead(t *testing.T) {
	viewer := protocol.Access{
		OrgRoles: map[string]protocol.OrgRole{"org-a": protocol.OrgRoleViewer},
	}
	if !viewer.Can(protocol.PermRead, []string{"org-a"}, "bot-1") {
		t.Error("a viewer cannot read")
	}
	if viewer.Can(protocol.PermDesktop, []string{"org-a"}, "bot-1") {
		t.Error("a viewer can drive a desktop")
	}
}
