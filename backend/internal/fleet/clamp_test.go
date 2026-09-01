package fleet

import (
	"errors"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// DefaultTiers has always described its profiles as advisory, with the Manager
// clamping them "to what the host can actually admit". Nothing did, so on a
// 4-core machine the developer-heavy tier (8 vCPU) could not start at all:
// Docker rejects a CPU request above NCPU with a hard 400, after the image has
// already been pulled.

func TestVCPUIsClampedToTheHostsCoreCount(t *testing.T) {
	tier := protocol.TierProfile{Name: protocol.TierDevHeavy, VCPU: 8, MemoryMB: 16384, ShmMB: 4096}
	host := HostCapacity{NCPU: 4, MemTotal: 64 << 30}

	got, notes := clampToHost(tier, host)
	if got.VCPU != 4 {
		t.Errorf("VCPU = %g, want 4 (the host's core count)", got.VCPU)
	}
	if len(notes) == 0 {
		t.Fatal("clamping happened silently; the operator has no way to know")
	}
	if !strings.Contains(notes[0], "vCPU") || !strings.Contains(notes[0], "4") {
		t.Errorf("note %q does not say what was reduced and to what", notes[0])
	}
}

func TestAProfileThatFitsIsLeftAlone(t *testing.T) {
	tier := protocol.TierProfile{Name: protocol.TierStandard, VCPU: 2, MemoryMB: 4096, ShmMB: 1024}
	host := HostCapacity{NCPU: 16, MemTotal: 64 << 30}

	got, notes := clampToHost(tier, host)
	if got.VCPU != 2 || got.MemoryMB != 4096 || got.ShmMB != 1024 {
		t.Errorf("a profile the host can satisfy was altered: %+v", got)
	}
	if len(notes) != 0 {
		t.Errorf("nothing was reduced but notes were produced: %v", notes)
	}
}

func TestMemoryIsClampedBelowTheWholeHost(t *testing.T) {
	// Docker accepts a limit above host RAM, so this is not about being
	// rejected — it is about not handing one sandbox a limit it can only reach
	// by pushing the host into swap or the OOM killer, taking the orchestrator
	// with it.
	tier := protocol.TierProfile{VCPU: 2, MemoryMB: 16384, ShmMB: 512}
	host := HostCapacity{NCPU: 8, MemTotal: 8 << 30} // 8 GB

	got, notes := clampToHost(tier, host)
	if got.MemoryMB >= 8192 {
		t.Errorf("memory = %d MB, want less than the host's 8192 MB", got.MemoryMB)
	}
	if got.MemoryMB != 8192*80/100 {
		t.Errorf("memory = %d MB, want 80%% of the host", got.MemoryMB)
	}
	if len(notes) == 0 {
		t.Error("the memory reduction was not reported")
	}
}

func TestShmStaysInsideTheMemoryLimit(t *testing.T) {
	// A shm larger than the container's own memory is counted against that
	// limit and guarantees an OOM the first time a browser fills it.
	tier := protocol.TierProfile{VCPU: 1, MemoryMB: 2048, ShmMB: 4096}
	host := HostCapacity{NCPU: 8, MemTotal: 64 << 30}

	got, notes := clampToHost(tier, host)
	if got.ShmMB > got.MemoryMB/2 {
		t.Errorf("shm = %d MB against a %d MB limit", got.ShmMB, got.MemoryMB)
	}
	if len(notes) == 0 {
		t.Error("the shm reduction was not reported")
	}
}

func TestClampingCascades(t *testing.T) {
	// developer-heavy on a small laptop: every dimension is over.
	tier := protocol.TierProfile{Name: protocol.TierDevHeavy, VCPU: 8, MemoryMB: 16384, ShmMB: 4096}
	host := HostCapacity{NCPU: 4, MemTotal: 8 << 30}

	got, notes := clampToHost(tier, host)
	if got.VCPU != 4 {
		t.Errorf("VCPU = %g, want 4", got.VCPU)
	}
	if got.MemoryMB > 8192 {
		t.Errorf("memory = %d MB, want below the host's 8192", got.MemoryMB)
	}
	// The shm clamp reads the *already reduced* memory, not the tier's original.
	if got.ShmMB > got.MemoryMB/2 {
		t.Errorf("shm = %d MB is more than half the clamped memory %d MB", got.ShmMB, got.MemoryMB)
	}
	if len(notes) < 3 {
		t.Errorf("expected a note per reduced dimension, got %d: %v", len(notes), notes)
	}
}

func TestGPUIsDroppedWhenTheHostHasNoNVIDIARuntime(t *testing.T) {
	// Without the toolkit the request is not ignored — the container fails at
	// start inside a prestart hook, after the image is pulled and the container
	// is created. That is how developer-heavy failed on a WSL host with no
	// adapters.
	tier := protocol.TierProfile{Name: protocol.TierDevHeavy, VCPU: 2, MemoryMB: 4096, GPU: true}
	host := HostCapacity{
		NCPU: 8, MemTotal: 64 << 30,
		Runtimes: map[string]struct {
			Path string `json:"path"`
		}{"runc": {Path: "runc"}},
	}

	got, notes := clampToHost(tier, host)
	if got.GPU {
		t.Error("a GPU was requested on a host with no nvidia runtime")
	}
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, " "), "GPU") {
		t.Errorf("the dropped GPU was not reported: %v", notes)
	}
}

func TestGPUIsKeptWhenTheRuntimeIsPresent(t *testing.T) {
	tier := protocol.TierProfile{VCPU: 2, MemoryMB: 4096, GPU: true}
	host := HostCapacity{
		NCPU: 8, MemTotal: 64 << 30,
		Runtimes: map[string]struct {
			Path string `json:"path"`
		}{"runc": {Path: "runc"}, "nvidia": {Path: "nvidia-container-runtime"}},
	}

	got, _ := clampToHost(tier, host)
	if !got.GPU {
		t.Error("the GPU request was dropped on a host that has the runtime")
	}
}

func TestGPUIsNotDroppedWhenTheHostIsUnknown(t *testing.T) {
	// An empty Runtimes map means /info told us nothing. Guessing "no GPU"
	// there would silently disable GPUs on hosts that have them.
	tier := protocol.TierProfile{VCPU: 2, MemoryMB: 4096, GPU: true}
	got, _ := clampToHost(tier, HostCapacity{NCPU: 8, MemTotal: 64 << 30})
	if !got.GPU {
		t.Error("the GPU request was dropped without evidence the host lacks one")
	}
}

func TestUnknownHostCapacityChangesNothing(t *testing.T) {
	// A host whose /info could not be read should run the tier as written
	// rather than being clamped to zero, which would create a container that
	// cannot run anything.
	tier := protocol.TierProfile{VCPU: 8, MemoryMB: 16384, ShmMB: 1024}
	got, notes := clampToHost(tier, HostCapacity{})

	if got.VCPU != 8 || got.MemoryMB != 16384 {
		t.Errorf("an unknown host capacity altered the profile: %+v", got)
	}
	if len(notes) != 0 {
		t.Errorf("notes produced with no host information: %v", notes)
	}
}

func TestGPUUnavailableIsRecognisedFromTheEnginesError(t *testing.T) {
	// The engine gives no structured signal: the prestart hook's stderr is
	// embedded in a generic 500 from /start. These are the real messages.
	unavailable := []string{
		`start container: docker /containers/abc/start: 500: {"message":"failed to create task ` +
			`for container: ... error running prestart hook #0: exit status 1, stdout: , stderr: ` +
			`Auto-detected mode as 'legacy'\nnvidia-container-cli: initialization error: WSL ` +
			`environment detected but no adapters were found"}`,
		`could not select device driver "nvidia" with capabilities: [[gpu]]`,
		`nvidia-container-cli: initialization error: failed to initialize NVML: unknown error`,
		`docker: Error response from daemon: unknown or invalid runtime name: nvidia`,
	}
	for _, msg := range unavailable {
		if !isGPUUnavailable(errors.New(msg)) {
			t.Errorf("not recognised as a GPU problem:\n  %s", msg[:min(len(msg), 120)])
		}
	}

	// Narrow on purpose: the fallback silently drops a capability, so doing it
	// for an unrelated start failure would hide a real problem behind a
	// working-looking sandbox.
	unrelated := []string{
		"start container: 500: no such container",
		"start container: 500: port is already allocated",
		"start container: 500: OCI runtime create failed: exec: \"/bin/sh\": not found",
		"", // a nil error is handled by the caller, but the helper must be safe
	}
	for _, msg := range unrelated {
		var err error
		if msg != "" {
			err = errors.New(msg)
		}
		if isGPUUnavailable(err) {
			t.Errorf("unrelated failure treated as a GPU problem: %q", msg)
		}
	}
}

func TestClampNoteIsEmptyWhenNothingChanged(t *testing.T) {
	if got := clampNote(nil); got != "" {
		t.Errorf("clampNote(nil) = %q, want empty", got)
	}
	got := clampNote([]string{"vCPU 8 -> 4 (the host has 4)"})
	if !strings.Contains(got, "vCPU") || !strings.Contains(got, "host is smaller") {
		t.Errorf("clampNote = %q, want it to explain why", got)
	}
}
