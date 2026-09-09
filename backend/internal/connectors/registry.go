package connectors

import (
	"strings"
	"net"
	"io"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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

	mu         sync.Mutex
	penalty    map[string]time.Time
	failures   map[string]int
	blindNoted map[string]bool
	// combos resolves a chain entry that names a combination rather than a
	// provider. Optional: with none attached, entries are provider IDs.
	combos ComboSource
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

// ErrEmptyCompletion is a model that answered with nothing.
//
// Almost always a reasoning model that spent its whole budget on the hidden
// thinking pass. It is worth its own error because the cheapest correct
// response is to ask the same model again without that pass, rather than
// falling through to a weaker one — or, as happened on the live fleet,
// failing the whole task.
var ErrEmptyCompletion = errors.New("empty completion")

func (r *Registry) complete(ctx context.Context, chain []Connector, req Request) (*Response, error) {
	// A chain with no eyes at all still gets to work. The turn carries the
	// Set-of-Marks element list and the accessibility tree alongside the
	// screenshot, and a text model can drive a desktop from those; what it
	// cannot do is take a request with an image attached, and the alternative
	// -- "no vision-capable provider is enabled" on every step -- retired the
	// whole fleet the moment an operator chose a text-only model. Drop the
	// pixels and let the words through.
	if hasImage(req) && !anyVision(chain) && len(chain) > 0 {
		req = withoutImages(req)
		r.noteBlind(chain)
	}
	var errs []error
	for _, c := range chain {
		// Vision-blind providers cannot see the desktop; skip them when the
		// request carries a screenshot.
		if !c.Vision() && hasImage(req) {
			continue
		}
		resp, err := r.completeTransient(ctx, c, req)

		// One retry without the thinking pass before giving up on this model.
		// A model that thought itself out of a budget will usually answer if
		// asked plainly, and the alternative is falling through to a model the
		// operator ranked lower for a reason.
		if errors.Is(err, ErrEmptyCompletion) && !req.DisableThinking && ctx.Err() == nil {
			r.log.Warn("empty completion, retrying without the thinking pass",
				"provider", c.Name())
			retry := req
			retry.DisableThinking = true
			if resp2, err2 := c.Complete(ctx, retry); err2 == nil {
				r.recordSuccess(c.ID())
				return resp2, nil
			}
		}

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

// completeTransient is one connector's Complete with a short retry on
// transport failures: a dropped connection, a reset, a refused port.
//
// An agent that is meant to work for days meets these routinely -- the
// gateway restarts to pick up a fix, a Wi-Fi hop blinks -- and a single EOF
// used to fall through the whole chain and fail a sixty-step task at step
// six, with "every model provider failed" as the epitaph for a network hiccup.
// Two quick retries cover the blink; anything longer is still a failure, and
// still falls through to the next provider as before.
func (r *Registry) completeTransient(ctx context.Context, c Connector, req Request) (*Response, error) {
	var resp *Response
	var err error
	for attempt, wait := range transientWaits {
		resp, err = c.Complete(ctx, req)
		if err == nil || !isTransient(err) || ctx.Err() != nil {
			return resp, err
		}
		r.log.Warn("transient transport error from provider, retrying",
			"provider", c.Name(), "attempt", attempt+1, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return c.Complete(ctx, req)
}

// transientWaits is the pause before each retry; the final attempt follows
// the last wait. Short on purpose: a step has its own deadline to respect.
var transientWaits = []time.Duration{2 * time.Second, 6 * time.Second}

// isTransient recognises the transport-level failures worth a second try.
// Model-level failures (a 400, an empty completion, an unknown model) are
// not: repeating them just repeats them.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"eof", "connection reset", "connection refused", "broken pipe",
		"server closed idle connection", "http2: client connection lost", "tls handshake timeout",
		"unexpected eof", "bad gateway", "http 502", "http 503", "http 504"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// ChainSees reports whether any provider in a bot's chain, resolved for a
// role, can see images. Callers that would attach a screenshot ask this first
// so the prompt can say what the model is actually working from.
func (r *Registry) ChainSees(ctx context.Context, entries []string, role string) bool {
	chain, err := r.ChainFor(ctx, r.ResolveChain(ctx, entries, role))
	if err != nil {
		return true // unknown; let complete() decide with the real chain
	}
	return anyVision(chain)
}

func anyVision(chain []Connector) bool {
	for _, c := range chain {
		if c.Vision() {
			return true
		}
	}
	return false
}

func withoutImages(req Request) Request {
	msgs := make([]Message, len(req.Messages))
	copy(msgs, req.Messages)
	for i := range msgs {
		msgs[i].Image = ""
		msgs[i].ImageMime = ""
	}
	req.Messages = msgs
	return req
}

// noteBlind logs the text-only fallback once per chain head, not once per
// step: an agent takes hundreds of steps and the fact does not change.
func (r *Registry) noteBlind(chain []Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.blindNoted == nil {
		r.blindNoted = map[string]bool{}
	}
	id := chain[0].ID()
	if r.blindNoted[id] {
		return
	}
	r.blindNoted[id] = true
	r.log.Info("no vision-capable provider in chain; perceiving through the accessibility tree and element marks",
		"provider", chain[0].Name())
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
