package fleet

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// A sandbox is addressed by its container's network alias, never by the IP it
// happened to have when it was created.
//
// The bug: docker gives a container a new address every restart, and these
// carry restart:unless-stopped. After a host reboot they came back on shuffled
// IPs without the orchestrator running the path that refreshes the stored URL.
// One bot then pointed at a dead address (500 on every desktop call) and
// another pointed at an address a DIFFERENT bot had been given, so its desktop
// showed the wrong agent's screen.
func TestSandboxURLsUseTheStableAliasNotAnIP(t *testing.T) {
	inst := &protocol.Instance{ID: "30bde327-b55f-4152-a0f5-ed67d8cabe52"}

	agentd, vnc, vncView := urlsFor(inst)

	for _, u := range []string{agentd, vnc, vncView} {
		if strings.Contains(u, "172.") || strings.Contains(u, "10.") {
			t.Errorf("%q pins an IP; it must use the container alias", u)
		}
		if !strings.Contains(u, "af-30bde327-b55") {
			t.Errorf("%q does not address the container by its alias", u)
		}
	}

	// The alias must match what the container is actually created with,
	// otherwise DNS resolves nothing.
	if got := sandboxHost(inst); got != "af-"+inst.ID[:12] {
		t.Errorf("sandboxHost = %q, want the create-time container name", got)
	}

	// The three ports stay distinct; addressing them all by one name is only
	// safe if the ports still separate the services.
	if agentd == vnc || vnc == vncView || agentd == vncView {
		t.Error("two sandbox URLs collide")
	}
}
