package fleet

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Clamping a tier to what the host can actually run.
//
// DefaultTiers has always described itself as advisory, with the Manager
// clamping the result "to what the host can actually admit". Nothing did. The
// tier's vCPU went straight to Docker as NanoCPUs, and asking for more CPUs than
// the host has is a hard 400 from the engine rather than a scheduling hint — so
// on a 4-core machine the `developer-heavy` tier (8 vCPU) could not start at
// all, and the operator got:
//
//	create container: docker /containers/create: 400: {"message":"range of CPUs
//	is from 0.01 to 4.00, as there are only 4 CPUs available"}
//
// which is accurate, unactionable, and arrives after the image has been pulled.
//
// Clamping rather than refusing is the right trade for this platform. A
// developer-heavy bot on a 4-core laptop is slower than the tier intends and is
// otherwise exactly what was asked for; refusing to start it would make the tier
// unusable on every machine smaller than its aspiration, which is most of them.
// What matters is that the operator is told, so "my builds are slow" has an
// answer.

// hostCapacity caches the engine's /info.
//
// Cached because provisioning already waits on an image pull and a desktop boot,
// and because the answer does not change while the daemon is running. The TTL
// exists so that a host resized under a running orchestrator is picked up
// without a restart.
type hostCapacityCache struct {
	mu       sync.Mutex
	value    HostCapacity
	fetched  time.Time
	lastErr  error
	cacheTTL time.Duration
}

var capacityCache = &hostCapacityCache{cacheTTL: 5 * time.Minute}

func (c *hostCapacityCache) get(ctx context.Context, docker *DockerClient) (HostCapacity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.fetched) < c.cacheTTL && c.value.NCPU > 0 {
		return c.value, nil
	}
	got, err := docker.Info(ctx)
	if err != nil {
		// The previous good answer beats no answer: a transient /info failure
		// should not turn into an unclamped request that the engine rejects.
		c.lastErr = err
		if c.value.NCPU > 0 {
			return c.value, nil
		}
		return HostCapacity{}, err
	}
	c.value, c.fetched, c.lastErr = got, time.Now(), nil
	return got, nil
}

// clampToHost reduces a profile to something the host will admit.
//
// Returns the adjusted profile and a human-readable note for each field that had
// to be reduced, empty when nothing was.
func clampToHost(p protocol.TierProfile, host HostCapacity) (protocol.TierProfile, []string) {
	var notes []string

	// CPU. Docker rejects anything above NCPU outright.
	if host.NCPU > 0 && p.VCPU > float64(host.NCPU) {
		notes = append(notes, fmt.Sprintf("vCPU %g -> %d (the host has %d)",
			p.VCPU, host.NCPU, host.NCPU))
		p.VCPU = float64(host.NCPU)
	}

	// Memory. Docker accepts a limit above host RAM, so this is not about being
	// rejected — it is about not handing one sandbox a limit it can only reach
	// by pushing the host into swap or the OOM killer, which takes the
	// orchestrator down with it. 80% leaves room for the orchestrator, the
	// database, and any other sandboxes.
	if host.MemTotal > 0 {
		maxMB := (host.MemTotal / (1024 * 1024)) * 80 / 100
		if p.MemoryMB > maxMB && maxMB > 0 {
			notes = append(notes, fmt.Sprintf("memory %d MB -> %d MB (80%% of the host's %d MB)",
				p.MemoryMB, maxMB, host.MemTotal/(1024*1024)))
			p.MemoryMB = maxMB
		}
	}

	// GPU. Without the NVIDIA container toolkit the request is not merely
	// ignored — the container fails at start, inside a prestart hook:
	//
	//   nvidia-container-cli: initialization error: WSL environment detected
	//   but no adapters were found
	//
	// which is a 500 from /start after the image is pulled and the container is
	// created. Dropping the request gives a working CPU sandbox, which is what
	// the developer-heavy tier is on a machine without a GPU anyway; the tier's
	// compilers do not need one.
	//
	// Only dropped when the host is known not to have it. An empty Runtimes map
	// means /info told us nothing, and guessing "no GPU" there would silently
	// disable GPUs on hosts that have them.
	if p.GPU && len(host.Runtimes) > 0 && !host.HasNVIDIARuntime() {
		notes = append(notes, "GPU dropped (no nvidia container runtime on this host)")
		p.GPU = false
	}

	// Shared memory has to stay inside the memory limit; a shm larger than the
	// container's own memory is counted against that limit and guarantees an OOM
	// the first time a browser fills it.
	if p.MemoryMB > 0 && p.ShmMB > p.MemoryMB/2 {
		reduced := p.MemoryMB / 2
		notes = append(notes, fmt.Sprintf("shm %d MB -> %d MB (half the memory limit)",
			p.ShmMB, reduced))
		p.ShmMB = reduced
	}

	return p, notes
}

// applyHostLimits clamps a profile and records what it had to change.
func (m *Manager) applyHostLimits(ctx context.Context, p protocol.TierProfile) protocol.TierProfile {
	host, err := capacityCache.get(ctx, m.docker)
	if err != nil {
		// Unclamped is what happened before this existed. Logged rather than
		// fatal: a host whose /info is unreadable can still run the small tiers,
		// and failing every provision over it would be worse.
		m.log.Warn("could not read host capacity; tier limits are not clamped", "err", err)
		return p
	}

	clamped, notes := clampToHost(p, host)
	if len(notes) > 0 {
		m.log.Info("tier clamped to host capacity",
			"tier", p.Name, "adjustments", strings.Join(notes, "; "))
	}
	return clamped
}

// isGPUUnavailable reports whether a container failed to start because the host
// could not actually provide the GPU it was asked for.
//
// Matched on the error text because the engine gives no structured signal: the
// failure surfaces as a generic 500 from /start whose message embeds the
// prestart hook's stderr. Matching is deliberately narrow — only errors that
// name the NVIDIA tooling qualify — because the fallback silently drops a
// capability, and doing that for an unrelated start failure would hide a real
// problem behind a working-looking sandbox.
func isGPUUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "nvidia") {
		return false
	}
	for _, marker := range []string{
		"no adapters were found",       // WSL with no GPU passed through
		"initialization error",         // nvidia-container-cli could not start
		"could not select device driver", // toolkit absent entirely
		"unknown or invalid runtime",
		"failed to initialize nvml",
		"no devices were found",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// ClampNote is the operator-facing summary of any reduction, for the instance's
// last_error field when it is otherwise empty.
func clampNote(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return "running below the tier's profile because the host is smaller: " +
		strings.Join(notes, "; ")
}
