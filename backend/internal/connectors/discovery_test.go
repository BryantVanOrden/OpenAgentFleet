package connectors

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

const testKey = "sk-discovery-canary-1234"

// every provider kind that discovery claims to support
var discoverableKinds = []protocol.ProviderKind{
	protocol.ProviderOllama,
	protocol.ProviderOpenAI,
	protocol.ProviderCompatible,
	protocol.ProviderAnthropic,
	protocol.ProviderGemini,
	protocol.ProviderAntigravity,
}

// recordingProvider stands in for a model endpoint and remembers how it was
// called, so a test can assert on the request rather than only the response.
type recordingProvider struct {
	server *httptest.Server
	paths  []string
	querys []string
	auth   http.Header
}

func newRecordingProvider(t *testing.T, body string) *recordingProvider {
	t.Helper()
	rp := &recordingProvider{}
	rp.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rp.paths = append(rp.paths, r.URL.Path)
		rp.querys = append(rp.querys, r.URL.RawQuery)
		rp.auth = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(rp.server.Close)
	return rp
}

// A model list is only useful if the caller can tell whether it came from the
// provider or from the built-in catalogue. Discovery used to swallow every
// failure and report success, so a bad key produced a confident dropdown of
// models the provider might not serve.
func TestDiscoveryReportsWhenItFellBackToTheCatalogue(t *testing.T) {
	for _, kind := range discoverableKinds {
		t.Run(string(kind), func(t *testing.T) {
			// Unreachable endpoint, no key: discovery cannot possibly be live.
			models, err := ListDynamicModels(context.Background(), kind,
				"http://127.0.0.1:1", "")

			if len(models) == 0 {
				t.Errorf("returned no models at all; an empty dropdown is worse than a catalogue")
			}
			var fallback *CatalogueFallback
			if !errors.As(err, &fallback) {
				t.Fatalf("err = %v, want a *CatalogueFallback so the caller can label the list", err)
			}
			if strings.TrimSpace(fallback.Reason) == "" {
				t.Error("fell back without saying why")
			}
		})
	}
}

func TestDiscoveryIsLiveWhenTheProviderAnswers(t *testing.T) {
	cases := []struct {
		kind protocol.ProviderKind
		body string
		want string
	}{
		{protocol.ProviderOllama, `{"models":[{"name":"qwen2.5vl:7b"}]}`, "qwen2.5vl:7b"},
		{protocol.ProviderOpenAI, `{"data":[{"id":"gpt-4o"}]}`, "gpt-4o"},
		{protocol.ProviderCompatible, `{"data":[{"id":"Qwen/Qwen2.5-VL-7B"}]}`, "Qwen/Qwen2.5-VL-7B"},
		{protocol.ProviderAnthropic, `{"data":[{"id":"claude-sonnet-4-5","display_name":"Sonnet"}]}`, "claude-sonnet-4-5"},
		{protocol.ProviderGemini, `{"models":[{"name":"models/gemini-2.0-flash","displayName":"Flash"}]}`, "gemini-2.0-flash"},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			rp := newRecordingProvider(t, tc.body)

			models, err := ListDynamicModels(context.Background(), tc.kind, rp.server.URL, testKey)
			if err != nil {
				t.Fatalf("discovery reported a fallback against a healthy endpoint: %v", err)
			}

			var found bool
			for _, m := range models {
				if m.ID == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("model %q not in the discovered list %v — the live response was ignored",
					tc.want, models)
			}
		})
	}
}

// The same rule the Complete path learned the hard way: a credential in a URL
// ends up in *url.Error, which is stringified into API responses and logs.
func TestDiscoveryNeverPutsTheKeyInAURL(t *testing.T) {
	for _, kind := range discoverableKinds {
		t.Run(string(kind), func(t *testing.T) {
			rp := newRecordingProvider(t, `{"data":[],"models":[]}`)

			_, _ = ListDynamicModels(context.Background(), kind, rp.server.URL, testKey)

			for i, q := range rp.querys {
				if strings.Contains(q, testKey) {
					t.Errorf("request %d put the api key in the query string: %q", i, q)
				}
			}
			for i, p := range rp.paths {
				if strings.Contains(p, testKey) {
					t.Errorf("request %d put the api key in the path: %q", i, p)
				}
			}
		})
	}
}

// An unknown kind is a caller mistake, not a discovery failure, and must not be
// dressed up as a usable catalogue.
func TestDiscoveryRejectsAnUnknownKind(t *testing.T) {
	models, err := ListDynamicModels(context.Background(), protocol.ProviderKind("wat"), "", "")
	if err == nil {
		t.Fatal("an unknown provider kind was accepted")
	}
	var fallback *CatalogueFallback
	if errors.As(err, &fallback) {
		t.Error("an unknown kind reported as a catalogue fallback; it is a bad request")
	}
	if len(models) != 0 {
		t.Errorf("returned %d models for an unknown kind", len(models))
	}
}

// countingServer reports how many separate TCP connections it accepted, which
// is how a leaked response body shows up from the outside: an unclosed body is
// never returned to the pool, so every call has to dial again.
type countingServer struct {
	server *httptest.Server
	mu     sync.Mutex
	conns  int
}

func newCountingServer(t *testing.T, status int, body string) *countingServer {
	t.Helper()
	cs := &countingServer{}
	cs.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	cs.server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			cs.mu.Lock()
			cs.conns++
			cs.mu.Unlock()
		}
	}
	cs.server.Start()
	t.Cleanup(cs.server.Close)
	return cs
}

func (cs *countingServer) count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.conns
}

// A provider that answers anything other than 200 is the ordinary case — an
// expired key answers 401 — and discovery runs whenever the model picker is
// opened. The body was closed only on the success path, so each of those calls
// stranded a connection that the pool could never hand back.
func TestDiscoveryReleasesTheConnectionWhenAProviderRejectsTheKey(t *testing.T) {
	const calls = 5

	for _, tc := range []struct {
		name string
		list func(ctx context.Context, hc *http.Client, base string) ([]ModelDescriptor, error)
	}{
		{"openai", func(ctx context.Context, hc *http.Client, base string) ([]ModelDescriptor, error) {
			return listOpenAIDynamic(ctx, hc, base, testKey, protocol.ProviderOpenAI)
		}},
		{"anthropic", func(ctx context.Context, hc *http.Client, base string) ([]ModelDescriptor, error) {
			return listAnthropicDynamic(ctx, hc, base, testKey)
		}},
		{"gemini", func(ctx context.Context, hc *http.Client, base string) ([]ModelDescriptor, error) {
			return listGeminiDynamic(ctx, hc, base, testKey)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCountingServer(t, http.StatusUnauthorized, `{"error":"invalid api key"}`)
			hc := &http.Client{Transport: &http.Transport{}, Timeout: 5 * time.Second}
			defer hc.CloseIdleConnections()

			for i := 0; i < calls; i++ {
				models, err := tc.list(context.Background(), hc, srv.server.URL)
				var fallback *CatalogueFallback
				if !errors.As(err, &fallback) {
					t.Fatalf("a rejected key should report a catalogue fallback, got %v", err)
				}
				if len(models) == 0 {
					t.Fatal("the curated catalogue should still be returned")
				}
			}

			if got := srv.count(); got != 1 {
				t.Fatalf("%d connections opened for %d calls; a rejected response leaves its "+
					"body unclosed so the connection is never reused", got, calls)
			}
		})
	}
}
