package fleet

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }
func b(v bool) *bool         { return &v }
func s(v string) *string     { return &v }

// ------------------------------------------------------------- default set ---

func TestDefaultTiers(t *testing.T) {
	tiers := DefaultTiers("agentfleet/sandbox:latest")
	if len(tiers) != 4 {
		t.Fatalf("got %d tiers, want 4", len(tiers))
	}

	seen := map[protocol.Tier]bool{}
	for _, tr := range tiers {
		if seen[tr.Name] {
			t.Errorf("duplicate tier name %q", tr.Name)
		}
		seen[tr.Name] = true
		if tr.Description == "" {
			t.Errorf("tier %q has no description for the admin panel", tr.Name)
		}
		if tr.VCPU <= 0 || tr.MemoryMB <= 0 || tr.DiskGB <= 0 || tr.ShmMB <= 0 {
			t.Errorf("tier %q has a non-positive resource: %+v", tr.Name, tr)
		}
		if tr.Driver != protocol.DriverDocker {
			t.Errorf("tier %q driver = %q, want docker (qemu is unimplemented)", tr.Name, tr.Driver)
		}
	}
	for _, want := range []protocol.Tier{
		protocol.TierMicro, protocol.TierStandard, protocol.TierPower, protocol.TierDevHeavy,
	} {
		if !seen[want] {
			t.Errorf("missing tier %q", want)
		}
	}

	// Resources must increase monotonically along the ladder.
	for i := 1; i < len(tiers); i++ {
		prev, cur := tiers[i-1], tiers[i]
		if cur.VCPU <= prev.VCPU || cur.MemoryMB <= prev.MemoryMB || cur.DiskGB <= prev.DiskGB {
			t.Errorf("tier %q is not larger than %q", cur.Name, prev.Name)
		}
	}

	// Every tier but developer-heavy uses the base image as given.
	for _, tr := range tiers {
		switch tr.Name {
		case protocol.TierDevHeavy:
			if tr.Image != "agentfleet/sandbox:latest-dev" {
				t.Errorf("developer-heavy image = %q, want the -dev variant", tr.Image)
			}
			if !tr.GPU {
				t.Error("developer-heavy should request a GPU")
			}
		default:
			if tr.Image != "agentfleet/sandbox:latest" {
				t.Errorf("tier %q image = %q, want the base image", tr.Name, tr.Image)
			}
			if tr.GPU {
				t.Errorf("tier %q should not request a GPU by default", tr.Name)
			}
		}
	}
}

// ------------------------------------------------------------- TierByName ---

func TestTierByName(t *testing.T) {
	tiers := DefaultTiers("img")

	cases := []struct {
		name string
		ask  protocol.Tier
		want protocol.Tier
	}{
		{"exact micro", protocol.TierMicro, protocol.TierMicro},
		{"exact standard", protocol.TierStandard, protocol.TierStandard},
		{"exact power-user", protocol.TierPower, protocol.TierPower},
		{"exact developer-heavy", protocol.TierDevHeavy, protocol.TierDevHeavy},
		{"unknown name falls back to standard", protocol.Tier("gigantic"), protocol.TierStandard},
		{"empty name falls back to standard", protocol.Tier(""), protocol.TierStandard},
		{"wrong case falls back to standard", protocol.Tier("Micro"), protocol.TierStandard},
		{"whitespace falls back to standard", protocol.Tier(" micro "), protocol.TierStandard},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TierByName(tiers, tc.ask)
			if got.Name != tc.want {
				t.Errorf("TierByName(%q).Name = %q, want %q", tc.ask, got.Name, tc.want)
			}
		})
	}
}

func TestTierByNameWithoutAStandardTier(t *testing.T) {
	// A custom tier list with no "standard" entry falls back to the first one
	// rather than returning a zero profile that would fail admission.
	tiers := []protocol.TierProfile{
		{Name: protocol.Tier("tiny"), VCPU: 1},
		{Name: protocol.Tier("huge"), VCPU: 32},
	}
	if got := TierByName(tiers, "nope"); got.Name != protocol.Tier("tiny") {
		t.Errorf("TierByName() = %q, want the first tier", got.Name)
	}
}

// ----------------------------------------------------------- ApplyOverride ---

func TestApplyOverride(t *testing.T) {
	base := protocol.TierProfile{
		Name: protocol.TierStandard, Driver: protocol.DriverDocker, Image: "base:1",
		VCPU: 2, MemoryMB: 4096, DiskGB: 30, ShmMB: 1024, GPU: false,
	}

	cases := []struct {
		name string
		in   *protocol.ResourceOverride
		want protocol.TierProfile
	}{
		{
			name: "nil override returns the profile unchanged",
			in:   nil,
			want: base,
		},
		{
			name: "empty override changes nothing",
			in:   &protocol.ResourceOverride{},
			want: base,
		},
		{
			name: "vcpu only",
			in:   &protocol.ResourceOverride{VCPU: f64(6)},
			want: withVCPU(base, 6),
		},
		{
			name: "memory only",
			in:   &protocol.ResourceOverride{MemoryMB: i64(16384)},
			want: withMem(base, 16384),
		},
		{
			name: "every field at once",
			in: &protocol.ResourceOverride{
				VCPU: f64(8), MemoryMB: i64(32768), DiskGB: i64(500),
				GPU: b(true), Image: s("custom:2"),
			},
			want: protocol.TierProfile{
				Name: protocol.TierStandard, Driver: protocol.DriverDocker, Image: "custom:2",
				VCPU: 8, MemoryMB: 32768, DiskGB: 500, ShmMB: 1024, GPU: true,
			},
		},
		{
			name: "zero vcpu is ignored",
			in:   &protocol.ResourceOverride{VCPU: f64(0)},
			want: base,
		},
		{
			name: "negative vcpu is ignored",
			in:   &protocol.ResourceOverride{VCPU: f64(-4)},
			want: base,
		},
		{
			name: "zero memory is ignored",
			in:   &protocol.ResourceOverride{MemoryMB: i64(0)},
			want: base,
		},
		{
			name: "negative memory is ignored",
			in:   &protocol.ResourceOverride{MemoryMB: i64(-1)},
			want: base,
		},
		{
			name: "zero disk is ignored",
			in:   &protocol.ResourceOverride{DiskGB: i64(0)},
			want: base,
		},
		{
			name: "negative disk is ignored",
			in:   &protocol.ResourceOverride{DiskGB: i64(-100)},
			want: base,
		},
		{
			name: "empty image is ignored",
			in:   &protocol.ResourceOverride{Image: s("")},
			want: base,
		},
		{
			name: "gpu can be switched on",
			in:   &protocol.ResourceOverride{GPU: b(true)},
			want: withGPU(base, true),
		},
		{
			name: "gpu can be switched off explicitly",
			in:   &protocol.ResourceOverride{GPU: b(false)},
			want: withGPU(base, false),
		},
		{
			name: "a fractional vcpu is honoured",
			in:   &protocol.ResourceOverride{VCPU: f64(0.5)},
			want: withVCPU(base, 0.5),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyOverride(base, tc.in)
			if got != tc.want {
				t.Errorf("ApplyOverride() = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestApplyOverrideCanTurnGPUOff(t *testing.T) {
	// The developer-heavy tier ships GPU=true; an operator on a GPU-less host
	// must be able to clear it.
	dev := TierByName(DefaultTiers("img"), protocol.TierDevHeavy)
	if !dev.GPU {
		t.Fatal("precondition: developer-heavy should default to GPU=true")
	}
	if got := ApplyOverride(dev, &protocol.ResourceOverride{GPU: b(false)}); got.GPU {
		t.Error("ApplyOverride(GPU=false) left GPU on")
	}
}

func TestApplyOverrideDoesNotMutateTheTier(t *testing.T) {
	tiers := DefaultTiers("img")
	before := TierByName(tiers, protocol.TierStandard)
	_ = ApplyOverride(before, &protocol.ResourceOverride{
		VCPU: f64(64), MemoryMB: i64(999999), Image: s("other:1"), GPU: b(true),
	})
	after := TierByName(tiers, protocol.TierStandard)
	if before != after {
		t.Errorf("the shared tier list was mutated: %+v -> %+v", before, after)
	}
}

func TestSandboxPortsAreDistinct(t *testing.T) {
	ports := []int{PortNoVNC, PortAgentd, PortSignal}
	seen := map[int]bool{}
	for _, p := range ports {
		if p <= 0 || p > 65535 {
			t.Errorf("port %d is out of range", p)
		}
		if seen[p] {
			t.Errorf("duplicate port %d", p)
		}
		seen[p] = true
	}
}

func TestTierNamesAreLowerCase(t *testing.T) {
	for _, tr := range DefaultTiers("img") {
		if string(tr.Name) != strings.ToLower(string(tr.Name)) {
			t.Errorf("tier name %q is not lower case", tr.Name)
		}
	}
}

// ----------------------------------------------------------------- helpers ---

func withVCPU(p protocol.TierProfile, v float64) protocol.TierProfile { p.VCPU = v; return p }
func withMem(p protocol.TierProfile, v int64) protocol.TierProfile    { p.MemoryMB = v; return p }
func withGPU(p protocol.TierProfile, v bool) protocol.TierProfile     { p.GPU = v; return p }
