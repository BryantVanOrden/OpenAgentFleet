package protocol

import "testing"

func access(role OrgRole, orgID string) Access {
	return Access{
		UserID:   "u1",
		OrgRoles: map[string]OrgRole{orgID: role},
		Grants:   map[string][]Permission{},
	}
}

// The whole point: someone in one org sees nothing of another.
func TestNoCrossOrgAccess(t *testing.T) {
	a := access(OrgRoleOwner, "org-a")

	if !a.Can(PermView, "org-a", "bot-1") {
		t.Error("an owner cannot see a bot in their own org")
	}
	for _, perm := range AllPermissions {
		if a.Can(perm, "org-b", "bot-2") {
			t.Errorf("an owner of org-a has %q in org-b", perm)
		}
	}
}

// A viewer may read and nothing more. Someone brought in to check what the
// fleet did must not be able to make it do more.
func TestViewerCannotAct(t *testing.T) {
	a := access(OrgRoleViewer, "org-a")

	for _, allowed := range []Permission{PermView, PermRead} {
		if !a.Can(allowed, "org-a", "bot-1") {
			t.Errorf("a viewer lacks %q", allowed)
		}
	}
	for _, denied := range []Permission{
		PermChat, PermDesktop, PermEdit, PermDelete, PermCreate,
		PermSecrets, PermManageMembers,
	} {
		if a.Can(denied, "org-a", "bot-1") {
			t.Errorf("a viewer has %q", denied)
		}
	}
}

// Driving a bot and reconfiguring it are different things.
func TestMemberDrivesButCannotEditOrDelete(t *testing.T) {
	a := access(OrgRoleMember, "org-a")

	if !a.Can(PermDesktop, "org-a", "bot-1") {
		t.Error("a member cannot use the desktop, which is the normal way to work")
	}
	if !a.Can(PermChat, "org-a", "bot-1") {
		t.Error("a member cannot talk to a bot")
	}
	for _, denied := range []Permission{PermEdit, PermDelete, PermCreate, PermSecrets} {
		if a.Can(denied, "org-a", "bot-1") {
			t.Errorf("a member has %q", denied)
		}
	}
}

// Only an owner manages who else has access.
func TestOnlyOwnerManagesMembers(t *testing.T) {
	if access(OrgRoleAdmin, "org-a").CanInOrg(PermManageMembers, "org-a") {
		t.Error("an org admin can manage members")
	}
	if !access(OrgRoleOwner, "org-a").CanInOrg(PermManageMembers, "org-a") {
		t.Error("an owner cannot manage members")
	}
}

// A per-bot grant overrides the org default, in both directions.
func TestGrantWidensAndNarrows(t *testing.T) {
	// A viewer given the desktop on one machine.
	widened := access(OrgRoleViewer, "org-a")
	widened.Grants["bot-1"] = []Permission{PermView, PermRead, PermDesktop}
	if !widened.Can(PermDesktop, "org-a", "bot-1") {
		t.Error("a grant did not widen a viewer's access")
	}
	// ...and still cannot drive anything else.
	if widened.Can(PermDesktop, "org-a", "bot-2") {
		t.Error("a grant on one bot leaked to another")
	}

	// An owner cut back on one sensitive machine.
	narrowed := access(OrgRoleOwner, "org-a")
	narrowed.Grants["bot-secret"] = []Permission{PermView}
	if narrowed.Can(PermDesktop, "org-a", "bot-secret") {
		t.Error("a narrowing grant did not restrict an owner")
	}
	if !narrowed.Can(PermDesktop, "org-a", "bot-other") {
		t.Error("narrowing one bot restricted the rest")
	}
}

// An empty grant is how a single bot is hidden from someone who can otherwise
// see the whole org. It must not fall through to the org default.
func TestEmptyGrantHidesABot(t *testing.T) {
	a := access(OrgRoleOwner, "org-a")
	a.Grants["bot-hidden"] = []Permission{}

	for _, perm := range AllPermissions {
		if a.Can(perm, "org-a", "bot-hidden") {
			t.Errorf("an empty grant still allowed %q", perm)
		}
	}
}

// The deployment's owner must not be locked out by a misconfigured org.
func TestGlobalAdminBypasses(t *testing.T) {
	a := Access{UserID: "root", GlobalAdmin: true}
	for _, perm := range AllPermissions {
		if !a.Can(perm, "any-org", "any-bot") {
			t.Errorf("a global admin lacks %q", perm)
		}
	}
	if !a.InOrg("some-org") {
		t.Error("a global admin is not treated as in every org")
	}
}

// A bot with no org is visible only to a global admin, so a fleet that
// predates orgs does not become invisible — nor world-readable.
func TestUnassignedBotIsAdminOnly(t *testing.T) {
	a := access(OrgRoleOwner, "org-a")
	if a.Can(PermView, "", "orphan") {
		t.Error("an org owner can see a bot belonging to no org")
	}
	admin := Access{GlobalAdmin: true}
	if !admin.Can(PermView, "", "orphan") {
		t.Error("a global admin cannot see an unassigned bot")
	}
}

// Fail closed: a role that is not recognised grants nothing. A typo in a
// stored role must not open a door.
func TestUnknownRoleGrantsNothing(t *testing.T) {
	a := Access{OrgRoles: map[string]OrgRole{"org-a": OrgRole("superuser")}}
	for _, perm := range AllPermissions {
		if a.Can(perm, "org-a", "bot-1") {
			t.Errorf("an unknown role granted %q", perm)
		}
	}
	if DefaultPermissions(OrgRole("nonsense")) != nil {
		t.Error("an unknown role returned permissions")
	}
}

// A caller with no membership at all gets nothing.
func TestNoMembershipGrantsNothing(t *testing.T) {
	a := Access{UserID: "u1"}
	for _, perm := range AllPermissions {
		if a.Can(perm, "org-a", "bot-1") {
			t.Errorf("a non-member has %q", perm)
		}
	}
}

// Mutating the returned slice must not change the defaults for everyone else.
func TestDefaultPermissionsAreCopied(t *testing.T) {
	first := DefaultPermissions(OrgRoleViewer)
	first[0] = PermDelete
	second := DefaultPermissions(OrgRoleViewer)
	if second[0] == PermDelete {
		t.Fatal("the defaults table was mutated through a returned slice")
	}
}

func TestValidation(t *testing.T) {
	if !ValidPermission(PermDesktop) || ValidPermission(Permission("root")) {
		t.Error("permission validation is wrong")
	}
	if !ValidOrgRole(OrgRoleOwner) || ValidOrgRole(OrgRole("god")) {
		t.Error("role validation is wrong")
	}
}
