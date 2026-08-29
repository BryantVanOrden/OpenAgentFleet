package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Resolving what the caller may do, once per request.
//
// The global role still gates whole endpoints — an auditor cannot reach a
// mutation route at all. Within a route, the question is finer: which of these
// bots may this person see, and may they drive this particular one. That is
// what Access answers, and it is resolved once and carried on the context
// because a list endpoint checks every row it is about to return.

type accessKeyType struct{}

var accessKey accessKeyType

// withAccess resolves the caller's org memberships and per-bot grants.
func (s *Server) withAccess(ctx context.Context, c *claims) (context.Context, error) {
	if s.db == nil || c == nil {
		// Tests construct a bare Server. Fail closed on membership but keep an
		// admin working, so a missing database cannot silently open the fleet.
		return context.WithValue(ctx, accessKey, protocol.Access{
			UserID:      userIDOf(c),
			GlobalAdmin: c != nil && c.Role == string(protocol.RoleAdmin),
			OrgRoles:    map[string]protocol.OrgRole{},
			Grants:      map[string][]protocol.Permission{},
		}), nil
	}

	acc, err := s.db.AccessFor(ctx, c.Subject, c.Role == string(protocol.RoleAdmin))
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, accessKey, acc), nil
}

// accessForClaims resolves access for a route that sits outside the auth
// middleware, such as the desktop proxy a browser loads directly.
func (s *Server) accessForClaims(ctx context.Context, c *claims) (protocol.Access, error) {
	if s.db == nil || c == nil {
		return protocol.Access{
			UserID:      userIDOf(c),
			GlobalAdmin: c != nil && c.Role == string(protocol.RoleAdmin),
		}, nil
	}
	return s.db.AccessFor(ctx, c.Subject, c.Role == string(protocol.RoleAdmin))
}

// accessFrom returns the caller's resolved access.
//
// A zero Access denies everything, so a handler that is reached without the
// middleware having run refuses rather than allows.
func accessFrom(ctx context.Context) protocol.Access {
	acc, _ := ctx.Value(accessKey).(protocol.Access)
	return acc
}

func userIDOf(c *claims) string {
	if c == nil {
		return ""
	}
	return c.Subject
}

// visibleInstances filters a list to what the caller may see.
func visibleInstances(acc protocol.Access, all []protocol.Instance) []protocol.Instance {
	out := make([]protocol.Instance, 0, len(all))
	for _, in := range all {
		if acc.Can(protocol.PermView, in.OrgIDs, in.ID) {
			out = append(out, in)
		}
	}
	return out
}

// requirePerm loads a bot and checks one permission against it.
//
// Returns false having already written the response. A caller who may not see
// the bot gets 404 rather than 403: telling someone a bot exists but is not
// theirs is itself a disclosure, and an org's bot names can be sensitive.
func (s *Server) requirePerm(w http.ResponseWriter, r *http.Request, instanceID string, perm protocol.Permission) (*protocol.Instance, bool) {
	inst, err := s.db.Instance(r.Context(), instanceID)
	if err != nil {
		failErr(w, err)
		return nil, false
	}
	acc := accessFrom(r.Context())

	if !acc.Can(protocol.PermView, inst.OrgIDs, inst.ID) {
		fail(w, http.StatusNotFound, "no such instance")
		return nil, false
	}
	if !acc.Can(perm, inst.OrgIDs, inst.ID) {
		fail(w, http.StatusForbidden,
			"you do not have permission to "+string(perm)+" this bot")
		return nil, false
	}
	return inst, true
}

// speaker is the person behind a request.
type speaker struct {
	ID   string
	Name string
}

// speakerOf identifies who is making this request, for attribution on anything
// an agent will later read back.
//
// The display name is the local part of the email rather than the whole
// address: an agent addressing someone by name should say "alex", and the full
// address is an identifier, not a name. The ID is what memories are keyed on,
// so a rename does not orphan them.
func (s *Server) speakerOf(r *http.Request) speaker {
	c := userFrom(r.Context())
	if c == nil {
		return speaker{Name: "Operator"}
	}
	name := c.Email
	if at := strings.IndexByte(name, '@'); at > 0 {
		name = name[:at]
	}
	if name == "" {
		name = "Operator"
	}
	return speaker{ID: c.Subject, Name: name}
}

// requirePermForTask checks a permission against the bot a task belongs to.
//
// A task is addressed by its own id, so the bot behind it has to be looked up
// before the question can even be asked. Without this, task detail and task
// cancellation were reachable across departments by anyone who knew an id.
func (s *Server) requirePermForTask(w http.ResponseWriter, r *http.Request, taskID string, perm protocol.Permission) (*protocol.Task, bool) {
	task, err := s.db.Task(r.Context(), taskID)
	if err != nil {
		failErr(w, err)
		return nil, false
	}
	inst, err := s.db.Instance(r.Context(), task.InstanceID)
	if err != nil {
		failErr(w, err)
		return nil, false
	}
	acc := accessFrom(r.Context())

	if !acc.Can(protocol.PermView, inst.OrgIDs, inst.ID) {
		fail(w, http.StatusNotFound, "no such task")
		return nil, false
	}
	if !acc.Can(perm, inst.OrgIDs, inst.ID) {
		fail(w, http.StatusForbidden,
			"you do not have permission to "+string(perm)+" this bot")
		return nil, false
	}
	return task, true
}
