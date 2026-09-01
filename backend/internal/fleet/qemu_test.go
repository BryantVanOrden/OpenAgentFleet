package fleet

import (
	"strings"
	"testing"
)

// The QEMU driver's contract, pinned where a unit test can reach it. The full
// boot is exercised end to end by `make smoke-vm` against a real engine; these
// pin the decisions that make the driver honest rather than another stub:
// which image it runs, and that the pairing between the sandbox image and its
// VM conversion is by construction.

func TestVMImageIsDerivedFromTheSandboxImage(t *testing.T) {
	// The guest disk inside the -vm image is BUILT FROM the sandbox image, so
	// deriving the tag (rather than configuring a second one) is what makes
	// the pairing impossible to misconfigure.
	cases := map[string]string{
		"agentfleet/sandbox:latest": "agentfleet/sandbox:latest-vm",
		"registry.example.com/fleet/sandbox:v2": "registry.example.com/fleet/sandbox:v2-vm",
	}
	for in, want := range cases {
		if got := vmImageFor(in); got != want {
			t.Errorf("vmImageFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildTargetKnowsTheVMImage(t *testing.T) {
	// The "image unavailable" error names the make target that builds the
	// missing tag. Pointing a -vm miss at `make sandbox` would rebuild the
	// base and leave the runner still missing — the developer-heavy tier had
	// exactly this bug before buildTargetFor existed.
	if got := buildTargetFor("agentfleet/sandbox:latest-vm"); got != "make sandbox-vm" {
		t.Errorf("buildTargetFor(-vm) = %q", got)
	}
	if got := buildTargetFor("agentfleet/sandbox:latest-dev"); got != "make sandbox-dev" {
		t.Errorf("buildTargetFor(-dev) = %q", got)
	}
}

func TestVMTagIsRecognisedBeforeDevTag(t *testing.T) {
	// A hypothetical dev VM tag ends in -dev-vm; the -vm suffix must win, or
	// the operator is sent to a target that builds the wrong image.
	if got := buildTargetFor("agentfleet/sandbox:latest-dev-vm"); got != "make sandbox-vm" {
		t.Errorf("buildTargetFor(-dev-vm) = %q, want make sandbox-vm", got)
	}
}

func TestQEMUKVMErrorIsRecognisable(t *testing.T) {
	// The create-retry drops /dev/kvm only for errors that actually mention
	// it. This pins the engine's real message shape so the match cannot rot
	// silently.
	msg := `docker /containers/create?name=af-x: 400: {"message":"error gathering device information while adding custom device \"/dev/kvm\": no such file or directory"}`
	if !strings.Contains(strings.ToLower(msg), "kvm") {
		t.Fatal("the engine's no-kvm error no longer mentions kvm; the retry heuristic is dead")
	}
}
