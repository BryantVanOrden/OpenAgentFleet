package protocol

import "time"

// Who may do what, to which bots, in which part of the organisation.
//
// Two levels, deliberately. An org role answers "what does someone at this
// level normally do", which is the question an administrator wants to answer
// once for a department. A per-bot grant answers "except for this one", which
// is the exception that always turns up — a contractor who may drive one
// machine, an auditor who may read one bot's transcripts and nothing else.
//
// Permissions are named rather than a bitmask so a row in the database is
// legible during an incident, when working out why someone could do something
// matters more than the bytes it took to store.

// Permission is one thing a user may do with one bot.
type Permission string

const (
	// PermView is knowing the bot exists, and seeing its name and state.
	PermView Permission = "view"
	// PermRead is reading its transcripts and task history without being able
	// to say anything to it. This is the auditor's permission.
	PermRead Permission = "read"
	// PermChat is talking to it, which can cause it to act.
	PermChat Permission = "chat"
	// PermDesktop is taking over its screen: full control of a live machine,
	// so it is separate from chat rather than implied by it.
	PermDesktop Permission = "desktop"
	// PermEdit is changing its configuration — models, voice, access.
	PermEdit Permission = "edit"
	// PermDelete is destroying it and its workspace.
	PermDelete Permission = "delete"
	// PermCreate is making new bots in an org. Org-level; it has no per-bot
	// meaning.
	PermCreate Permission = "create"
	// PermSecrets is reading and managing the org's shared credentials and
	// browser sessions.
	PermSecrets Permission = "secrets"
	// PermManageMembers is adding people to the org and changing what they
	// may do.
	PermManageMembers Permission = "manage_members"
)

// AllPermissions is every permission, in the order a picker should show them:
// least to most dangerous.
var AllPermissions = []Permission{
	PermView, PermRead, PermChat, PermDesktop,
	PermEdit, PermCreate, PermDelete, PermSecrets, PermManageMembers,
}

// PermissionLabels describe each permission in the terms an administrator
// thinks in rather than the code's.
var PermissionLabels = map[Permission]string{
	PermView:          "See the bot exists",
	PermRead:          "Read its replies and history",
	PermChat:          "Talk to it",
	PermDesktop:       "Use its desktop",
	PermEdit:          "Change its settings",
	PermCreate:        "Create new bots",
	PermDelete:        "Delete bots",
	PermSecrets:       "Use shared secrets and sessions",
	PermManageMembers: "Manage who has access",
}

// OrgRole is a member's standing in an org, which supplies default permissions.
type OrgRole string

const (
	OrgRoleOwner  OrgRole = "owner"
	OrgRoleAdmin  OrgRole = "admin"
	OrgRoleMember OrgRole = "member"
	OrgRoleViewer OrgRole = "viewer"
)

// OrgRoles lists roles from most to least capable.
var OrgRoles = []OrgRole{OrgRoleOwner, OrgRoleAdmin, OrgRoleMember, OrgRoleViewer}

// orgRoleDefaults is what each role may do to the org's bots by default.
//
// Viewer deliberately stops at read: someone brought in to check what the
// fleet has been doing should not be able to make it do more. Member gets the
// desktop because driving a bot is the normal way to work with one, but not
// edit or delete, which change or destroy other people's work.
var orgRoleDefaults = map[OrgRole][]Permission{
	OrgRoleOwner: {
		PermView, PermRead, PermChat, PermDesktop,
		PermEdit, PermCreate, PermDelete, PermSecrets, PermManageMembers,
	},
	OrgRoleAdmin: {
		PermView, PermRead, PermChat, PermDesktop,
		PermEdit, PermCreate, PermDelete, PermSecrets,
	},
	OrgRoleMember: {PermView, PermRead, PermChat, PermDesktop},
	OrgRoleViewer: {PermView, PermRead},
}

// DefaultPermissions returns what an org role may do by default.
func DefaultPermissions(role OrgRole) []Permission {
	perms, ok := orgRoleDefaults[role]
	if !ok {
		// Fail closed. An unrecognised role grants nothing rather than
		// everything, so a typo in a stored role cannot open a door.
		return nil
	}
	out := make([]Permission, len(perms))
	copy(out, perms)
	return out
}

// Org is an organisation or department.
type Org struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`

	// Filled in on read.
	MemberCount int `json:"member_count"`
	BotCount    int `json:"bot_count"`
}

// OrgMember is one person's standing in one org.
type OrgMember struct {
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email,omitempty"`
	OrgRole   OrgRole   `json:"org_role"`
	CreatedAt time.Time `json:"created_at"`
}

// BotGrant is a per-bot exception to what a member may do.
type BotGrant struct {
	UserID      string       `json:"user_id"`
	InstanceID  string       `json:"instance_id"`
	Permissions []Permission `json:"permissions"`
	CreatedAt   time.Time    `json:"created_at"`
}

// Access is everything needed to answer "may this user do this to this bot"
// without another database round trip per check.
//
// Resolved once per request and carried on the context: a list endpoint checks
// every bot it is about to return, and doing that with a query each would make
// the permission system the slowest thing in the API.
type Access struct {
	UserID string
	// GlobalAdmin bypasses every check. The operator who owns the deployment
	// is not locked out of their own fleet by a misconfigured org.
	GlobalAdmin bool
	// OrgRoles maps org ID to the role held there.
	OrgRoles map[string]OrgRole
	// Grants maps instance ID to an explicit per-bot permission set, which
	// overrides the org default for that bot.
	Grants map[string][]Permission
}

// InOrg reports membership.
func (a Access) InOrg(orgID string) bool {
	if orgID == "" {
		return a.GlobalAdmin
	}
	_, ok := a.OrgRoles[orgID]
	return ok || a.GlobalAdmin
}

// Can reports whether the user may do this to a bot in this org.
//
// A per-bot grant wins outright, including when it is empty — that is how a
// single bot is hidden from someone who can otherwise see the whole org.
func (a Access) Can(perm Permission, orgID, instanceID string) bool {
	if a.GlobalAdmin {
		return true
	}
	if instanceID != "" {
		if grant, ok := a.Grants[instanceID]; ok {
			return hasPerm(grant, perm)
		}
	}
	role, ok := a.OrgRoles[orgID]
	if !ok {
		// Not a member. An unassigned bot (no org) is visible only to a global
		// admin, which the check above has already handled.
		return false
	}
	return hasPerm(DefaultPermissions(role), perm)
}

// CanInOrg is Can for something that is not a single bot — creating a bot,
// reading the org's secrets, managing its members.
func (a Access) CanInOrg(perm Permission, orgID string) bool {
	return a.Can(perm, orgID, "")
}

// PermissionsFor lists what the user may do to one bot, for the UI to render
// without guessing at the rules.
func (a Access) PermissionsFor(orgID, instanceID string) []Permission {
	if a.GlobalAdmin {
		return DefaultPermissions(OrgRoleOwner)
	}
	if grant, ok := a.Grants[instanceID]; ok {
		out := make([]Permission, len(grant))
		copy(out, grant)
		return out
	}
	if role, ok := a.OrgRoles[orgID]; ok {
		return DefaultPermissions(role)
	}
	return nil
}

func hasPerm(list []Permission, want Permission) bool {
	for _, p := range list {
		if p == want {
			return true
		}
	}
	return false
}

// ValidPermission reports whether a name is one this system knows.
//
// Used when accepting a grant from a client: an unknown permission name stored
// today is a permission that might mean something tomorrow.
func ValidPermission(p Permission) bool {
	for _, known := range AllPermissions {
		if known == p {
			return true
		}
	}
	return false
}

// ValidOrgRole reports whether a role name is known.
func ValidOrgRole(r OrgRole) bool {
	for _, known := range OrgRoles {
		if known == r {
			return true
		}
	}
	return false
}
