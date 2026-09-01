package fleet

import (
	"os"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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

func TestQEMUEgressEnvReachesTheRunner(t *testing.T) {
	// The policy is enforced in the runner's netns — QEMU's SLIRP sockets are
	// the guest's only way out — so the EGRESS_* variables must reach the
	// runner container through the same env every sandbox gets. If someone
	// makes sandboxEnv driver-aware and drops them for qemu, a policied VM
	// boots open, which is the exact failure the old create-time refusal
	// existed to prevent.
	m := &Manager{}
	inst := &protocol.Instance{
		ID:     "i-test",
		Driver: protocol.DriverQEMU,
		Egress: protocol.EgressPolicy{
			Allow:      []string{"example.com"},
			Deny:       []string{"10.9.8.7"},
			BlockLocal: true,
		},
	}
	env := strings.Join(m.sandboxEnv(inst, protocol.TierProfile{}), "\n")
	for _, want := range []string{
		"EGRESS_ALLOW=example.com",
		"EGRESS_DENY=10.9.8.7",
		"EGRESS_BLOCK_LOCAL=true",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("runner env is missing %q; the VM would boot with no egress policy", want)
		}
	}
}

func TestGuestNeverLearnsItsEgressPolicy(t *testing.T) {
	// vm-entrypoint forwards a whitelist of variables into the guest on the
	// kernel command line. EGRESS_* must never be on it: enforcement lives in
	// the runner precisely because the guest cannot be trusted with it, and a
	// forwarded copy invites someone to "optimise" by enforcing guest-side.
	// The same file must also apply the policy before QEMU starts, fail-closed.
	src, err := os.ReadFile("../../../sandbox/vm-entrypoint.sh")
	if err != nil {
		t.Skipf("vm-entrypoint.sh not readable from the test's working directory: %v", err)
	}
	text := string(src)

	egressAt := strings.Index(text, "egress.sh")
	bootAt := strings.Index(text, "qemu-system-x86_64")
	if egressAt == -1 {
		t.Fatal("vm-entrypoint.sh no longer applies egress.sh; policied VMs boot unrestricted")
	}
	if bootAt != -1 && egressAt > bootAt {
		t.Fatal("egress.sh runs after QEMU starts; guest packets race the policy")
	}
	if !strings.Contains(text, "exit 1") {
		t.Fatal("vm-entrypoint.sh does not fail closed when the policy cannot be applied")
	}
	if i := strings.Index(text, "for var in"); i != -1 {
		j := strings.Index(text[i:], "; do")
		if j != -1 && strings.Contains(text[i:i+j], "EGRESS") {
			t.Fatal("EGRESS_* is on the guest cmdline whitelist; the policy belongs to the runner only")
		}
	}
}

func TestEgressPolicyKeepsControlPlaneReplies(t *testing.T) {
	// Allow-list mode ends in a drop-everything rule. Reply packets to
	// connections opened from outside — the orchestrator's health poll above
	// all — must be accepted before that drop, or an allow-only instance
	// provisions, restricts itself, and is declared dead for answering nobody.
	src, err := os.ReadFile("../../../sandbox/egress.sh")
	if err != nil {
		t.Skipf("egress.sh not readable from the test's working directory: %v", err)
	}
	text := string(src)
	if strings.Contains(text, "nft flush ruleset") {
		t.Fatal("egress.sh flushes the whole ruleset; that destroys Docker's embedded-DNS " +
			"NAT rules in the same netns and breaks resolution in every policied sandbox")
	}
	ctAt := strings.Index(text, "ct state established,related accept")
	dropAt := strings.Index(text, `daddr 0.0.0.0/0 drop`)
	if ctAt == -1 {
		t.Fatal("egress.sh has no established/related accept; allow-list mode kills health-check replies")
	}
	if dropAt != -1 && ctAt > dropAt {
		t.Fatal("the established/related accept sits after the allow-list drop, where it matches nothing")
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
