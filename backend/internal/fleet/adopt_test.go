package fleet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A container can be found again by a name derived from the instance id, which
// is what lets a row that lost its runtime_id be recovered at all. Adoption
// then turns on the instance label, so the parsing of that label is what keeps
// a row from being bound to another bot's desktop.
func TestInspectReadsTheInstanceLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/json") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"Id":"cafebabe0001deadbeef",
			"Name":"/af-fbeee2172beb",
			"State":{"Running":true},
			"Config":{"Labels":{
				"managed-by":"agentfleet",
				"agentfleet.instance":"fbeee217-2beb-42a9-aa00-3c28a4eae9fb"
			}}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewDockerClient(srv.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ci, err := c.InspectContainer(context.Background(), "af-fbeee2172beb")
	if err != nil {
		t.Fatalf("InspectContainer() error = %v", err)
	}
	if got := ci.Config.Labels["agentfleet.instance"]; got != "fbeee217-2beb-42a9-aa00-3c28a4eae9fb" {
		t.Errorf("instance label = %q, want the owning instance id", got)
	}
	if !ci.State.Running || ci.ID == "" {
		t.Errorf("inspect lost the state or id: running=%v id=%q", ci.State.Running, ci.ID)
	}
}

// The name adoption searches for must be the one the container was created
// with, or a stuck row can never be matched to its sandbox.
func TestAdoptionLooksForTheCreateTimeName(t *testing.T) {
	inst := &protocol.Instance{ID: "fbeee217-2beb-42a9-aa00-3c28a4eae9fb"}
	if got, want := sandboxHost(inst), "af-fbeee217-2be"; got != want {
		t.Errorf("sandboxHost = %q, want %q", got, want)
	}
}

// A short or empty id must not be turned into a container name: sandboxHost
// slices the id and would panic, and there is nothing to adopt anyway.
func TestAdoptOrphanRefusesAShortID(t *testing.T) {
	m := &Manager{}
	for _, id := range []string{"", "abc", "0123456789a"} {
		if m.adoptOrphan(context.Background(), &protocol.Instance{ID: id}) {
			t.Errorf("adoptOrphan(%q) = true, want false", id)
		}
	}
}
