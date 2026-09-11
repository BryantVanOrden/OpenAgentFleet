package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// First-run setup, done for the operator rather than by them.
//
// A fresh deployment has no model engine, no bots and no work, and every
// screen in both clients is a blank list until all three exist. Nothing in the
// product used to say so; the operator was left to find the engines page,
// know their Ollama host's address, type the model name exactly, guess whether
// it could see, then find the fleet page and pick an archetype. This is the
// one place that knows what is missing and can do the next step itself.

type setupStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	// Hint is what the operator would have to do; Action is the fleet command
	// that does it for them, when there is one.
	Hint   string `json:"hint,omitempty"`
	Action string `json:"action,omitempty"`
}

type setupStatus struct {
	Providers   int         `json:"providers"`
	Bots        int         `json:"bots"`
	RunningBots int         `json:"running_bots"`
	Tasks       int         `json:"tasks"`
	Steps       []setupStep `json:"steps"`
	// Next is the first step not done: "model", "bot", "goal" or "ready".
	Next string `json:"next"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.setupStatus(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) setupStatus(ctx context.Context) (setupStatus, error) {
	providers, err := s.db.ListProviders(ctx, true)
	if err != nil {
		return setupStatus{}, err
	}
	instances, err := s.db.ListInstances(ctx)
	if err != nil {
		return setupStatus{}, err
	}
	tasks, err := s.db.ListTasks(ctx, "", 1)
	if err != nil {
		return setupStatus{}, err
	}
	running := 0
	for _, in := range instances {
		if in.State == protocol.InstanceRunning {
			running++
		}
	}
	return setupPlan(len(providers), len(instances), running, len(tasks)), nil
}

// setupPlan is the pure part: what is done and what comes next.
func setupPlan(providers, bots, running, tasks int) setupStatus {
	steps := []setupStep{
		{ID: "model", Title: "Connect a model", Done: providers > 0,
			Hint:   "Oaf can find Ollama, LM Studio or llama.cpp running on this machine, or you can paste an address.",
			Action: "/setup"},
		{ID: "bot", Title: "Create your first bot", Done: bots > 0,
			Hint:   "A sandboxed desktop with an archetype. Takes about a minute.",
			Action: "/new fullstack_dev"},
		{ID: "goal", Title: "Give it something to do", Done: tasks > 0,
			Hint: "Type what you want done. The fleet divides the work and keeps going until it is finished."},
	}
	next := "ready"
	for _, st := range steps {
		if !st.Done {
			next = st.ID
			break
		}
	}
	return setupStatus{Providers: providers, Bots: bots, RunningBots: running, Tasks: tasks, Steps: steps, Next: next}
}

// ------------------------------------------------------------ autodetect ---

type autodetectRequest struct {
	// BaseURL is an engine the operator names; empty scans the usual places.
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	// Apply registers the best model found as a provider. Off, this is a
	// dry run that only reports what is reachable.
	Apply bool `json:"apply"`
}

type foundEngine struct {
	BaseURL string   `json:"base_url"`
	Models  []string `json:"models"`
}

type autodetectResult struct {
	Found    []foundEngine      `json:"found"`
	Provider *protocol.Provider `json:"provider,omitempty"`
	// Vision is whether the registered model saw the probe image.
	Vision bool   `json:"vision"`
	Note   string `json:"note,omitempty"`
}

func (s *Server) handleSetupAutodetect(w http.ResponseWriter, r *http.Request) {
	var req autodetectRequest
	if r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	res, err := s.autodetect(r.Context(), req)
	if err != nil {
		failErr(w, err)
		return
	}
	// The chat verb records its own result as a system note (every mutating
	// command does); only the direct API call has to say what it did.
	if res.Provider != nil {
		s.systemNote(r.Context(), res.Note)
	}
	writeJSON(w, http.StatusOK, res)
}

// candidateBases is where a local engine is likely to be, seen from inside
// the orchestrator's container. The configured Ollama address leads.
func (s *Server) candidateBases() []string {
	gw := s.cfg.HostGateway
	if gw == "" {
		gw = "host.docker.internal"
	}
	seen := map[string]bool{}
	var out []string
	add := func(b string) {
		b = strings.TrimRight(strings.TrimSpace(b), "/")
		if b == "" || seen[b] {
			return
		}
		seen[b] = true
		out = append(out, b)
	}
	add(s.defaultOllamaBase())
	add("http://" + gw + ":11434") // Ollama
	add("http://" + gw + ":1234")  // LM Studio
	add("http://" + gw + ":8080")  // llama.cpp server
	add("http://" + gw + ":8000")  // vLLM
	return out
}

// v1Of is the OpenAI-compatible root of an engine address. Registered this
// way rather than as native Ollama on purpose: it is the route every local
// server shares, and on at least one gateway it is the only route that
// carries images through.
func v1Of(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func (s *Server) autodetect(ctx context.Context, req autodetectRequest) (autodetectResult, error) {
	bases := s.candidateBases()
	if strings.TrimSpace(req.BaseURL) != "" {
		bases = []string{strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")}
	}

	res := autodetectResult{Found: []foundEngine{}}
	for _, base := range bases {
		models, err := connectors.ListDynamicModels(ctx, protocol.ProviderCompatible, v1Of(base), req.APIKey)
		// A curated fallback list is not a discovery; only a live answer counts.
		if err != nil || len(models) == 0 {
			continue
		}
		names := make([]string, 0, len(models))
		for _, m := range models {
			names = append(names, m.ID)
		}
		res.Found = append(res.Found, foundEngine{BaseURL: base, Models: names})
	}
	if len(res.Found) == 0 {
		res.Note = "No model engine answered. Start Ollama, LM Studio or llama.cpp, or paste its address."
		return res, nil
	}
	if !req.Apply {
		return res, nil
	}

	engine := res.Found[0]
	candidates := rankModels(engine.Models)
	// Measure vision on the likeliest few; the first that can see wins, and
	// a fleet with no sighted model still gets its best text model.
	chosen, sees := "", false
	hc := &http.Client{Timeout: 2 * time.Minute}
	for i, name := range candidates {
		if i >= 3 {
			break
		}
		p := draftProvider(engine.BaseURL, name)
		c, err := connectors.Build(p, req.APIKey, hc)
		if err != nil {
			continue
		}
		ok, err := connectors.DetectVision(ctx, c)
		if err != nil {
			s.log.Info("setup: model did not answer the vision probe", "model", name, "err", err)
			continue
		}
		if chosen == "" {
			chosen = name
		}
		if ok {
			chosen, sees = name, true
			break
		}
	}
	if chosen == "" {
		chosen = candidates[0]
	}

	p := draftProvider(engine.BaseURL, chosen)
	p.Vision = sees
	if req.APIKey != "" {
		ref := "provider/" + slug(p.Name) + "/api_key"
		if err := s.vault.Put(ctx, ref, req.APIKey, "API key for "+p.Name); err != nil {
			return res, err
		}
		p.APIKeyRef = ref
	}
	if err := s.db.UpsertProvider(ctx, &p); err != nil {
		return res, err
	}
	if saved, err := s.db.ListProviders(ctx, false); err == nil {
		for i := range saved {
			if saved[i].Model == p.Model && saved[i].BaseURL == p.BaseURL {
				p = saved[i]
				break
			}
		}
	}
	res.Provider = &p
	res.Vision = sees
	eyes := "sees screenshots"
	if !sees {
		eyes = "text only, so it will drive from the accessibility tree"
	}
	res.Note = fmt.Sprintf("Connected **%s** at %s (%s).", chosen, hostOf(engine.BaseURL), eyes)
	return res, nil
}

func draftProvider(base, model string) protocol.Provider {
	return protocol.Provider{
		Name:      "Local engine (" + hostOf(base) + ")",
		Kind:      protocol.ProviderCompatible,
		BaseURL:   v1Of(base),
		Model:     model,
		Priority:  1,
		MaxTokens: 4096,
		Enabled:   true,
	}
}

func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	return u.Host
}

// rankModels orders a model list by how likely each is to drive a desktop:
// names that advertise vision first, then general chat models, then the
// rest. Embedding and OCR models go last: they answer the list call but
// cannot hold a conversation.
func rankModels(names []string) []string {
	score := func(n string) int {
		l := strings.ToLower(n)
		switch {
		case strings.Contains(l, "embed"), strings.Contains(l, "ocr"), strings.Contains(l, "whisper"), strings.Contains(l, "rerank"):
			return 0
		case strings.Contains(l, "vision"), strings.Contains(l, "-vl"), strings.Contains(l, "vl:"), strings.Contains(l, "llava"), strings.Contains(l, "4o"), strings.Contains(l, "gemma"):
			return 3
		case strings.Contains(l, "qwen"), strings.Contains(l, "llama"), strings.Contains(l, "mistral"), strings.Contains(l, "gpt"), strings.Contains(l, "claude"), strings.Contains(l, "flash"):
			return 2
		}
		return 1
	}
	out := append([]string(nil), names...)
	sort.SliceStable(out, func(i, j int) bool { return score(out[i]) > score(out[j]) })
	return out
}

// cmdSetup is the chat verb: find an engine and connect it, in one line.
func (s *Server) cmdSetup(r *http.Request, args string) commandResult {
	ctx := r.Context()
	if !accessFrom(ctx).CanInOrg(protocol.PermCreate, "") {
		return commandResult{Command: "setup", OK: false, Title: "Not allowed", Body: "Only an administrator can connect a model engine."}
	}
	req := autodetectRequest{Apply: true}
	if f := strings.Fields(args); len(f) > 0 {
		req.BaseURL = f[0]
		if len(f) > 1 {
			req.APIKey = f[1]
		}
	}
	res, err := s.autodetect(ctx, req)
	if err != nil {
		return commandResult{Command: "setup", OK: false, Title: "Setup failed", Body: err.Error()}
	}
	if res.Provider == nil {
		return commandResult{Command: "setup", OK: false, Title: "No engine found",
			Body: res.Note + "\n\n`/setup http://host:port [api-key]` connects one by address."}
	}
	body := res.Note + "\n\nModels seen at " + hostOf(res.Found[0].BaseURL) + ": `" + strings.Join(res.Found[0].Models, "`, `") + "`"
	return commandResult{Command: "setup", OK: true, Title: "Model connected", Body: body}
}
