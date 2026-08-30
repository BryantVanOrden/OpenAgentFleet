package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ModelDescriptor represents a model returned by dynamic model discovery.
type ModelDescriptor struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Speed       string `json:"speed,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Vision      bool   `json:"vision"`
	Description string `json:"description,omitempty"`
}

// CatalogueFallback reports that a model list came from the built-in catalogue
// rather than from the provider.
//
// Discovery used to swallow every failure and return the curated list with a nil
// error, so an operator who typed a bad key, pointed at a dead endpoint or was
// offline saw a full, confident dropdown and no hint that nothing had been
// contacted. They would then pick a model the provider does not serve and only
// find out several steps into a run. The fallback is still useful — it is better
// than an empty dropdown — but the caller has to be able to tell the difference.
type CatalogueFallback struct{ Reason string }

func (e *CatalogueFallback) Error() string { return e.Reason }

// fellBack is shorthand for returning the curated list with the reason attached.
func fellBack(models []ModelDescriptor, format string, args ...any) ([]ModelDescriptor, error) {
	return models, &CatalogueFallback{Reason: fmt.Sprintf(format, args...)}
}

// readAndClose reads a discovery response and releases its connection, taking
// the result of an http.Do call directly so neither can be forgotten.
//
// It returns a status of zero when the request itself failed. The body used to
// be closed only on the 200 path, which leaks a connection on every other
// answer -- and the commonest answer of all is 401 from a key that has expired,
// on a call the model picker makes every time it is opened.
func readAndClose(resp *http.Response, err error) (int, []byte) {
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// ListDynamicModels discovers and lists models for any configured provider kind.
//
// Every kind queries the provider's real model endpoint. When that cannot be
// reached the curated catalogue is returned together with a *CatalogueFallback,
// so callers can present the list and still say it is not live.
func ListDynamicModels(ctx context.Context, kind protocol.ProviderKind, base, key string) ([]ModelDescriptor, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hc := &http.Client{Timeout: 8 * time.Second}

	switch kind {
	case protocol.ProviderOllama:
		return listOllamaDynamic(ctx, hc, base)

	case protocol.ProviderOpenAI, protocol.ProviderCompatible:
		return listOpenAIDynamic(ctx, hc, base, key, kind)

	case protocol.ProviderAnthropic:
		return listAnthropicDynamic(ctx, hc, base, key)

	case protocol.ProviderGemini:
		return listGeminiDynamic(ctx, hc, base, key)

	case protocol.ProviderAntigravity:
		return listAntigravityDynamic(ctx, hc, base, key)

	default:
		return []ModelDescriptor{}, fmt.Errorf("unknown provider kind: %s", kind)
	}
}

func listOllamaDynamic(ctx context.Context, hc *http.Client, base string) ([]ModelDescriptor, error) {
	if base == "" {
		base = "http://localhost:11434"
	}
	base = strings.TrimSuffix(base, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return fellBack(fallbackOllama(), "could not build a request for %s", base)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fellBack(fallbackOllama(), "ollama unreachable at %s: %v", base, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return fellBack(fallbackOllama(), "ollama at %s answered %d", base, resp.StatusCode)
	}
	defer resp.Body.Close()

	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Models) == 0 {
		return fellBack(fallbackOllama(), "could not read the model list from %s", base)
	}

	var results []ModelDescriptor
	for _, m := range out.Models {
		isVision := strings.Contains(m.Name, "vl") || strings.Contains(m.Name, "vision") || strings.Contains(m.Name, "llava")
		results = append(results, ModelDescriptor{
			ID:          m.Name,
			Name:        m.Name,
			Speed:       "Local GPU/CPU",
			Thinking:    "Standard",
			Vision:      isVision,
			Description: fmt.Sprintf("Locally hosted Ollama model (%s)", m.Name),
		})
	}
	return results, nil
}

func fallbackOllama() []ModelDescriptor {
	return []ModelDescriptor{
		{ID: "qwen3.5:4b", Name: "Qwen 3.5 4B", Speed: "Fast", Thinking: "Medium", Vision: true, Description: "Default vision model — small enough to answer promptly on one consumer GPU"},
		{ID: "qwen2.5vl:7b", Name: "Qwen 2.5 VL 7B", Speed: "Fast", Thinking: "Medium", Vision: true, Description: "Multimodal vision model for desktop agent navigation"},
		{ID: "qwen2.5vl:72b", Name: "Qwen 2.5 VL 72B", Speed: "Heavy", Thinking: "High", Vision: true, Description: "Heavyweight local vision model with high grounding accuracy"},
		{ID: "llama3.2-vision:11b", Name: "Llama 3.2 Vision 11B", Speed: "Fast", Thinking: "Medium", Vision: true, Description: "Meta multimodal vision model"},
		{ID: "deepseek-r1:14b", Name: "DeepSeek R1 14B", Speed: "Balanced", Thinking: "High Reasoning", Vision: false, Description: "Local reasoning & logic model"},
	}
}

func listOpenAIDynamic(ctx context.Context, hc *http.Client, base, key string, kind protocol.ProviderKind) ([]ModelDescriptor, error) {
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	base = strings.TrimSuffix(base, "/")
	endpoint := base + "/models"
	if !strings.HasSuffix(base, "/v1") && kind == protocol.ProviderOpenAI {
		endpoint = base + "/v1/models"
	}

	if key != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+key)
			if status, raw := readAndClose(hc.Do(req)); status == http.StatusOK {
				var out struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if err := json.Unmarshal(raw, &out); err == nil && len(out.Data) > 0 {
					var list []ModelDescriptor
					for _, d := range out.Data {
						id := d.ID
						// Filter noise like tts, audio, moderation, dall-e, text-embedding
						if strings.Contains(id, "tts") || strings.Contains(id, "embedding") || strings.Contains(id, "whisper") || strings.Contains(id, "moderation") || strings.Contains(id, "babbage") || strings.Contains(id, "davinci") {
							continue
						}
						isVision := strings.Contains(id, "4o") || strings.Contains(id, "vision") || strings.Contains(id, "vl") || strings.Contains(id, "o1") || strings.Contains(id, "o3")
						list = append(list, ModelDescriptor{
							ID:          id,
							Name:        id,
							Speed:       "Cloud",
							Thinking:    "Auto",
							Vision:      isVision,
							Description: fmt.Sprintf("Live endpoint model (%s)", id),
						})
					}
					if len(list) > 0 {
						sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
						return list, nil
					}
				}
			}
		}
	}

	// Dynamic curated fallback catalog
	return []ModelDescriptor{
		{ID: "gpt-4o", Name: "GPT-4o (Omni)", Speed: "Fast", Thinking: "High", Vision: true, Description: "Flagship multimodal vision model with high speed and precision."},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Speed: "Ultra Fast", Thinking: "Medium", Vision: true, Description: "Fast, cost-efficient multimodal sub-agent worker."},
		{ID: "o3-mini", Name: "o3-mini (High Reasoning)", Speed: "Balanced", Thinking: "High Reasoning", Vision: true, Description: "Specialized STEM, reasoning, and coding model."},
		{ID: "o1", Name: "o1 (Full Reasoning)", Speed: "Deep Reasoning", Thinking: "Maximum Reasoning", Vision: true, Description: "Frontier chain-of-thought reasoning model for difficult engineering tasks."},
		{ID: "chatgpt-4o-latest", Name: "ChatGPT-4o Latest", Speed: "Fast", Thinking: "High", Vision: true, Description: "Dynamic continuously updated GPT-4o checkpoint."},
	}, &CatalogueFallback{Reason: reasonFor("openai", key, base)}
}

func listAnthropicDynamic(ctx context.Context, hc *http.Client, base, key string) ([]ModelDescriptor, error) {
	if base == "" {
		base = "https://api.anthropic.com"
	}
	base = strings.TrimSuffix(base, "/")
	if key != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
		if err == nil {
			req.Header.Set("x-api-key", key)
			req.Header.Set("anthropic-version", "2023-06-01")
			if status, raw := readAndClose(hc.Do(req)); status == http.StatusOK {
				var out struct {
					Data []struct {
						ID          string `json:"id"`
						DisplayName string `json:"display_name"`
					} `json:"data"`
				}
				if err := json.Unmarshal(raw, &out); err == nil && len(out.Data) > 0 {
					var list []ModelDescriptor
					for _, d := range out.Data {
						name := d.DisplayName
						if name == "" {
							name = d.ID
						}
						list = append(list, ModelDescriptor{
							ID:          d.ID,
							Name:        name,
							Speed:       "Cloud",
							Thinking:    "Extended Thinking Available",
							Vision:      true,
							Description: fmt.Sprintf("Anthropic model %s", name),
						})
					}
					return list, nil
				}
			}
		}
	}

	return []ModelDescriptor{
		{ID: "claude-3-7-sonnet-20250219", Name: "Claude 3.7 Sonnet (Hybrid Thinking)", Speed: "Fast / Deep", Thinking: "Adjustable High Thinking", Vision: true, Description: "Frontier hybrid reasoning model with native computer use & visual grounding."},
		{ID: "claude-3-5-sonnet-20241022", Name: "Claude 3.5 Sonnet v2", Speed: "Fast", Thinking: "High", Vision: true, Description: "Industry standard for computer use, UI navigation, and coding."},
		{ID: "claude-3-5-haiku-20241022", Name: "Claude 3.5 Haiku", Speed: "Ultra Fast", Thinking: "Medium", Vision: true, Description: "High throughput, low latency sub-agent task runner."},
		{ID: "claude-3-opus-20240229", Name: "Claude 3 Opus", Speed: "Deep Reasoning", Thinking: "Deep Logic", Vision: true, Description: "Complex long-context reasoning and analysis."},
	}, &CatalogueFallback{Reason: reasonFor("anthropic", key, base)}
}

func listGeminiDynamic(ctx context.Context, hc *http.Client, base, key string) ([]ModelDescriptor, error) {
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	base = strings.TrimSuffix(base, "/")
	if key != "" {
		// Header, not the query string. A transport failure here is wrapped in
		// *url.Error, which stringifies the whole URL into an error that the
		// handler returns to the browser — a key in the URL is a key in the
		// response body, and in whatever logs it on the way.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1beta/models", nil)
		if err == nil {
			req.Header.Set("x-goog-api-key", key)
			if status, raw := readAndClose(hc.Do(req)); status == http.StatusOK {
				var out struct {
					Models []struct {
						Name                       string   `json:"name"`
						DisplayName                string   `json:"displayName"`
						Description                string   `json:"description"`
						SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
					} `json:"models"`
				}
				if err := json.Unmarshal(raw, &out); err == nil && len(out.Models) > 0 {
					var list []ModelDescriptor
					for _, m := range out.Models {
						id := strings.TrimPrefix(m.Name, "models/")

						// Exclude a model only when it explicitly says it cannot
						// generate content. Treating an ABSENT list as "cannot"
						// silently emptied the results for every Gemini-compatible
						// gateway that does not return the field — Antigravity
						// among them — which then looked like a healthy fallback
						// rather than the discovery failure it was.
						if len(m.SupportedGenerationMethods) > 0 {
							supportsGenerate := false
							for _, method := range m.SupportedGenerationMethods {
								if method == "generateContent" {
									supportsGenerate = true
									break
								}
							}
							if !supportsGenerate {
								continue
							}
						}
						disp := m.DisplayName
						if disp == "" {
							disp = id
						}
						list = append(list, ModelDescriptor{
							ID:          id,
							Name:        disp,
							Speed:       "Cloud Fast",
							Thinking:    "Supported",
							Vision:      true,
							Description: m.Description,
						})
					}
					if len(list) > 0 {
						return list, nil
					}
				}
			}
		}
	}

	return []ModelDescriptor{
		{ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", Speed: "Fast", Thinking: "Medium", Vision: true, Description: "Next-gen multimodal model with native high speed and tool use."},
		{ID: "gemini-2.0-flash-thinking-exp-01-21", Name: "Gemini 2.0 Flash (Thinking)", Speed: "Fast", Thinking: "High Reasoning", Vision: true, Description: "Reasoning-specialized Flash model showing step-by-step thinking."},
		{ID: "gemini-2.0-pro-exp-02-05", Name: "Gemini 2.0 Pro", Speed: "Deep Reasoning", Thinking: "High", Vision: true, Description: "Frontier multimodal Gemini model for complex workflows."},
		{ID: "gemini-1.5-pro", Name: "Gemini 1.5 Pro", Speed: "Balanced", Thinking: "High", Vision: true, Description: "2M token context window with comprehensive visual understanding."},
		{ID: "gemini-1.5-flash", Name: "Gemini 1.5 Flash", Speed: "Ultra Fast", Thinking: "Medium", Vision: true, Description: "Fast, cost-effective multimodal vision worker."},
	}, &CatalogueFallback{Reason: reasonFor("gemini", key, base)}
}

// listAntigravityDynamic asks the gateway what it actually serves.
//
// This used to return the hardcoded catalogue unconditionally and report success,
// which made "dynamic discovery" a label rather than a behaviour: the dropdown
// looked identical whether the gateway was reachable, misconfigured, or serving a
// completely different set of models.
//
// Antigravity fronts a Gemini-compatible endpoint, so the same `/v1beta/models`
// listing works. When it answers we use what it says; when it does not, the
// curated catalogue is still returned — with the reason attached, so the console
// can label it instead of implying it is live.
func listAntigravityDynamic(ctx context.Context, hc *http.Client, base, key string) ([]ModelDescriptor, error) {
	curated := antigravityCatalogue(ctx, base, key)

	if key == "" {
		return curated, &CatalogueFallback{
			Reason: "no Antigravity key configured, showing the built-in catalogue",
		}
	}

	live, err := listGeminiDynamic(ctx, hc, base, key)
	var fallback *CatalogueFallback
	if err != nil && !errors.As(err, &fallback) {
		return curated, &CatalogueFallback{
			Reason: fmt.Sprintf("Antigravity discovery failed: %v", err),
		}
	}
	if fallback != nil {
		// Gemini discovery itself fell back, so its list is the generic Gemini
		// catalogue rather than anything this gateway told us. Prefer ours.
		return curated, &CatalogueFallback{Reason: fallback.Reason}
	}

	// Merge: keep everything the gateway reports, and enrich any entry we have
	// curated metadata for, since the wire format carries no speed or
	// thinking-depth hints and those are what the picker is actually for.
	byID := make(map[string]ModelDescriptor, len(curated))
	for _, m := range curated {
		byID[m.ID] = m
	}
	out := make([]ModelDescriptor, 0, len(live))
	for _, m := range live {
		if enriched, ok := byID[m.ID]; ok {
			enriched.Name = firstNonEmpty(m.Name, enriched.Name)
			out = append(out, enriched)
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// antigravityCatalogue is the curated list, in ModelDescriptor form.
func antigravityCatalogue(ctx context.Context, base, key string) []ModelDescriptor {
	models, _ := ListAntigravityModels(ctx, base, key)
	list := make([]ModelDescriptor, 0, len(models))
	for _, m := range models {
		list = append(list, ModelDescriptor{
			ID:          m.ID,
			Name:        m.Name,
			Speed:       m.Speed,
			Thinking:    m.ThinkingLevel,
			Vision:      m.Vision,
			Description: m.Description,
		})
	}
	return list
}

// reasonFor explains why a curated catalogue was returned. The commonest cause
// by far is simply that no API key has been entered yet, which is not an error
// and should not read like one.
func reasonFor(provider, key, base string) string {
	if key == "" {
		return fmt.Sprintf("no %s API key configured, showing the built-in catalogue", provider)
	}
	return fmt.Sprintf("could not list models from %s at %s, showing the built-in catalogue",
		provider, base)
}
