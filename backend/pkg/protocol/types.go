// Package protocol holds the wire types shared between the orchestrator, the
// in-sandbox agent daemon, the admin panel and the Flutter companion app.
package protocol

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

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
	VNCViewURL  string       `json:"vnc_view_url,omitempty"`
	StreamURL   string       `json:"stream_url,omitempty"` // WebRTC signalling
	AgentdURL   string       `json:"agentd_url,omitempty"` // internal only
	Egress      EgressPolicy `json:"egress"`
	ShellAccess bool         `json:"shell_access"`
	// SudoAccess controls the setuid bit on /usr/bin/sudo inside the sandbox.
	// Changeable on a running agent through Manager.SetSudo; it used to be a
	// container option fixed at creation, and this said so long after that
	// stopped being true.
	SudoAccess bool `json:"sudo_access"`
	// Voice this agent speaks in. Empty falls back to the operator's default.
	// Distinct voices are what make a fleet legible by ear.
	Voice string `json:"voice,omitempty"`
	// VoiceSpeed is how fast this agent speaks, as a multiplier. 0 means the
	// operator's default: a distinct voice makes a fleet legible by ear, and
	// pace is the other half of that, but pinning every existing bot to a
	// rate nobody chose would not be.
	VoiceSpeed float64 `json:"voice_speed,omitempty"`
	// OrgIDs are the departments this bot belongs to. Empty means unassigned,
	// which only a global admin can see.
	//
	// Several, not one: a bot two teams both rely on had to be filed under
	// one of them and be invisible to the other. A member of any of these
	// departments can reach the bot, at whatever that department's role
	// allows -- so sharing a bot widens who can use it and never narrows it.
	OrgIDs []string `json:"org_ids,omitempty"`

	// CustomTools are operator-added tools with how to fetch them.
	CustomTools []CustomTool `json:"custom_tools,omitempty"`
	// ProviderIDs is this bot's own model fallback chain, most preferred first.
	// Empty means the fleet-wide order, which is what every bot had before.
	ProviderIDs []string          `json:"provider_ids,omitempty"`
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
//
// Coordinates here are DESKTOP pixels, not image pixels — unlike the
// coordinates a model reports, which are in the space of the picture it was
// shown. agentd derives marks from AT-SPI extents, which are already desktop
// coordinates, and only translates them into frame space for drawing.
//
// That asymmetry is deliberate but easy to get wrong in both directions: a mark
// centre must be used verbatim, and must NOT be passed through
// Observation.ToDesktop, or it would be scaled a second time and every
// set-of-marks click would land short. See annotate_frame in
// sandbox/agentd/som.py.
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
	ActClick        ActionKind = "click"
	ActDoubleClick  ActionKind = "double_click"
	ActRightClick   ActionKind = "right_click"
	ActType         ActionKind = "type"
	ActKey          ActionKind = "key"
	ActScroll       ActionKind = "scroll"
	ActDrag         ActionKind = "drag"
	ActWait         ActionKind = "wait"
	ActWaitFor      ActionKind = "wait_for"      // poll until text appears on screen
	ActFocus        ActionKind = "focus"         // raise a window by title
	ActShell        ActionKind = "shell"         // gated by Instance.ShellAccess
	ActPython       ActionKind = "python"        // persistent Python REPL execution in sandbox
	ActSpawnAgent   ActionKind = "spawn_agent"   // recursive sub-agent delegation
	ActMountTool    ActionKind = "mount_tool"    // dynamically synthesize and register a custom tool
	ActUnmountTool  ActionKind = "unmount_tool"  // dispose of a mounted custom tool
	ActCallTool     ActionKind = "call_tool"     // execute a dynamically mounted custom tool
	ActDeepSearch   ActionKind = "deep_search"   // live web search and intelligence synthesis
	ActRemember     ActionKind = "remember"      // store long-term episodic memory across the fleet
	ActRecall       ActionKind = "recall"        // semantically search fleet episodic memory
	ActSpeak        ActionKind = "speak"         // spoken voice output via Pocket TTS
	ActMsgPeer      ActionKind = "message_peer"  // send message/question to a peer bot in the fleet
	ActDelegateTask ActionKind = "delegate_task" // manager bot delegates sub-task to a peer bot
	ActShareSecret  ActionKind = "share_secret"  // publish a variable/secret to the shared fleet vault
	ActShareSession ActionKind = "share_session" // share cookies/auth session with peer bots
	ActSnapshot     ActionKind = "snapshot"      // create OS container workspace snapshot checkpoint
	ActRollback     ActionKind = "rollback"      // rollback OS container to a previous snapshot
	ActCallMCP      ActionKind = "call_mcp"      // invoke tool on a Model Context Protocol (MCP) server
	ActPublishWork  ActionKind = "publish_work"  // publish a file, app or workspace to the shared catalog
	ActReadWork     ActionKind = "read_work"     // read another agent's published work by name
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
	ID string `json:"id"`
	// ConversationID is the thread this message belongs to. Messages predating
	// conversations carry an empty one and are placed by their recipient.
	ConversationID string `json:"conversation_id,omitempty"`
	// Compacted marks a message that a summary has replaced. It stays in the
	// database and stops being shown or replayed.
	Compacted        bool   `json:"compacted,omitempty"`
	FromInstanceID   string `json:"from_instance_id"`
	FromInstanceName string `json:"from_instance_name"`
	// FromUserID identifies the person, for messages a human sent. Empty when
	// the sender is a bot.
	FromUserID   string         `json:"from_user_id,omitempty"`
	ToInstanceID string         `json:"to_instance_id"` // Target instance or "broadcast"
	Kind         string         `json:"kind"`           // "message", "question", "report", "delegation"
	Content      string         `json:"content"`
	Data         map[string]any `json:"data,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// Model roles. What a model is being asked to do, so a combination can send
// each kind of thinking to the model suited to it.
const (
	// RoleVision is perception and action: reading the screen and deciding the
	// next click. The hands.
	RoleVision = "vision"
	// RoleReasoning is planning, deduction and code. The brain.
	RoleReasoning = "reasoning"
	// RoleChat is talking to the operator.
	RoleChat = "chat"
	// RoleSummarize is compaction — cheap, high-volume, no judgement needed.
	RoleSummarize = "summarize"
	// RoleRefine is trajectory analysis and skill hardening.
	RoleRefine = "refine"
)

// ModelRoles lists every role, in the order a picker should show them.
var ModelRoles = []string{RoleVision, RoleReasoning, RoleChat, RoleSummarize, RoleRefine}

// ModelCombo assigns models to roles.
//
// A combination is selectable anywhere a provider is, including inside a bot's
// fallback chain — so "this pair of models, and if neither answers, that single
// one" is expressible.
type ModelCombo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Roles maps a role to the provider serving it. A role left unset falls
	// back within the combination before the chain moves on.
	Roles     map[string]string `json:"roles"`
	CreatedAt time.Time         `json:"created_at"`
}

// Simple reports whether this is a brain-and-hands pair rather than a full
// assignment, which is the distinction the UI draws.
func (c ModelCombo) Simple() bool {
	if len(c.Roles) > 2 {
		return false
	}
	for role := range c.Roles {
		if role != RoleVision && role != RoleReasoning {
			return false
		}
	}
	return true
}

// CustomTool is a tool the operator added by hand, with how to fetch it.
//
// The archetype catalogue cannot know about a company's internal CLI, and
// making someone edit a file inside the image to add one is not a workflow.
type CustomTool struct {
	Name string `json:"name"`
	// Method is how to get it: apt, pip, npm, go, url or github.
	Method string `json:"method"`
	// Spec is the argument for that method — a package name, a module path, a
	// download URL, or an owner/repo.
	Spec string `json:"spec"`
}

// ToolMethods are the ways a custom tool can be fetched.
var ToolMethods = []string{"apt", "pip", "npm", "go", "url", "github"}

// toolNameOK matches a name safe to use as a command and a filename. Deliberate
// allowlist: this value reaches a shell, and the last injection here came from
// assuming a tool name was well behaved.
var toolNameOK = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// specOK is permissive enough for URLs, module paths and owner/repo, and
// refuses the characters that give a shell or a URL fetch new meaning.
// The leading @ is for scoped npm packages, which are ordinary and would
// otherwise be refused.
var specOK = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9._+:/@#-]{0,255}$`)

// Validate reports whether a custom tool is safe and complete.
func (c CustomTool) Validate() error {
	if !toolNameOK.MatchString(c.Name) {
		return fmt.Errorf("tool name %q must be letters, digits, dot, underscore, plus or dash", c.Name)
	}
	known := false
	for _, m := range ToolMethods {
		if m == c.Method {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("unknown install method %q", c.Method)
	}
	if !specOK.MatchString(c.Spec) {
		return fmt.Errorf("install spec for %q contains characters that are not allowed", c.Name)
	}
	if c.Method == "github" && !strings.Contains(c.Spec, "/") {
		return fmt.Errorf("a github tool needs owner/repo, got %q", c.Spec)
	}
	return nil
}

// Conversation kinds. A conversation is an explicit, named thread rather than
// something inferred from who happened to message whom: the operator creates
// and deletes them, and agents talk inside them.
const (
	// ConversationDirect is you and one agent.
	ConversationDirect = "direct"
	// ConversationPair is two agents talking to each other. You can read it
	// without being in it — watching agents coordinate is the point.
	ConversationPair = "pair"
	// ConversationGroup is any other set of members, you included.
	ConversationGroup = "group"
	// ConversationBroadcast is an everyone-channel: every agent in the fleet
	// hears it, including agents provisioned after the thread was made. Its
	// stored member list is therefore not what decides who is in it -- a
	// frozen roster would quietly stop including new bots.
	ConversationBroadcast = "broadcast"
)

// BroadcastConversationID is the built-in channel every agent can hear. It is
// not stored, cannot be deleted, and is where an unaddressed message lands.
const BroadcastConversationID = "broadcast"

// OperatorMemberID identifies you in a conversation's member list. Agents are
// identified by instance ID, and no instance can hold this ID.
const OperatorMemberID = "operator"

// Conversation is a thread in fleet comms.
type Conversation struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Members are instance IDs, plus OperatorMemberID when you are in it.
	Members []string `json:"members"`
	// Pinned keeps a thread at the top of the list regardless of how recently
	// anyone spoke in it.
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"created_at"`

	// Hidden records that the built-in everyone-channel has been deleted.
	// That channel is implicit and has no row of its own until something is
	// stored about it, so "deleted" has to be stored as a fact rather than as
	// the absence of one. Never set on an ordinary thread, which is simply
	// removed.
	Hidden bool `json:"-"`

	// Populated on read for the conversation list; not stored.
	LastMessageAt time.Time `json:"last_message_at,omitzero"`
	MessageCount  int       `json:"message_count"`
}

// SharedSecret represents a variable or secret accessible across the fleet.
type SharedSecret struct {
	// OrgID scopes the secret to a department. Empty is admin-only.
	OrgID     string    `json:"org_id,omitempty"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Scope     string    `json:"scope"` // "fleet", "instance:<id>", "swarm:<id>"
	Note      string    `json:"note,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SharedSession represents browser cookies and session storage exported by an agent.
type SharedSession struct {
	// OrgID scopes the session to a department. Empty is admin-only.
	OrgID             string    `json:"org_id,omitempty"`
	ID                string    `json:"id"`
	Domain            string    `json:"domain"`
	Title             string    `json:"title"`
	CookiesJSON       string    `json:"cookies_json"`
	LocalStorageJSON  string    `json:"local_storage_json,omitempty"`
	CreatedByInstance string    `json:"created_by_instance,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// OSSnapshot represents an OS container workspace checkpoint.
type OSSnapshot struct {
	ID           string    `json:"id"`
	InstanceID   string    `json:"instance_id"`
	TaskID       string    `json:"task_id,omitempty"`
	StepNumber   int       `json:"step_number"`
	Name         string    `json:"name"`
	SnapshotPath string    `json:"snapshot_path"`
	FileCount    int       `json:"file_count"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
}

// MCPServer represents a configured Model Context Protocol server.
type MCPServer struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Transport  string            `json:"transport"` // "stdio" or "sse"
	Command    string            `json:"command"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	URL        string            `json:"url,omitempty"`
	ToolsCount int               `json:"tools_count"`
	Active     bool              `json:"active"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// MCPTool represents an available tool exposed by an MCP server.
type MCPTool struct {
	ServerID    string         `json:"server_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// PipelineNode represents one step/agent in a multi-bot workflow DAG.
type PipelineNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// InstanceID pins the node to one bot. Empty falls back to ArchetypeID,
	// which picks whichever eligible instance is free.
	InstanceID   string            `json:"instance_id,omitempty"`
	ArchetypeID  string            `json:"archetype_id,omitempty"`
	GoalTemplate string            `json:"goal_template"`
	Params       map[string]string `json:"params,omitempty"`
}

// PipelineEdge represents a directed dependency between two workflow nodes.
type PipelineEdge struct {
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
	Condition  string `json:"condition,omitempty"` // optional condition, e.g. "success"
}

// WorkflowPipeline represents a multi-bot DAG orchestration workflow.
type WorkflowPipeline struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Nodes       []PipelineNode `json:"nodes"`
	Edges       []PipelineEdge `json:"edges"`
	// MaxParallel bounds how many nodes run at once. Zero means the engine's
	// default. Every node starts a real task on a real desktop, so a graph with
	// no edges and no limit would try to start every node simultaneously.
	MaxParallel int       `json:"max_parallel,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PipelineRun tracks an execution of a workflow pipeline.
type PipelineRun struct {
	ID         string `json:"id"`
	PipelineID string `json:"pipeline_id"`
	Status     string `json:"status"` // "running", "completed", "failed", "cancelled"
	// CurrentNodeID is the most recently started node. It predates parallel
	// execution and cannot describe several nodes running at once; NodeStates is
	// the accurate answer. Kept because the console and the app both read it.
	CurrentNodeID string            `json:"current_node_id,omitempty"`
	NodeResults   map[string]string `json:"node_results,omitempty"`
	// NodeStates is each node's state: waiting, running, done, failed, skipped.
	// "skipped" is how a branch whose edge condition was not met is reported —
	// distinct from failed, since a failure branch that does not fire because
	// nothing failed is the pipeline working.
	NodeStates map[string]string `json:"node_states,omitempty"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
}

// TokenTelemetryRecord tracks token usage, dollar cost, and latency for financial telemetry.
type TokenTelemetryRecord struct {
	ID               string    `json:"id"`
	TaskID           string    `json:"task_id"`
	InstanceID       string    `json:"instance_id"`
	ArchetypeID      string    `json:"archetype_id,omitempty"`
	ProviderID       string    `json:"provider_id"`
	ModelName        string    `json:"model_name"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CachedTokens     int       `json:"cached_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	LatencyMS        int       `json:"latency_ms"`
	CreatedAt        time.Time `json:"created_at"`
}

// MemoryRecord represents a long-term cross-fleet semantic memory item.
type MemoryRecord struct {
	ID        string    `json:"id"`
	Namespace string    `json:"namespace"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	Embedding []float32 `json:"embedding,omitempty"`
	// EmbedModel names the model Embedding came from. Empty means the built-in
	// hashed bag-of-words fallback.
	//
	// It has to be recorded, not inferred: two models' vectors live in unrelated
	// spaces, so a cosine similarity between them is a number with no meaning.
	// Without this the index silently mixes schemes as soon as an operator
	// configures an embedding provider, and search gets worse than it was under
	// either scheme alone.
	EmbedModel       string `json:"embed_model,omitempty"`
	SourceTaskID     string `json:"source_task_id,omitempty"`
	SourceInstanceID string `json:"source_instance_id,omitempty"`
	// AboutUserID attributes a memory to a person rather than to the world, so
	// "prefers terse answers" is kept against whoever it is true of.
	AboutUserID string    `json:"about_user_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
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
	// AboutUser marks a remember action as a note about the person who asked
	// rather than about the machine or the task, so it comes back when that
	// person turns up and not when anyone does.
	AboutUser bool `json:"about_user,omitempty"`
	// MemoryScope is "bot" (default) or "fleet", for remember.
	//
	// Without it every memory went to the recording agent's own private
	// namespace and the shared namespaces were never written to at all — so the
	// fleet-wide episodic memory the docs describe held nothing, and a discovery
	// one agent made was unreachable by every other one.
	MemoryScope   string `json:"memory_scope,omitempty"`
	SecretKey     string `json:"secret_key,omitempty"`     // key for share_secret
	SecretVal     string `json:"secret_val,omitempty"`     // value for share_secret
	SessionDomain string `json:"session_domain,omitempty"` // domain for share_session
	// Shared work catalog. WorkName is what other agents refer to the item
	// by, so a second publish under the same name is an edit rather than a
	// duplicate; WorkKind is "file", "app" or "workspace".
	WorkName       string         `json:"work_name,omitempty"`
	WorkKind       string         `json:"work_kind,omitempty"`
	WorkWorkspace  string         `json:"work_workspace,omitempty"`
	SessionCookies string         `json:"session_cookies,omitempty"` // cookies JSON for share_session
	SnapshotName   string         `json:"snapshot_name,omitempty"`   // for snapshot action
	RollbackID     string         `json:"rollback_id,omitempty"`     // for rollback action
	MCPServerID    string         `json:"mcp_server_id,omitempty"`   // for call_mcp action
	MCPToolName    string         `json:"mcp_tool_name,omitempty"`   // for call_mcp action
	MCPParams      map[string]any `json:"mcp_params,omitempty"`      // for call_mcp action
	Amount         int            `json:"amount,omitempty"`          // scroll clicks / wait seconds
	Timeout        int            `json:"timeout,omitempty"`         // seconds, for wait_for
	Question       string         `json:"question,omitempty"`
	Summary        string         `json:"summary,omitempty"` // filled on done/fail
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
	ProviderOpenAI      ProviderKind = "openai"
	ProviderOllama      ProviderKind = "ollama"
	ProviderAnthropic   ProviderKind = "anthropic"
	ProviderGemini      ProviderKind = "gemini"
	ProviderAntigravity ProviderKind = "antigravity"       // Google Antigravity & Gemini Subscription Gateway
	ProviderCompatible  ProviderKind = "openai-compatible" // vLLM, LocalAI, LiteLLM, ...
)

// Provider is a configured model endpoint.
type Provider struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Kind      ProviderKind `json:"kind"`
	BaseURL   string       `json:"base_url,omitempty"`
	Model     string       `json:"model"`
	APIKeyRef string       `json:"api_key_ref,omitempty"` // vault key, never the secret
	// AuthMode is "api_key" or "oauth". OAuth exists for providers whose
	// entitlement belongs to an account rather than to a key.
	AuthMode string `json:"auth_mode,omitempty"`
	// OAuthClientID is not a secret; the client secret and refresh token live
	// in the vault under OAuthTokenRef.
	OAuthClientID string `json:"oauth_client_id,omitempty"`
	OAuthTokenRef string `json:"oauth_token_ref,omitempty"`
	// OAuth endpoints. Empty falls back to Google's, which is what the
	// google-flavoured provider kinds use.
	OAuthAuthURL   string `json:"oauth_auth_url,omitempty"`
	OAuthDeviceURL string `json:"oauth_device_url,omitempty"`
	OAuthTokenURL  string `json:"oauth_token_url,omitempty"`
	OAuthScope     string `json:"oauth_scope,omitempty"`
	// SignedIn is computed on read so the app can show sign-in state without
	// ever being handed the token.
	SignedIn    bool    `json:"signed_in,omitempty"`
	Vision      bool    `json:"vision"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
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
	// DisabledAt turns an account off without deleting it. Deleting cascades
	// a person's keys away and orphans what they made, which is the wrong
	// thing to do to someone who has simply left.
	DisabledAt time.Time `json:"disabled_at,omitzero"`
}

// Disabled reports whether this account has been turned off. A disabled user
// cannot sign in, and their API keys stop working with them.
func (u User) Disabled() bool { return !u.DisabledAt.IsZero() }

// APIKey is a long-lived credential for scripts and CI.
//
// The secret is shown once, at creation, and never stored — only a hash of it
// is. A key carries its owner's role, so revoking the user revokes the key.
type APIKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// UserID is whose authority the key acts with.
	UserID    string    `json:"user_id"`
	UserEmail string    `json:"user_email,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// LastUsedAt is zero for a key that has never been used, which is how a
	// key issued and forgotten is told apart from one in daily service.
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
	RevokedAt  time.Time `json:"revoked_at,omitzero"`
	// Secret is populated only in the response that creates the key.
	Secret string `json:"secret,omitempty"`
}

// Revoked reports whether this key has been turned off.
func (k APIKey) Revoked() bool { return !k.RevokedAt.IsZero() }

// Work item kinds. They differ in what the app does with the content, not in
// how it is stored.
const (
	// WorkFile is text: notes, code, data. Shown as text.
	WorkFile = "file"
	// WorkApp is a self-contained HTML document the app renders and runs.
	// Self-contained on purpose: it is rendered in an isolated web view with
	// no network of its own, so anything it needs has to be in the document.
	WorkApp = "app"
	// WorkWorkspace groups related items so several agents can work on one
	// thing without inventing a naming convention for it.
	WorkWorkspace = "workspace"
)

// WorkItem is something an agent published for the fleet, and for you.
//
// Agents could already message each other and share credentials; this is
// where the work itself goes. Without it, anything a bot produced lived in
// its own container and died with it, so a second bot asked to build on it
// had to be told what to rebuild rather than handed the thing.
type WorkItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
	MIME        string `json:"mime,omitempty"`

	// CreatedBy is an instance id for a bot, empty for the operator.
	CreatedBy     string `json:"created_by,omitempty"`
	CreatedByName string `json:"created_by_name,omitempty"`

	// OrgID scopes it to a department. Empty is admin-only, matching how an
	// unfiled secret behaves.
	OrgID string `json:"org_id,omitempty"`

	// ParentID puts this item inside a workspace.
	ParentID string `json:"parent_id,omitempty"`

	// Version is bumped on every write. Two agents editing one item is the
	// normal case in a catalog like this, so a writer can tell whether what
	// it read is still what is there.
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValidWorkKind reports whether a kind is one this system knows. An unknown
// kind stored today is a kind that might mean something tomorrow.
func ValidWorkKind(kind string) bool {
	switch kind {
	case WorkFile, WorkApp, WorkWorkspace:
		return true
	}
	return false
}

// ParamHandoff marks a task that one agent handed to another.
//
// Such a task carries concrete instructions from a colleague, so stopping to
// ask a person is almost never the right move: nobody is waiting to answer,
// and each wait holds up everyone downstream.
const ParamHandoff = "handoff"
