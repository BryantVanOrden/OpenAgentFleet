package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Managing organisations, their members, and per-bot exceptions.

func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	all, err := s.db.ListOrgs(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	// You see the departments you are in. A global admin sees all of them,
	// which is how a new deployment is set up in the first place.
	acc := accessFrom(r.Context())
	out := make([]protocol.Org, 0, len(all))
	for _, o := range all {
		if acc.InOrg(o.ID) {
			out = append(out, o)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpsertOrg(w http.ResponseWriter, r *http.Request) {
	var o protocol.Org
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if id := r.PathValue("id"); id != "" {
		o.ID = id
	}
	o.Name = strings.TrimSpace(o.Name)
	if o.Name == "" {
		fail(w, http.StatusBadRequest, "an organisation needs a name")
		return
	}

	acc := accessFrom(r.Context())
	// Editing an existing org needs standing in it; creating one is a
	// deployment-level act, so it stays with the global admin.
	if o.ID != "" {
		if !acc.CanInOrg(protocol.PermManageMembers, o.ID) {
			fail(w, http.StatusForbidden, "you cannot manage that organisation")
			return
		}
	} else if !acc.GlobalAdmin {
		fail(w, http.StatusForbidden, "only an administrator can create an organisation")
		return
	}

	creating := o.ID == ""
	if err := s.db.UpsertOrg(r.Context(), &o); err != nil {
		failErr(w, err)
		return
	}
	// Whoever creates an org owns it. Otherwise the creator would immediately
	// lose sight of it, since listing is filtered by membership.
	if creating {
		if c := userFrom(r.Context()); c != nil {
			_ = s.db.UpsertOrgMember(r.Context(), protocol.OrgMember{
				OrgID: o.ID, UserID: c.Subject, OrgRole: protocol.OrgRoleOwner,
			})
		}
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) handleDeleteOrg(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !accessFrom(r.Context()).CanInOrg(protocol.PermManageMembers, id) {
		fail(w, http.StatusForbidden, "you cannot manage that organisation")
		return
	}
	if err := s.db.DeleteOrg(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("id")
	if !accessFrom(r.Context()).InOrg(orgID) {
		fail(w, http.StatusNotFound, "no such organisation")
		return
	}
	members, err := s.db.ListOrgMembers(r.Context(), orgID)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

type orgMemberReq struct {
	UserID  string           `json:"user_id"`
	OrgRole protocol.OrgRole `json:"org_role"`
}

func (s *Server) handleUpsertOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("id")
	if !accessFrom(r.Context()).CanInOrg(protocol.PermManageMembers, orgID) {
		fail(w, http.StatusForbidden, "you cannot manage members of that organisation")
		return
	}

	var req orgMemberReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" {
		fail(w, http.StatusBadRequest, "a user is required")
		return
	}
	if !protocol.ValidOrgRole(req.OrgRole) {
		fail(w, http.StatusBadRequest, "unknown role: "+string(req.OrgRole))
		return
	}
	if _, err := s.db.UserByID(r.Context(), req.UserID); err != nil {
		failErr(w, err)
		return
	}

	m := protocol.OrgMember{OrgID: orgID, UserID: req.UserID, OrgRole: req.OrgRole}
	if err := s.db.UpsertOrgMember(r.Context(), m); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleRemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("id")
	if !accessFrom(r.Context()).CanInOrg(protocol.PermManageMembers, orgID) {
		fail(w, http.StatusForbidden, "you cannot manage members of that organisation")
		return
	}
	if err := s.db.RemoveOrgMember(r.Context(), orgID, r.PathValue("userID")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetInstanceOrg moves a bot between departments.
func (s *Server) handleSetInstanceOrg(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit)
	if !ok {
		return
	}

	var req struct {
		// OrgIDs is the full set of departments this bot belongs to. Sent
		// whole rather than as add/remove: two admins editing at once should
		// disagree about the result, not silently compose into a third set
		// neither of them chose.
		OrgIDs []string `json:"org_ids"`
		// OrgID is the single-department form this endpoint used to take.
		// Still accepted so an older client keeps working.
		OrgID string `json:"org_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	orgs := req.OrgIDs
	if orgs == nil && req.OrgID != "" {
		orgs = []string{req.OrgID}
	}

	// Every department involved has to be one you may act in — both the ones
	// being added and the ones being removed.
	//
	// Adding: putting a bot into a department you cannot manage hands it to
	// people you have no standing over. Removing: taking a bot out of a
	// department you are not in makes it vanish for people who were relying
	// on it, and you would never see that it had.
	acc := accessFrom(r.Context())
	touched := map[string]bool{}
	for _, id := range orgs {
		touched[id] = true
	}
	for _, id := range inst.OrgIDs {
		touched[id] = true
	}
	for id := range touched {
		if !acc.CanInOrg(protocol.PermCreate, id) {
			fail(w, http.StatusForbidden,
				"you cannot change this bot's membership of that organisation")
			return
		}
	}

	if err := s.db.SetInstanceOrgs(r.Context(), inst.ID, orgs); err != nil {
		failErr(w, err)
		return
	}
	inst.OrgIDs = orgs
	writeJSON(w, http.StatusOK, redact(*inst))
}

// handleListBotGrants shows the per-bot exceptions on one bot.
func (s *Server) handleListBotGrants(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit)
	if !ok {
		return
	}
	grants, err := s.db.ListBotGrants(r.Context(), inst.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

type botGrantReq struct {
	UserID string `json:"user_id"`
	// Permissions replaces the grant. Null removes it, falling back to the
	// org default; an empty list explicitly allows nothing, which is how a
	// single bot is hidden from someone who can see the rest of their org.
	Permissions *[]protocol.Permission `json:"permissions"`
}

func (s *Server) handleSetBotGrant(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit)
	if !ok {
		return
	}
	// Handing out access is a stronger act than editing settings, so it needs
	// the permission that governs access rather than the one that governs
	// configuration.
	if !accessFrom(r.Context()).Can(protocol.PermManageMembers, inst.OrgIDs, "") {
		fail(w, http.StatusForbidden, "you cannot change who has access to this bot")
		return
	}

	var req botGrantReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" {
		fail(w, http.StatusBadRequest, "a user is required")
		return
	}
	if _, err := s.db.UserByID(r.Context(), req.UserID); err != nil {
		failErr(w, err)
		return
	}

	g := protocol.BotGrant{UserID: req.UserID, InstanceID: inst.ID}
	if req.Permissions != nil {
		for _, p := range *req.Permissions {
			if !protocol.ValidPermission(p) {
				// Storing a name this system does not know is storing a
				// permission that might mean something after the next upgrade.
				fail(w, http.StatusBadRequest, "unknown permission: "+string(p))
				return
			}
		}
		g.Permissions = *req.Permissions
	}
	if err := s.db.SetBotGrant(r.Context(), g); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// handleMyPermissions tells the app what this user may do, so the UI can hide
// what it must rather than offering actions that will be refused.
func (s *Server) handleMyPermissions(w http.ResponseWriter, r *http.Request) {
	acc := accessFrom(r.Context())
	instances, err := s.db.ListInstances(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}

	perBot := map[string][]protocol.Permission{}
	for _, in := range instances {
		if p := acc.PermissionsFor(in.OrgIDs, in.ID); len(p) > 0 {
			perBot[in.ID] = p
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":      acc.UserID,
		"global_admin": acc.GlobalAdmin,
		"org_roles":    acc.OrgRoles,
		"bots":         perBot,
	})
}
