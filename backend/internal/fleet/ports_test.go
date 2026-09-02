package fleet

import (
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The lease ledger is the published_* labels, and only those: user labels are
// caller-supplied, so a value that happens to end in ":6901" must not release
// a port some other live sandbox is bound to.
func TestPublishedPortsIgnoresUserLabels(t *testing.T) {
	got := publishedPorts(map[string]string{
		"published_vnc":    "http://127.0.0.1:15900",
		"published_agentd": "http://127.0.0.1:15901",
		"contact":          "ops room:6901", // user label, must not count
		"note":             "no port here",
	})
	want := map[int]bool{15900: true, 15901: true}
	if len(got) != 2 {
		t.Fatalf("publishedPorts = %v, want exactly the two published_* ports", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("publishedPorts leaked %d from a non-published label", p)
		}
	}
	if publishedPorts(nil) != nil {
		t.Error("nil labels should yield no ports")
	}
}

// A restarted orchestrator has an empty lease map while every live sandbox
// still holds its binding; markLeases is what stops the next provision from
// leasing a taken port and dying with "port is already allocated".
func TestMarkLeasesRehydratesAndLeasePortSkipsThem(t *testing.T) {
	m := &Manager{
		cfg:   &config.Config{PortMin: 15900, PortMax: 15903},
		ports: map[int]bool{},
	}
	m.markLeases([]protocol.Instance{
		{Labels: map[string]string{"published_vnc": "http://127.0.0.1:15900"}},
		{Labels: map[string]string{"published_agentd": "http://127.0.0.1:15901"}},
		{Labels: map[string]string{"note": "not:15902"}}, // user label: stays free
	})

	if p := m.leasePort(); p != 15902 {
		t.Errorf("leasePort() = %d, want 15902 (the first port no sandbox holds)", p)
	}
	if p := m.leasePort(); p != 15903 {
		t.Errorf("leasePort() = %d, want 15903", p)
	}
	// Range exhausted: 0 tells the caller to let the engine pick.
	if p := m.leasePort(); p != 0 {
		t.Errorf("leasePort() = %d, want 0 once the range is exhausted", p)
	}

	// Releasing through the labels frees exactly the published ports again.
	m.releasePortsFor(&protocol.Instance{Labels: map[string]string{
		"published_vnc": "http://127.0.0.1:15900",
	}})
	if p := m.leasePort(); p != 15900 {
		t.Errorf("leasePort() after release = %d, want 15900 back", p)
	}
}
