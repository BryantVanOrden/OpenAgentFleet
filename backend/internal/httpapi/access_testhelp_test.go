package httpapi

import (
	"context"
	"net/http"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// asAdmin puts a global-admin Access on a request's context.
//
// Handlers deny when no Access is present, which is deliberate — a route
// reached without the auth middleware must fail closed rather than open. In
// production requireAuth always supplies it; a test calling a handler directly
// has to do the same, and saying so here beats each test rebuilding it.
func asAdmin(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accessKey, protocol.Access{
		UserID:      "test-admin",
		GlobalAdmin: true,
		OrgRoles:    map[string]protocol.OrgRole{},
		Grants:      map[string][]protocol.Permission{},
	}))
}

// asMember puts a non-admin Access on the context, for tests that check a
// permission is actually enforced rather than bypassed.
func asMember(r *http.Request, orgID string, role protocol.OrgRole) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accessKey, protocol.Access{
		UserID:   "test-member",
		OrgRoles: map[string]protocol.OrgRole{orgID: role},
		Grants:   map[string][]protocol.Permission{},
	}))
}
