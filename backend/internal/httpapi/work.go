package httpapi

import (
	"net/http"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The shared work catalog: where agents publish what they have made, for each
// other and for you.

// visibleWork filters the catalog to what the caller may see.
//
// Scoped like secrets: an item filed to a department is visible to that
// department, and an unfiled item is admin-only. Filtered here rather than in
// the client, or every department's work would go over the wire to everyone.
func visibleWork(acc protocol.Access, all []protocol.WorkItem) []protocol.WorkItem {
	out := make([]protocol.WorkItem, 0, len(all))
	for _, w := range all {
		if w.OrgID == "" {
			if acc.GlobalAdmin {
				out = append(out, w)
			}
			continue
		}
		if acc.CanInOrg(protocol.PermRead, w.OrgID) {
			out = append(out, w)
		}
	}
	return out
}

func (s *Server) handleListWork(w http.ResponseWriter, r *http.Request) {
	all, err := s.db.ListWorkItems(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, visibleWork(accessFrom(r.Context()), all))
}

func (s *Server) handleGetWork(w http.ResponseWriter, r *http.Request) {
	item, err := s.db.WorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	// 404 rather than 403 for something you may not see: telling someone an
	// item exists but is not theirs is itself a disclosure.
	if len(visibleWork(accessFrom(r.Context()), []protocol.WorkItem{*item})) == 0 {
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

func (s *Server) handleDeleteWork(w http.ResponseWriter, r *http.Request) {
	item, err := s.db.WorkItem(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	acc := accessFrom(r.Context())
	if len(visibleWork(acc, []protocol.WorkItem{*item})) == 0 {
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
