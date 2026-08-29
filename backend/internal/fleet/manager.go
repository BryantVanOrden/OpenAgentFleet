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

	"github.com/BryantVanOrden/AgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
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
	// OrgID is the department the bot is created into.
	OrgID string `json:"org_id,omitempty"`
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
	inst := &protocol.Instance{
		ID:                id,
		Name:              name,
		OwnerID:           req.OwnerID,
		OrgID:             req.OrgID,
		ArchetypeID:       req.ArchetypeID,
		SystemPrompt:      req.SystemPrompt,
		PreinstalledTools: req.PreinstalledTools,
		Tier:              profile.Name,
		Driver:            profile.Driver,
		State:             protocol.InstanceProvisioning,
		Profile:           profile,
		Override:          req.Override,
		Egress:            req.Egress,
		Voice:             req.Voice,
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
	if !m.docker.ImageExists(ctx, p.Image) {
		m.log.Info("pulling sandbox image", "image", p.Image)
		if err := m.docker.PullImage(ctx, p.Image); err != nil {
			return fmt.Errorf("image %s unavailable: %w (build it with `make sandbox`)", p.Image, err)
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
			// no-new-privileges is what actually stops sudo inside the sandbox:
			// the kernel ignores sudo's setuid bit, so the sudoers deny-list in
			// the image is only a guardrail behind it. Dropping it is therefore
			// a real reduction in containment, which is why it is opt-in per
			// instance and cannot be changed without recreating the container.
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
		return fmt.Errorf("start container: %w", err)
	}

	ci, err := m.docker.InspectContainer(ctx, cid)
	if err != nil {
		return err
	}
	ip := ci.IPOn(m.cfg.SandboxNetwork)
	if ip == "" {
		return fmt.Errorf("container has no address on %s", m.cfg.SandboxNetwork)
	}
	inst.AgentdURL = fmt.Sprintf("http://%s:%d", ip, PortAgentd)
	inst.VNCURL = fmt.Sprintf("http://%s:%d", ip, PortNoVNC)
	inst.VNCViewURL = fmt.Sprintf("http://%s:%d", ip, PortNoVNCView)
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
	inst.PreinstalledTools = m.verifyTools(ctx, inst, inst.PreinstalledTools)

	inst.State = protocol.InstanceRunning
	inst.LastError = ""
	return m.db.UpdateInstance(ctx, inst)
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
		// The tool name is echoed, not the binary, so the caller gets back the
		// names it asked about.
		fmt.Fprintf(&sb, "command -v %q >/dev/null 2>&1 && echo %q\n", bin, t)
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
		return err
	}
	ci, err := m.docker.InspectContainer(ctx, inst.Runtime)
	if err != nil {
		return err
	}
	if ip := ci.IPOn(m.cfg.SandboxNetwork); ip != "" {
		inst.AgentdURL = fmt.Sprintf("http://%s:%d", ip, PortAgentd)
		inst.VNCURL = fmt.Sprintf("http://%s:%d", ip, PortNoVNC)
		inst.VNCViewURL = fmt.Sprintf("http://%s:%d", ip, PortNoVNCView)
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
