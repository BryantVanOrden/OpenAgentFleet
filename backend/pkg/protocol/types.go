// Package protocol holds the wire types shared between the orchestrator, the
// in-sandbox agent daemon, the admin panel and the Flutter companion app.
package protocol

import "time"

// ---------------------------------------------------------------- hardware ---

// Tier is a named hardware profile. Callers may override individual fields on a
// per-instance basis; the tier only supplies the defaults.
type Tier string

const (
	TierMicro    Tier = "micro"
	TierStandard Tier = "standard"
	TierPower    Tier = "power-user"
	TierDevHeavy Tier = "developer-heavy"
)

// Driver selects the virtualisation backend used to realise an instance.
type Driver string

const (
	DriverDocker Driver = "docker" // shared kernel, seconds to boot
	DriverQEMU   Driver = "qemu"   // full VM, GPU passthrough, heavy builds
)

// TierProfile is the resource envelope handed to the driver.
type TierProfile struct {
	Name        Tier    `json:"name"`
	Description string  `json:"description"`
	Driver      Driver  `json:"driver"`
	Image       string  `json:"image"`
	VCPU        float64 `json:"vcpu"`
	MemoryMB    int64   `json:"memory_mb"`
	DiskGB      int64   `json:"disk_gb"`
	GPU         bool    `json:"gpu"`
	// ShmMB matters for browser-heavy and Electron workloads.
	ShmMB int64 `json:"shm_mb"`
}

// ResourceOverride lets an operator deviate from the tier defaults.
type ResourceOverride struct {
	VCPU     *float64 `json:"vcpu,omitempty"`
	MemoryMB *int64   `json:"memory_mb,omitempty"`
	DiskGB   *int64   `json:"disk_gb,omitempty"`
	GPU      *bool    `json:"gpu,omitempty"`
	Image    *string  `json:"image,omitempty"`
}

// --------------------------------------------------------------- instances ---

type InstanceState string

const (
	InstanceProvisioning InstanceState = "provisioning"
	InstanceRunning      InstanceState = "running"
	InstancePaused       InstanceState = "paused"
	InstanceStopped      InstanceState = "stopped"
	InstanceError        InstanceState = "error"
)

// Instance is one sandboxed operating system.
type Instance struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	OwnerID           string            `json:"owner_id"`
	ArchetypeID       string            `json:"archetype_id,omitempty"`  // bot archetype template id
	SystemPrompt      string            `json:"system_prompt,omitempty"` // specialized persona instructions
	PreinstalledTools []string          `json:"preinstalled_tools,omitempty"`
	Tier              Tier              `json:"tier"`
	Driver            Driver            `json:"driver"`
	State             InstanceState     `json:"state"`
	Runtime           string            `json:"runtime_id,omitempty"` // container or domain id
	Profile           TierProfile       `json:"profile"`
	Override          *ResourceOverride `json:"override,omitempty"`
	VNCURL            string            `json:"vnc_url,omitempty"`
	// VNCViewURL is the same desktop served by a -viewonly VNC server. The
	// auditor role is proxied here, so "may watch, may not touch" is enforced by
	// the server rather than by a client-side flag anyone could edit away.
	VNCViewURL  string            `json:"vnc_view_url,omitempty"`
	StreamURL   string            `json:"stream_url,omitempty"` // WebRTC signalling
	AgentdURL   string            `json:"agentd_url,omitempty"` // internal only
	Egress      EgressPolicy      `json:"egress"`
	ShellAccess bool              `json:"shell_access"`
	Labels      map[string]string `json:"labels,omitempty"`
	LastError   string            `json:"last_error,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// EgressPolicy constrains what the sandbox may talk to. An empty Allow list
// means "everything except Deny"; a non-empty Allow list is exclusive.
type EgressPolicy struct {
	Allow      []string `json:"allow,omitempty"`
	Deny       []string `json:"deny,omitempty"`
	BlockLocal bool     `json:"block_local"` // drop RFC1918 / link-local traffic
}

// InstanceStats is a point-in-time resource sample.
type InstanceStats struct {
	InstanceID  string    `json:"instance_id"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemoryBytes int64     `json:"memory_bytes"`
	MemoryLimit int64     `json:"memory_limit"`
	RxBytes     int64     `json:"rx_bytes"`
	TxBytes     int64     `json:"tx_bytes"`
	SampledAt   time.Time `json:"sampled_at"`
}

// ------------------------------------------------------------------- tasks ---

type TaskState string

const (
	TaskQueued        TaskState = "queued"
	TaskRunning       TaskState = "running"
	TaskAwaitingHuman TaskState = "awaiting_human"
	TaskSucceeded     TaskState = "succeeded"
	TaskFailed        TaskState = "failed"
	TaskCancelled     TaskState = "cancelled"
)

// Task is one unit of autonomous work assigned to an instance.
type Task struct {
	ID           string            `json:"id"`
	InstanceID   string            `json:"instance_id"`
	OwnerID      string            `json:"owner_id"`
	Goal         string            `json:"goal"`
	SkillID      string            `json:"skill_id,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	ParentTaskID string            `json:"parent_task_id,omitempty"` // for recursive sub-agents
	AutoRefine   bool              `json:"auto_refine,omitempty"`    // trigger skill self-refinement on success
	State        TaskState         `json:"state"`
	Step         int               `json:"step"`
	MaxSteps     int               `json:"max_steps"`
	ProviderID   string            `json:"provider_id,omitempty"`
	Error        string            `json:"error,omitempty"`
	Result       string            `json:"result,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	StartedAt    *time.Time        `json:"started_at,omitempty"`
	EndedAt      *time.Time        `json:"ended_at,omitempty"`
}

// ------------------------------------------------------- perception/action ---

// MarkItem is one numbered element in a Set-of-Marks overlay: the box drawn on
// the screenshot and the accessible identity behind it.
type MarkItem struct {
	ID     int    `json:"id"`
	Role   string `json:"role,omitempty"`
	Label  string `json:"label,omitempty"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	CX     int    `json:"cx"`
	CY     int    `json:"cy"`
}

// Observation is what the daemon reports about the current desktop.
//
// Coordinate spaces, because getting these confused is how clicks land in the
// wrong place:
//
//   - Width/Height are the dimensions of the captured frame BEFORE encoding —
//     the full desktop, or the crop region when zoomed.
//   - Scale is encoded-image width / frame width. The model sees the encoded
//     image and answers in ITS pixel space.
//   - OriginX/OriginY locate the frame on the desktop; non-zero only for crops.
//
// So: desktop = origin + (model coordinate / scale). ToDesktop does this.
type Observation struct {
	ScreenshotB64 string     `json:"screenshot_b64,omitempty"` // WebP, base64
	Width         int        `json:"width"`
	Height        int        `json:"height"`
	Scale         float64    `json:"scale"`
	OriginX       int        `json:"origin_x"`
	OriginY       int        `json:"origin_y"`
	DesktopWidth  int        `json:"desktop_width"`
	DesktopHeight int        `json:"desktop_height"`
	ActiveWindow  string     `json:"active_window"`
	A11yTree      string     `json:"a11y_tree,omitempty"` // flattened, indented
	Marks         []MarkItem `json:"marks,omitempty"`     // Set-of-Marks visual overlay elements
	// Hash is always taken over the full desktop, never a crop, so zooming does
	// not blind stall detection to activity elsewhere on screen.
	Hash       string    `json:"hash"`
	CapturedAt time.Time `json:"captured_at"`
}

// ImageWidth is the width of the encoded image the model actually receives.
func (o *Observation) ImageWidth() int {
	if o.Scale <= 0 {
		return o.Width
	}
	return int(float64(o.Width) * o.Scale)
}

func (o *Observation) ImageHeight() int {
	if o.Scale <= 0 {
		return o.Height
	}
	return int(float64(o.Height) * o.Scale)
}

// ToDesktop maps a coordinate the model gave in image space onto the real
// display. Clamped, because a model that overshoots the edge of the picture
// should click the edge rather than have the action rejected.
func (o *Observation) ToDesktop(x, y int) (int, int) {
	scale := o.Scale
	if scale <= 0 {
		scale = 1
	}
	dx := o.OriginX + int(float64(x)/scale)
	dy := o.OriginY + int(float64(y)/scale)

	maxX, maxY := o.DesktopWidth, o.DesktopHeight
	if maxX <= 0 || maxY <= 0 {
		maxX, maxY = o.OriginX+o.Width, o.OriginY+o.Height
	}
	return clampInt(dx, 0, maxX-1), clampInt(dy, 0, maxY-1)
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ActionKind enumerates everything the model is allowed to emit.
type ActionKind string

const (
	ActClick       ActionKind = "click"
	ActDoubleClick ActionKind = "double_click"
	ActRightClick  ActionKind = "right_click"
	ActType        ActionKind = "type"
	ActKey         ActionKind = "key"
	ActScroll      ActionKind = "scroll"
	ActDrag        ActionKind = "drag"
	ActWait        ActionKind = "wait"
	ActWaitFor     ActionKind = "wait_for"     // poll until text appears on screen
	ActFocus       ActionKind = "focus"        // raise a window by title
	ActShell       ActionKind = "shell"        // gated by Instance.ShellAccess
	ActPython      ActionKind = "python"       // persistent Python REPL execution in sandbox
	ActSpawnAgent  ActionKind = "spawn_agent"  // recursive sub-agent delegation
	ActMountTool   ActionKind = "mount_tool"   // dynamically synthesize and register a custom tool
	ActUnmountTool ActionKind = "unmount_tool" // dispose of a mounted custom tool
	ActCallTool    ActionKind = "call_tool"    // execute a dynamically mounted custom tool
	ActDeepSearch  ActionKind = "deep_search"  // live web search and intelligence synthesis
	ActRemember     ActionKind = "remember"      // store long-term episodic memory across the fleet
	ActRecall       ActionKind = "recall"        // semantically search fleet episodic memory
	ActSpeak        ActionKind = "speak"         // spoken voice output via Pocket TTS
	ActMsgPeer      ActionKind = "message_peer"  // send message/question to a peer bot in the fleet
	ActDelegateTask ActionKind = "delegate_task" // manager bot delegates sub-task to a peer bot
	ActShareSecret  ActionKind = "share_secret"  // publish a variable/secret to the shared fleet vault
	ActShareSession ActionKind = "share_session" // share cookies/auth session with peer bots
	ActAssert       ActionKind = "assert"        // file exists / size / exit code
	ActAskHuman     ActionKind = "ask_human"     // hand control back to the operator
	ActDone         ActionKind = "done"
	ActFail         ActionKind = "fail"
)

// PeerInfo represents a live peer bot running in the fleet.
type PeerInfo struct {
	InstanceID   string   `json:"instance_id"`
	Name         string   `json:"name"`
	ArchetypeID  string   `json:"archetype_id"`
	State        string   `json:"state"`
	CurrentTask  string   `json:"current_task,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// PeerMessage represents an inter-bot message or delegation.
type PeerMessage struct {
	ID               string         `json:"id"`
	FromInstanceID   string         `json:"from_instance_id"`
	FromInstanceName string         `json:"from_instance_name"`
	ToInstanceID     string         `json:"to_instance_id"` // Target instance or "broadcast"
	Kind             string         `json:"kind"`           // "message", "question", "report", "delegation"
	Content          string         `json:"content"`
	Data             map[string]any `json:"data,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

// SharedSecret represents a variable or secret accessible across the fleet.
type SharedSecret struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Scope     string    `json:"scope"` // "fleet", "instance:<id>", "swarm:<id>"
	Note      string    `json:"note,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SharedSession represents browser cookies and session storage exported by an agent.
type SharedSession struct {
	ID                string    `json:"id"`
	Domain            string    `json:"domain"`
	Title             string    `json:"title"`
	CookiesJSON       string    `json:"cookies_json"`
	LocalStorageJSON  string    `json:"local_storage_json,omitempty"`
	CreatedByInstance string    `json:"created_by_instance,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// MemoryRecord represents a long-term cross-fleet semantic memory item.
type MemoryRecord struct {
	ID               string    `json:"id"`
	Namespace        string    `json:"namespace"`
	Title            string    `json:"title"`
	Content          string    `json:"content"`
	Tags             []string  `json:"tags,omitempty"`
	Embedding        []float32 `json:"embedding,omitempty"`
	SourceTaskID     string    `json:"source_task_id,omitempty"`
	SourceInstanceID string    `json:"source_instance_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// MountedTool describes an ephemeral custom tool synthesized by the agent.
type MountedTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	HandlerCode string         `json:"handler_code,omitempty"`
}

// Action is the structured decision returned by the model each turn.
type Action struct {
	Thought         string            `json:"thought"`
	Action          ActionKind        `json:"action"`
	Target          string            `json:"target,omitempty"`      // accessible label / window title
	Mark            int               `json:"mark,omitempty"`        // Set-of-Marks ID (e.g. 1, 2, 3)
	Coordinates     []int             `json:"coordinates,omitempty"` // [x,y] in desktop pixels
	To              []int             `json:"to,omitempty"`          // drag destination
	Text            string            `json:"text,omitempty"`
	Key             string            `json:"key,omitempty"`              // e.g. "ctrl+shift+p"
	Code            string            `json:"code,omitempty"`             // for python action
	Query           string            `json:"query,omitempty"`            // for deep_search action
	SubGoal         string            `json:"sub_goal,omitempty"`         // for spawn_agent action
	SubSkillID      string            `json:"sub_skill_id,omitempty"`     // for spawn_agent action
	SubParams       map[string]string `json:"sub_params,omitempty"`       // for spawn_agent action
	WaitChild       bool              `json:"wait_child,omitempty"`       // whether spawn_agent blocks for child
	ToolName        string            `json:"tool_name,omitempty"`        // for mount_tool/unmount_tool/call_tool
	ToolDescription string            `json:"tool_description,omitempty"` // for mount_tool
	ToolParameters  map[string]any    `json:"tool_parameters,omitempty"`  // for mount_tool/call_tool
	ToolHandler     string            `json:"tool_handler,omitempty"`     // for mount_tool Python code
	PeerID          string            `json:"peer_id,omitempty"`          // target peer bot instance ID for message_peer/delegate_task
	SecretKey       string            `json:"secret_key,omitempty"`       // key for share_secret
	SecretVal       string            `json:"secret_val,omitempty"`       // value for share_secret
	SessionDomain   string            `json:"session_domain,omitempty"`   // domain for share_session
	SessionCookies  string            `json:"session_cookies,omitempty"`  // cookies JSON for share_session
	Amount          int               `json:"amount,omitempty"`           // scroll clicks / wait seconds
	Timeout         int               `json:"timeout,omitempty"`          // seconds, for wait_for
	Question        string            `json:"question,omitempty"`
	Summary         string            `json:"summary,omitempty"` // filled on done/fail
}

// StepRecord is one persisted turn of the agent loop, used for audit replay.
type StepRecord struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	Step         int       `json:"step"`
	Action       Action    `json:"action"`
	Observation  string    `json:"observation_key,omitempty"` // object-store key
	Outcome      string    `json:"outcome"`
	DurationMS   int64     `json:"duration_ms"`
	PromptTokens int       `json:"prompt_tokens"`
	OutTokens    int       `json:"output_tokens"`
	At           time.Time `json:"at"`
}

// ------------------------------------------------------------------ skills ---

// SkillStep is one semantic instruction compiled from a human demonstration.
type SkillStep struct {
	Index       int               `json:"index"`
	Kind        ActionKind        `json:"kind"`
	Window      string            `json:"window,omitempty"`
	Role        string            `json:"role,omitempty"`     // a11y role, e.g. "push button"
	Label       string            `json:"label,omitempty"`    // accessible name
	Selector    string            `json:"selector,omitempty"` // DOM selector when in-browser
	Coordinates []int             `json:"coordinates,omitempty"`
	Text        string            `json:"text,omitempty"`
	Key         string            `json:"key,omitempty"`
	Param       string            `json:"param,omitempty"` // variable name, if parameterised
	Assert      string            `json:"assert,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// Skill is a recorded, editable workflow.
type Skill struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Description     string      `json:"description"`
	Params          []string    `json:"params,omitempty"`
	Steps           []SkillStep `json:"steps"`
	Markdown        string      `json:"markdown"` // rendered SKILL.md handed to the model
	SourceRunID     string      `json:"source_run_id,omitempty"`
	Version         int         `json:"version"`                    // incremented on refinement
	RefinementNotes string      `json:"refinement_notes,omitempty"` // notes from self-improvement loop
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// RawEvent is a single captured input/accessibility event from the recorder.
type RawEvent struct {
	T        float64           `json:"t"` // seconds since recording start
	Type     string            `json:"type"`
	Button   string            `json:"button,omitempty"`
	Key      string            `json:"key,omitempty"`
	X        int               `json:"x,omitempty"`
	Y        int               `json:"y,omitempty"`
	Window   string            `json:"window,omitempty"`
	Role     string            `json:"role,omitempty"`
	Label    string            `json:"label,omitempty"`
	Selector string            `json:"selector,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
}

// ------------------------------------------------------------------ models ---

// ProviderKind selects the connector implementation.
type ProviderKind string

const (
	ProviderOpenAI     ProviderKind = "openai"
	ProviderOllama     ProviderKind = "ollama"
	ProviderAnthropic  ProviderKind = "anthropic"
	ProviderGemini     ProviderKind = "gemini"
	ProviderCompatible ProviderKind = "openai-compatible" // vLLM, LocalAI, LiteLLM, ...
)

// Provider is a configured model endpoint.
type Provider struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Kind        ProviderKind `json:"kind"`
	BaseURL     string       `json:"base_url,omitempty"`
	Model       string       `json:"model"`
	APIKeyRef   string       `json:"api_key_ref,omitempty"` // vault key, never the secret
	Vision      bool         `json:"vision"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`
	// Priority orders the fallback chain; lower runs first.
	Priority  int       `json:"priority"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ------------------------------------------------------------------ alerts ---

type AlertKind string

const (
	AlertNeedsHuman AlertKind = "needs_human"
	AlertStalled    AlertKind = "stalled"
	AlertFailed     AlertKind = "failed"
	AlertCompleted  AlertKind = "completed"
	AlertBudget     AlertKind = "budget"
	AlertResource   AlertKind = "resource"
)

// Alert is a push-notifiable event that may require an operator response.
type Alert struct {
	ID           string     `json:"id"`
	Kind         AlertKind  `json:"kind"`
	Severity     string     `json:"severity"` // info | warn | critical
	InstanceID   string     `json:"instance_id,omitempty"`
	TaskID       string     `json:"task_id,omitempty"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	ScreenshotID string     `json:"screenshot_id,omitempty"`
	NeedsReply   bool       `json:"needs_reply"`
	Reply        string     `json:"reply,omitempty"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// ------------------------------------------------------------------ events ---

// Event is broadcast over the WebSocket bus to admin and mobile clients.
type Event struct {
	Type       string    `json:"type"` // task.step, task.state, instance.state, alert, stats, chat
	InstanceID string    `json:"instance_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	Payload    any       `json:"payload,omitempty"`
	At         time.Time `json:"at"`
}

// ------------------------------------------------------------------- users ---

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleAuditor  Role = "auditor"
)

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}
