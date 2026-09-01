package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The shared work catalog: where agents publish what they have made, for each
// other and for you.

// visibleWork filters the catalog to what the caller may see.
//
// Scoped like secrets: an item filed to a department is visible to that
// department, and an unfiled item is admin-only. Filtered here rather than in
// the client, or every department's work would go over the wire to everyone.
func visibleWork(acc protocol.Access, all []protocol.WorkItem, authorOrgs map[string][]string) []protocol.WorkItem {
	out := make([]protocol.WorkItem, 0, len(all))
	for _, w := range all {
		if canSeeWork(acc, w, authorOrgs) {
			out = append(out, w)
		}
	}
	return out
}

// canSeeWork answers for one item.
//
// An item filed to a department is that department's. An item with no
// department falls back to the departments of the bot that made it -- which
// matters because a bot shared between two departments files its work
// nowhere: there is no single department a shared bot's output belongs to, and
// picking one would hand it to the wrong people. Without this fallback the
// exact arrangement multi-department bots exist to enable made that bot's
// output invisible to both of them.
//
// If you may see the bot, you may see what it published. Work the operator
// filed nowhere stays admin-only, as an unfiled secret does.
func canSeeWork(acc protocol.Access, w protocol.WorkItem, authorOrgs map[string][]string) bool {
	if acc.GlobalAdmin {
		return true
	}
	if w.OrgID != "" {
		return acc.CanInOrg(protocol.PermRead, w.OrgID)
	}
	if w.CreatedBy == "" {
		// Published by a person into no department.
		return false
	}
	return acc.Can(protocol.PermRead, authorOrgs[w.CreatedBy], w.CreatedBy)
}

// workAuthorOrgs maps each bot to the departments it belongs to, for the
// fallback above. One query for the fleet rather than one per item.
func (s *Server) workAuthorOrgs(ctx context.Context) map[string][]string {
	instances, err := s.db.ListInstances(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string][]string, len(instances))
	for _, in := range instances {
		out[in.ID] = in.OrgIDs
	}
	return out
}

func (s *Server) handleListWork(w http.ResponseWriter, r *http.Request) {
	all, err := s.db.ListWorkItems(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK,
		visibleWork(accessFrom(r.Context()), all, s.workAuthorOrgs(r.Context())))
}

func (s *Server) handleGetWork(w http.ResponseWriter, r *http.Request) {
	item, err := s.db.WorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	// 404 rather than 403 for something you may not see: telling someone an
	// item exists but is not theirs is itself a disclosure.
	if !canSeeWork(accessFrom(r.Context()), *item, s.workAuthorOrgs(r.Context())) {
		fail(w, http.StatusNotFound, "no such work item")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handlePutWork(w http.ResponseWriter, r *http.Request) {
	var req protocol.WorkItem
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Kind == "" {
		req.Kind = protocol.WorkFile
	}
	if !protocol.ValidWorkKind(req.Kind) {
		fail(w, http.StatusBadRequest, "unknown kind: "+req.Kind)
		return
	}
	// Publishing into a department you are not in would put work somewhere you
	// cannot then see it, and hand it to people you have no standing over.
	if req.OrgID != "" && !accessFrom(r.Context()).CanInOrg(protocol.PermRead, req.OrgID) {
		fail(w, http.StatusForbidden, "you cannot publish into that department")
		return
	}
	if req.CreatedBy == "" {
		req.CreatedByName = userFrom(r.Context()).Email
	}

	if err := s.db.PutWorkItem(r.Context(), &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.bus.Emit("work", req.CreatedBy, "", req)
	writeJSON(w, http.StatusOK, req)
}

// handleMoveWork renames an item, moves it, or both.
//
// Separate from publishing because publishing addresses an item by name: a
// rename that way leaves the old name behind and a move makes a second copy.
func (s *Server) handleMoveWork(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.db.WorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	acc := accessFrom(r.Context())
	if !canSeeWork(acc, *item, s.workAuthorOrgs(r.Context())) {
		fail(w, http.StatusNotFound, "no such work item")
		return
	}
	if item.OrgID != "" && !acc.CanInOrg(protocol.PermEdit, item.OrgID) {
		fail(w, http.StatusForbidden, "you cannot change work in that department")
		return
	}
	// A missing parent_id means "leave it where it is"; an explicit empty one
	// means the top level. Without the distinction every rename would drag the
	// item out of its folder.
	parent := item.ParentID
	if req.ParentID != nil {
		parent = *req.ParentID
	}
	if parent != "" {
		dest, err := s.db.WorkItem(r.Context(), parent)
		if err != nil {
			fail(w, http.StatusBadRequest, "no such folder")
			return
		}
		if !canSeeWork(acc, *dest, s.workAuthorOrgs(r.Context())) {
			fail(w, http.StatusNotFound, "no such folder")
			return
		}
	}
	moved, err := s.db.MoveWorkItem(r.Context(), item.ID, req.Name, parent)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.bus.Emit("work", "", "", *moved)
	writeJSON(w, http.StatusOK, moved)
}

func (s *Server) handleDeleteWork(w http.ResponseWriter, r *http.Request) {
	item, err := s.db.WorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	acc := accessFrom(r.Context())
	if !canSeeWork(acc, *item, s.workAuthorOrgs(r.Context())) {
		fail(w, http.StatusNotFound, "no such work item")
		return
	}
	// Seeing something is not the same as being allowed to destroy it.
	if item.OrgID != "" && !acc.CanInOrg(protocol.PermEdit, item.OrgID) {
		fail(w, http.StatusForbidden, "you cannot delete work in that department")
		return
	}
	if err := s.db.DeleteWorkItem(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// renderableApp reports whether an item is something the app can run.
//
// Kept next to the catalog rather than in the client: what is safe to render
// is a property of the item, and deciding it in one place means the phone and
// the desktop cannot disagree about it.
func renderableApp(w protocol.WorkItem) bool {
	return w.Kind == protocol.WorkApp && strings.TrimSpace(w.Content) != ""
}
