package connectors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ProviderSource is the subset of the store the registry needs.
type ProviderSource interface {
	ListProviders(ctx context.Context, onlyEnabled bool) ([]protocol.Provider, error)
	Provider(ctx context.Context, id string) (*protocol.Provider, error)
}

// KeyResolver turns a vault ref into a plaintext API key.
type KeyResolver interface {
	Open(ctx context.Context, ref string) (string, error)
}

// Registry builds connectors on demand and runs the fallback chain: providers
// are tried in ascending priority order until one answers. A provider that
// errors is put in a short penalty box so a dead endpoint does not add latency
// to every subsequent step.
type Registry struct {
	src  ProviderSource
	keys KeyResolver
	log  *slog.Logger
	hc   *http.Client

	mu       sync.Mutex
	penalty  map[string]time.Time
	failures map[string]int
}

const penaltyWindow = 60 * time.Second

func NewRegistry(src ProviderSource, keys KeyResolver, log *slog.Logger) *Registry {
	return &Registry{
		src:      src,
		keys:     keys,
		log:      log,
		hc:       defaultClient(),
		penalty:  map[string]time.Time{},
		failures: map[string]int{},
	}
}

// Get builds a single connector by provider id.
func (r *Registry) Get(ctx context.Context, id string) (Connector, error) {
	p, err := r.src.Provider(ctx, id)
	if err != nil {
		return nil, err
	}
	key, err := r.keys.Open(ctx, secretRef(*p))
	if err != nil {
		return nil, err
	}
	return Build(*p, key, r.hc)
}

// Chain returns the ordered, currently-healthy connector list. `preferred` is
// moved to the front when supplied — that is how a task pins itself to one model
// while still keeping the rest of the chain as a safety net.
func (r *Registry) Chain(ctx context.Context, preferred string) ([]Connector, error) {
	providers, err := r.src.ListProviders(ctx, true)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: add one in Settings > AI Engines", ErrNoProvider)
	}

	if preferred != "" {
		for i, p := range providers {
			if p.ID == preferred {
				providers = append([]protocol.Provider{p}, append(providers[:i:i], providers[i+1:]...)...)
				break
			}
		}
	}

	out := make([]Connector, 0, len(providers))
	var penalised []Connector
	for _, p := range providers {
		key, err := r.keys.Open(ctx, secretRef(p))
		if err != nil {
			r.log.Warn("provider key unavailable, skipping", "provider", p.Name, "err", err)
			continue
		}
		c, err := Build(p, key, r.hc)
		if err != nil {
			r.log.Warn("provider misconfigured, skipping", "provider", p.Name, "err", err)
			continue
		}
		if r.inPenaltyBox(p.ID) {
			penalised = append(penalised, c)
			continue
		}
		out = append(out, c)
	}
	// Penalised providers still go on the end: better a slow retry than no model.
	out = append(out, penalised...)
	if len(out) == 0 {
		return nil, ErrNoProvider
	}
	return out, nil
}

// Complete walks the chain until a provider answers. The returned Response
// carries the provider that actually served the request.
// CompleteFor runs a request down one bot's own fallback chain.
//
// The bot's providers are tried in the order given, then everything else as a
// safety net: a bot whose assigned model was deleted or is down should degrade
// to a working model rather than stop being able to think at all. Pass nil to
// get the fleet-wide order.
func (r *Registry) CompleteFor(ctx context.Context, providerIDs []string, req Request) (*Response, error) {
	if len(providerIDs) == 0 {
		return r.Complete(ctx, "", req)
	}
	chain, err := r.ChainFor(ctx, providerIDs)
	if err != nil {
		return nil, err
	}
	return r.complete(ctx, chain, req)
}

func (r *Registry) Complete(ctx context.Context, preferred string, req Request) (*Response, error) {
	chain, err := r.Chain(ctx, preferred)
	if err != nil {
		return nil, err
	}
	return r.complete(ctx, chain, req)
}

func (r *Registry) complete(ctx context.Context, chain []Connector, req Request) (*Response, error) {
	var errs []error
	for _, c := range chain {
		// Vision-blind providers cannot see the desktop; skip them when the
		// request carries a screenshot.
		if !c.Vision() && hasImage(req) {
			continue
		}
		resp, err := c.Complete(ctx, req)
		if err == nil {
			r.recordSuccess(c.ID())
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		r.recordFailure(c.ID())
		r.log.Warn("provider failed, falling through", "provider", c.Name(), "err", err)
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("%w: no vision-capable provider is enabled", ErrNoProvider)
	}
	return nil, errors.Join(errs...)
}

// ChainFor orders the connectors by a bot's own preference.
//
// Assigned providers come first in the order the operator put them in; the
// rest follow so an agent is never left with nothing to think with. A provider
// that no longer exists is simply skipped rather than being an error, because
// deleting a provider must not break every bot that once referenced it.
func (r *Registry) ChainFor(ctx context.Context, providerIDs []string) ([]Connector, error) {
	all, err := r.Chain(ctx, "")
	if err != nil {
		return nil, err
	}

	byID := make(map[string]Connector, len(all))
	for _, c := range all {
		byID[c.ID()] = c
	}

	out := make([]Connector, 0, len(all))
	taken := make(map[string]bool, len(providerIDs))
	for _, id := range providerIDs {
		if c, ok := byID[id]; ok && !taken[id] {
			out = append(out, c)
			taken[id] = true
		}
	}
	for _, c := range all {
		if !taken[c.ID()] {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoProvider
	}
	return out, nil
}

func hasImage(req Request) bool {
	for _, m := range req.Messages {
		if m.Image != "" {
			return true
		}
	}
	return false
}

func (r *Registry) inPenaltyBox(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.penalty[id]
	return ok && time.Now().Before(until)
}

func (r *Registry) recordFailure(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures[id]++
	if r.failures[id] >= 2 {
		r.penalty[id] = time.Now().Add(penaltyWindow)
	}
}

func (r *Registry) recordSuccess(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failures, id)
	delete(r.penalty, id)
}

// Probe does a cheap round-trip so the admin panel can show a live health dot
// next to each configured provider.
func (r *Registry) Probe(ctx context.Context, id string) error {
	c, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = c.Complete(ctx, Request{
		System:   "Reply with the single word: ok",
		Messages: []Message{{Role: RoleUser, Text: "ping"}},
		// A reasoning model would spend this whole budget thinking and return
		// empty content, which reads as an unhealthy provider.
		MaxTokens:       8,
		DisableThinking: true,
	})
	return err
}

// PreferredChain puts an explicit per-request choice at the head of a bot's own
// chain.
//
// A task pinned to one provider still falls back down that bot's list rather
// than to the fleet default, which is what makes "this agent uses these models,
// in this order" hold even for a one-off run.
func PreferredChain(preferred string, botChain []string) []string {
	if preferred == "" {
		return botChain
	}
	out := make([]string, 0, len(botChain)+1)
	out = append(out, preferred)
	for _, id := range botChain {
		if id != preferred {
			out = append(out, id)
		}
	}
	return out
}

// secretRef is where a provider's secret lives in the vault.
//
// A signed-in provider stores a credential blob under its own ref rather than
// in api_key_ref, so switching a provider between a key and a sign-in does not
// overwrite the credential it is not currently using — and switching back does
// not require re-entering it.
func secretRef(p protocol.Provider) string {
	if p.AuthMode == "oauth" && p.OAuthTokenRef != "" {
		return p.OAuthTokenRef
	}
	return p.APIKeyRef
}
