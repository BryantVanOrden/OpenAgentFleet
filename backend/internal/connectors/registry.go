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
	key, err := r.keys.Open(ctx, p.APIKeyRef)
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
		key, err := r.keys.Open(ctx, p.APIKeyRef)
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
func (r *Registry) Complete(ctx context.Context, preferred string, req Request) (*Response, error) {
	chain, err := r.Chain(ctx, preferred)
	if err != nil {
		return nil, err
	}
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
