package fleet

import (
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Ports exposed by every sandbox image.
const (
	PortNoVNC = 6901 // websockified HTML5 desktop
	// Same desktop, served by a -viewonly x11vnc. Auditors are routed here;
	// enforcing read-only in the client would not survive an edited URL.
	PortNoVNCView = 6902
	PortAgentd    = 7900 // observe/act/record control plane
	PortSignal    = 8090 // WebRTC signalling (selkies), when built in
)

// DefaultTiers are the shipped hardware profiles. They are advisory: the admin
// panel can override any field per instance, and Manager clamps the result to
// what the host can actually admit — see clamp.go, which is where that finally
// became true. This comment described the behaviour for a long time before
// anything implemented it, so `developer-heavy` (8 vCPU) could not start on any
// host with fewer cores.
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

// buildTargetFor names the make target that produces an image tag.
//
// The developer-heavy tier runs a different image built from a different
// Dockerfile, so telling everyone to run `make sandbox` sent whoever picked
// that tier to a command that rebuilds the base and leaves the tag they need
// still missing.
func buildTargetFor(image string) string {
	if strings.HasSuffix(image, "-vm") {
		return "make sandbox-vm"
	}
	if strings.HasSuffix(image, "-dev") {
		return "make sandbox-dev"
	}
	return "make sandbox"
}

// vmImageFor derives the VM runner's tag from the sandbox image: the guest
// disk inside it is built FROM that image, so the pairing is by construction.
func vmImageFor(image string) string {
	return image + "-vm"
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

// HasTier reports whether a tier name matches a configured profile. TierByName
// deliberately falls back to a sane default, which is right for an unset tier
// and wrong for a misspelled one; callers that can tell the difference use this
// first.
func HasTier(tiers []protocol.TierProfile, name protocol.Tier) bool {
	for _, t := range tiers {
		if t.Name == name {
			return true
		}
	}
	return false
}

// TierNames lists the configured tiers, for error messages.
func TierNames(tiers []protocol.TierProfile) string {
	names := make([]string, 0, len(tiers))
	for _, t := range tiers {
		names = append(names, string(t.Name))
	}
	return strings.Join(names, ", ")
}

// securityOpts returns the container security options for an instance.
//
// no-new-privileges is deliberately not set, and that is a considered trade
// rather than an oversight.
//
// It is the stronger control: the kernel ignores every setuid bit in the
// container, so sudo cannot escalate whatever the filesystem says. But the
// kernel applies it when the container is created and it cannot be changed
// afterwards. Using it to gate sudo therefore meant the only way to revoke
// sudo from an agent was to recreate its container — and these sandboxes carry
// no volume, so that discards everything the agent has done. Being unable to
// take privilege away from a misbehaving agent without destroying its work is
// the wrong failure to build in; the moment you most want to revoke sudo is
// mid-incident, which is exactly when losing the workspace costs most.
//
// Sudo is gated instead by the setuid bit on /usr/bin/sudo, cleared at
// provision unless asked for and changeable at any time through
// Manager.SetSudo. That is still a real boundary: with the bit cleared sudo
// cannot escalate, and the agent runs unprivileged so it cannot restore the
// bit — doing so needs the privilege being withheld. What is lost is the
// backstop, so a vulnerability in sudo or another setuid binary is now
// reachable where the kernel used to refuse outright.
func securityOpts(sudo bool) []string {
	_ = sudo // sudo is enforced by the setuid bit, not by a container option.
	return nil
}
