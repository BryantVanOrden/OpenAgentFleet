package connectors

import (
	"context"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Resolving a chain for one role.
//
// A bot's chain is an ordered list of entries, each either a plain provider or
// a combination. A plain provider answers for every role — it is one model
// doing everything, which is the old behaviour. A combination answers with
// whichever model it assigns to the role being asked for.
//
// This is what makes "these two models, and if neither answers, that one"
// expressible: combinations and providers sit in the same list.

// ComboSource supplies the combinations a chain may refer to.
type ComboSource interface {
	ListModelCombos(ctx context.Context) ([]protocol.ModelCombo, error)
}

// roleFallback is what a combination tries when it does not assign the role
// being asked for.
//
// Falling back rather than skipping means a brain-and-hands pair still answers
// a summarise request instead of silently handing it to the next chain entry —
// the operator picked those two models, so those two should be used. Vision is
// not a fallback for anything: a request carrying a screenshot is filtered
// against Vision() downstream, so a text model reached this way is dropped
// there rather than clicking blind.
var roleFallback = map[string][]string{
	protocol.RoleVision:    {protocol.RoleVision},
	protocol.RoleReasoning: {protocol.RoleReasoning, protocol.RoleChat, protocol.RoleVision},
	protocol.RoleChat:      {protocol.RoleChat, protocol.RoleVision, protocol.RoleReasoning},
	protocol.RoleSummarize: {protocol.RoleSummarize, protocol.RoleReasoning, protocol.RoleChat},
	protocol.RoleRefine:    {protocol.RoleRefine, protocol.RoleReasoning, protocol.RoleChat},
}

// ProviderForRole picks the provider a combination uses for a role, following
// the fallback order. Empty means the combination has nothing for this role.
func ProviderForRole(c protocol.ModelCombo, role string) string {
	order, ok := roleFallback[role]
	if !ok {
		order = []string{role, protocol.RoleReasoning, protocol.RoleVision}
	}
	for _, r := range order {
		if id := c.Roles[r]; id != "" {
			return id
		}
	}
	return ""
}

// ExpandChainForRole turns a bot's chain entries into provider IDs for one
// role, resolving any combination along the way.
//
// Unknown entries are dropped rather than erroring: deleting a provider or a
// combination must not break every bot that once referenced it.
func ExpandChainForRole(entries []string, combos []protocol.ModelCombo, role string) []string {
	byID := make(map[string]protocol.ModelCombo, len(combos))
	for _, c := range combos {
		byID[c.ID] = c
	}

	out := make([]string, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		id := e
		if c, isCombo := byID[e]; isCombo {
			id = ProviderForRole(c, role)
			if id == "" {
				continue
			}
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// CompleteRole runs a request down a bot's chain, resolved for one role.
func (r *Registry) CompleteRole(ctx context.Context, entries []string, role string, req Request) (*Response, error) {
	return r.CompleteFor(ctx, r.ResolveChain(ctx, entries, role), req)
}

// ResolveChain expands a chain for a role, loading combinations if a source is
// attached. With no source the entries are already provider IDs.
func (r *Registry) ResolveChain(ctx context.Context, entries []string, role string) []string {
	if len(entries) == 0 || r.combos == nil {
		return entries
	}
	combos, err := r.combos.ListModelCombos(ctx)
	if err != nil {
		// A combination lookup that fails must not take the fleet down: the
		// entries still name providers, and an unresolved combination is
		// dropped by the expansion below.
		r.log.Warn("model combinations unavailable, using the chain as providers", "err", err)
		return entries
	}
	return ExpandChainForRole(entries, combos, role)
}

// AttachCombos makes combinations resolvable from a chain.
func (r *Registry) AttachCombos(src ComboSource) { r.combos = src }
