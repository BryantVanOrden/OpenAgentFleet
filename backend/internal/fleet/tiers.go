package fleet

import (
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Ports exposed by every sandbox image.
const (
	PortNoVNC  = 6901 // websockified HTML5 desktop
	PortAgentd = 7900 // observe/act/record control plane
	PortSignal = 8090 // WebRTC signalling (selkies), when built in
)

// DefaultTiers are the shipped hardware profiles. They are advisory: the admin
// panel can override any field per instance, and Manager clamps the result to
// what the host can actually admit.
func DefaultTiers(image string) []protocol.TierProfile {
	return []protocol.TierProfile{
		{
			Name:        protocol.TierMicro,
			Description: "Light scraping, CLI jobs, file sorting.",
			Driver:      protocol.DriverDocker,
			Image:       image,
			VCPU:        1,
			MemoryMB:    2048,
			DiskGB:      15,
			ShmMB:       256,
		},
		{
			Name:        protocol.TierStandard,
			Description: "Browser automation, web testing, form entry.",
			Driver:      protocol.DriverDocker,
			Image:       image,
			VCPU:        2,
			MemoryMB:    4096,
			DiskGB:      30,
			ShmMB:       1024,
		},
		{
			Name:        protocol.TierPower,
			Description: "Multi-app desktop workflows, office suites.",
			Driver:      protocol.DriverDocker,
			Image:       image,
			VCPU:        4,
			MemoryMB:    8192,
			DiskGB:      60,
			ShmMB:       2048,
		},
		{
			Name:        protocol.TierDevHeavy,
			Description: "Game engine builds, C++/Rust compiles, GPU workloads.",
			Driver:      protocol.DriverDocker, // switch to qemu for full VM isolation
			Image:       image + "-dev",
			VCPU:        8,
			MemoryMB:    16384,
			DiskGB:      150,
			GPU:         true,
			ShmMB:       4096,
		},
	}
}

// TierByName resolves a tier, falling back to standard.
func TierByName(tiers []protocol.TierProfile, name protocol.Tier) protocol.TierProfile {
	for _, t := range tiers {
		if t.Name == name {
			return t
		}
	}
	for _, t := range tiers {
		if t.Name == protocol.TierStandard {
			return t
		}
	}
	return tiers[0]
}

// ApplyOverride layers per-instance resource tweaks over a tier profile.
func ApplyOverride(p protocol.TierProfile, o *protocol.ResourceOverride) protocol.TierProfile {
	if o == nil {
		return p
	}
	if o.VCPU != nil && *o.VCPU > 0 {
		p.VCPU = *o.VCPU
	}
	if o.MemoryMB != nil && *o.MemoryMB > 0 {
		p.MemoryMB = *o.MemoryMB
	}
	if o.DiskGB != nil && *o.DiskGB > 0 {
		p.DiskGB = *o.DiskGB
	}
	if o.GPU != nil {
		p.GPU = *o.GPU
	}
	if o.Image != nil && *o.Image != "" {
		p.Image = *o.Image
	}
	return p
}
