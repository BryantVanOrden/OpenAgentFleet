// Thin typed client for the orchestrator API. One place that knows about the
// token, so every other module can just await a call.

export type Tier = "micro" | "standard" | "power-user" | "developer-heavy";
export type InstanceState = "provisioning" | "running" | "paused" | "stopped" | "error";
export type TaskState =
  | "queued"
  | "running"
  | "awaiting_human"
  | "succeeded"
  | "failed"
  | "cancelled";

export interface TierProfile {
  name: Tier;
  description: string;
  driver: string;
  image: string;
  vcpu: number;
  memory_mb: number;
  disk_gb: number;
  gpu: boolean;
  shm_mb: number;
}

/** A portable archetype package. Mirrors the SDK's dataclass. */
export interface ArchetypeManifest {
  version: string;
  id: string;
  name: string;
  tagline: string;
  category: string;
  icon?: string;
  recommended_tier: string;
  vcpu: number;
  memory_mb: number;
  disk_gb: number;
  gpu: boolean;
  preinstalled_tools: string[];
  preinstalled_repos?: string[];
  system_prompt: string;
  default_voice?: string;
  default_shell_access?: boolean;
  default_environment: Record<string, string>;
  /** Credentials are never included; only the key names that must be supplied. */
  mcp_servers: {
    name: string;
    transport: string;
    command?: string;
    args?: string[];
    url?: string;
    env_keys?: string[];
  }[];
  recorded_skills: { id: string; name: string; description?: string }[];
}

/** What an import actually did, itemised. */
export interface ImportArchetypeResult {
  archetype: string;
  skills_created: string[];
  skills_skipped: string[];
  mcp_registered: string[];
  mcp_failed: string[];
  needs_secrets?: string[];
  instance_id?: string;
  instance_name?: string;
  instance_status?: string;
}

export interface BotTemplate {
  id: string;
  name: string;
  tagline: string;
  category: string;
  icon: string;
  recommended_tier: Tier;
  vcpu: number;
  memory_mb: number;
  disk_gb: number;
  gpu: boolean;
  preinstalled_tools: string[];
  preinstalled_repos?: string[];
  specialized_prompt: string;
  default_environment?: Record<string, string>;
}

export interface EgressPolicy {
  allow?: string[];
  deny?: string[];
  block_local: boolean;
}

export interface Instance {
  id: string;
  name: string;
  archetype_id?: string;
  system_prompt?: string;
  preinstalled_tools?: string[];
  tier: Tier;
  driver: string;
  state: InstanceState;
  profile: TierProfile;
  vnc_url: string;
  egress: EgressPolicy;
  shell_access: boolean;
  sudo_access?: boolean;
  /** This bot's own model fallback chain, most preferred first. Empty means
   *  the fleet-wide order. Entries are provider ids or combo ids. */
  provider_ids?: string[];
  /** Voice this agent speaks in. Empty uses the app-wide default. */
  voice?: string;
  /** Speaking rate multiplier. 0 means the app-wide default. */
  voice_speed?: number;
  /** The departments this bot belongs to. Empty means unassigned — visible
   *  only to a deployment administrator. */
  org_ids?: string[];
  last_error?: string;
  created_at: string;
}

export interface InstanceStats {
  instance_id: string;
  cpu_percent: number;
  memory_bytes: number;
  memory_limit: number;
  rx_bytes: number;
  tx_bytes: number;
  sampled_at: string;
}

export interface Task {
  id: string;
  instance_id: string;
  goal: string;
  skill_id?: string;
  parent_task_id?: string;
  auto_refine?: boolean;
  state: TaskState;
  step: number;
  max_steps: number;
  error?: string;
  result?: string;
  created_at: string;
}

export interface AgentAction {
  thought?: string;
  action: string;
  target?: string;
  mark?: number;
  coordinates?: number[];
  text?: string;
  code?: string;
  query?: string;
  sub_goal?: string;
  sub_skill_id?: string;
  wait_child?: boolean;
  tool_name?: string;
  tool_description?: string;
  tool_parameters?: Record<string, unknown>;
  tool_handler?: string;
  key?: string;
  question?: string;
  summary?: string;
}

export interface StepRecord {
  id: string;
  task_id: string;
  step: number;
  action: AgentAction;
  observation_key?: string;
  outcome: string;
  duration_ms: number;
  prompt_tokens: number;
  output_tokens: number;
  at: string;
}

export interface SkillStep {
  index: number;
  kind: string;
  window?: string;
  role?: string;
  label?: string;
  coordinates?: number[];
  text?: string;
  key?: string;
  param?: string;
  assert?: string;
  meta?: Record<string, string>;
}

export interface Skill {
  id: string;
  name: string;
  description: string;
  params?: string[];
  steps: SkillStep[];
  markdown: string;
  version?: number;
  refinement_notes?: string;
  created_at: string;
  updated_at: string;
}

export interface Provider {
  id: string;
  name: string;
  kind: "openai" | "ollama" | "anthropic" | "gemini" | "antigravity" | "openai-compatible";
  base_url?: string;
  model: string;
  api_key_ref?: string;
  vision: boolean;
  temperature: number;
  max_tokens: number;
  priority: number;
  enabled: boolean;
  /** 'api_key' or 'oauth'. */
  auth_mode?: string;
  oauth_client_id?: string;
  /** Whether an account sign-in is stored. The token itself never leaves the
   *  server, so this is all the client is told. */
  signed_in?: boolean;
}

/** Engines that can be signed into with a Google account instead of a key. */
export const OAUTH_PROVIDER_KINDS: ReadonlySet<string> = new Set(["gemini", "antigravity"]);

export interface AntigravityModelInfo {
  id: string;
  name: string;
  speed: string;
  thinking_level: string;
  vision: boolean;
  description: string;
}

/** One model as reported by provider discovery. */
export interface ModelDescriptor {
  id: string;
  name: string;
  speed?: string;
  thinking?: string;
  vision: boolean;
  description?: string;
}

export interface Alert {
  id: string;
  kind: string;
  severity: "info" | "warn" | "critical";
  instance_id?: string;
  task_id?: string;
  title: string;
  body: string;
  screenshot_id?: string;
  needs_reply: boolean;
  reply?: string;
  resolved_at?: string | null;
  created_at: string;
}

export interface User {
  id: string;
  email: string;
  role: "admin" | "operator" | "auditor" | "viewer";
  created_at?: string;
  /** Set when the account has been turned off. Disabled rather than deleted,
   *  so what they did stays traceable and their keys die with them. */
  disabled_at?: string;
}

/** An organisation or department: who can see and drive which bots, set once. */
export interface Org {
  id: string;
  name: string;
  description?: string;
  created_at?: string;
  member_count: number;
  bot_count: number;
}

/** One person's standing in one department. */
export interface OrgMember {
  org_id?: string;
  user_id: string;
  email?: string;
  org_role: string;
}

/** Org roles, most to least capable. */
export const ORG_ROLES = ["owner", "admin", "member", "viewer"] as const;

/** What each role can do, in the terms an administrator thinks in. */
export const ORG_ROLE_SUMMARY: Record<string, string> = {
  owner: "Everything, including who else has access",
  admin: "Create, edit and delete bots; use shared secrets",
  member: "Talk to bots and use their desktops",
  viewer: "Read only — cannot make anything happen",
};

/** A per-bot exception to what someone may do. */
export interface BotGrant {
  user_id: string;
  instance_id?: string;
  permissions: string[];
}

/** The grant permissions that mean anything for a single bot, least to most
 *  dangerous. */
export const GRANT_PERMS_PER_BOT = ["view", "read", "chat", "desktop", "edit", "delete"] as const;

export const GRANT_PERM_LABELS: Record<string, string> = {
  view: "See the bot exists",
  read: "Read its replies and history",
  chat: "Talk to it",
  desktop: "Use its desktop",
  edit: "Change its settings",
  delete: "Delete bots",
};

/** A long-lived access key for scripts and CI. */
export interface ApiKeyRecord {
  id: string;
  name: string;
  user_email?: string;
  created_at: string;
  /** Absent for a key that has never been used. */
  last_used_at?: string;
  revoked_at?: string;
  /** Only ever set on the response that creates the key. There is no second
   *  copy to fetch later. */
  secret?: string;
}

/** Live resource usage of the machine running the orchestrator, from
 *  GET /api/telemetry/host. Distinct from InstanceStats, which is per sandbox. */
export interface HostStats {
  cpu_percent: number;
  cpu_cores: number;
  load1: number;
  memory_used_bytes: number;
  memory_total_bytes: number;
  swap_used_bytes: number;
  swap_total_bytes: number;
  disk_used_bytes: number;
  disk_total_bytes: number;
  uptime_sec: number;
  /** Null on a host with no GPU, or where the orchestrator cannot read one;
   *  gpu_message then says which. */
  gpu?: GpuStats | null;
  gpu_message?: string;
}

export interface GpuStats {
  name: string;
  memory_used_bytes: number;
  memory_total_bytes: number;
  utilisation_percent: number;
  temperature_c: number;
}

const TOKEN_KEY = "agentfleet.token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_KEY, token);
  else localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = getToken();
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");

  const res = await fetch(path, { ...init, headers });

  if (res.status === 401) {
    setToken(null);
    // Let the router re-render into the login screen rather than throwing the
    // user into a blank page with a console error.
    window.dispatchEvent(new CustomEvent("agentfleet:unauthorized"));
    throw new ApiError("Session expired", 401);
  }
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(message, res.status);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

const get = <T,>(path: string) => request<T>(path);
const post = <T,>(path: string, body?: unknown) =>
  request<T>(path, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) });
const put = <T,>(path: string, body?: unknown) =>
  request<T>(path, { method: "PUT", body: body === undefined ? undefined : JSON.stringify(body) });
const patch = <T,>(path: string, body?: unknown) =>
  request<T>(path, { method: "PATCH", body: body === undefined ? undefined : JSON.stringify(body) });
const del = <T,>(path: string) => request<T>(path, { method: "DELETE" });

export interface SwarmMember {
  instance_id: string;
  role: string;
  /**
   * Filled in by the server from the instance itself, so these are optional on
   * create. The fleet's own name for a bot wins over whatever the caller typed —
   * two names for one bot makes a blackboard unreadable.
   */
  instance_name?: string;
  archetype_id?: string;
  status?: string;
}

export interface SwarmMessage {
  id: string;
  swarm_id: string;
  from_bot: string;
  to_bot: string;
  phase: string;
  content: string;
  artifacts?: string[];
  created_at: string;
}

export interface SwarmArtifact {
  id: string;
  swarm_id: string;
  title: string;
  author: string;
  category: string;
  content: string;
  approved_by?: string[];
  created_at: string;
}

export interface SwarmTeam {
  id: string;
  name: string;
  mission: string;
  status: "initializing" | "running" | "reviewing" | "completed" | "failed";
  members: SwarmMember[];
  messages: SwarmMessage[];
  artifacts: SwarmArtifact[];
  created_at: string;
  updated_at: string;
}

/** Which sender a webhook is for; selects the signature scheme. */
export type WebhookKind = "generic" | "github" | "stripe" | "crm";

export const WEBHOOK_KINDS: { value: WebhookKind; label: string; hint: string }[] = [
  {
    value: "generic",
    label: "Generic",
    hint: "HMAC-SHA256 of the body in X-Hub-Signature-256 or X-OpenAgentFleet-Signature.",
  },
  {
    value: "github",
    label: "GitHub",
    hint: "Paste the secret into the repository's webhook settings. Events are summarised.",
  },
  {
    value: "stripe",
    label: "Stripe",
    hint: "Use the whsec_… signing secret from the Stripe dashboard, not your API key.",
  },
  {
    value: "crm",
    label: "CRM / form",
    hint: "A bare JSON document. Contact and deal fields are extracted where present.",
  },
];

export interface WebhookRecord {
  id: string;
  token: string;
  name: string;
  target_instance_id?: string;
  target_archetype: string;
  goal_template: string;
  kind?: WebhookKind;
  /**
   * Never returned by the API — the listing projection drops it, because a
   * shared signing key is a credential. Present here because it is accepted on
   * create.
   */
  secret?: string;
  has_secret?: boolean;
  active: boolean;
  last_triggered_at?: string;
  created_at: string;
}

export interface CronTriggerRecord {
  id: string;
  name: string;
  schedule_cron: string;
  target_archetype: string;
  goal_template: string;
  active: boolean;
  last_run_at?: string;
  created_at: string;
}

/**
 * A shared fleet secret as the API returns it — metadata only.
 *
 * There is no `value` field, and that is deliberate rather than an oversight:
 * the list endpoint is gated at `roleAny`, which includes the read-only auditor
 * role, so returning the value handed every fleet credential to anyone who
 * could sign in. Use `has_value` to show whether one is set. Consumers that
 * genuinely need a value resolve it server-side; it never crosses the wire.
 */
export interface SharedSecret {
  key: string;
  scope: string;
  note?: string;
  created_by?: string;
  updated_at: string;
  has_value: boolean;
}

export interface SharedSession {
  id: string;
  domain: string;
  title: string;
  cookies_json: string;
  local_storage_json?: string;
  created_by_instance?: string;
  created_at: string;
}

export interface PeerMessage {
  id: string;
  from_instance_id: string;
  from_instance_name: string;
  to_instance_id: string;
  kind: string;
  content: string;
  data?: Record<string, unknown>;
  /** The thread this message belongs to. */
  conversation_id?: string;
  created_at: string;
}

/** For a summary message, how many messages it stands in for. */
export function compactedCount(m: PeerMessage): number {
  const n = m.data?.["compacted_messages"];
  return typeof n === "number" ? n : 0;
}

/** How you are identified in a conversation member list. */
export const OPERATOR_ID = "operator";

/** The always-present channel every agent hears. */
export const BROADCAST_ID = "broadcast";

/**
 * A thread in fleet comms: you and a bot, two bots, or a group.
 *
 * Threads are created and deleted deliberately rather than inferred from who
 * happened to message whom, so you can put two agents in a room before they
 * have anything to say to each other.
 */
export interface Conversation {
  id: string;
  /** 'direct', 'pair', 'group' or 'broadcast'. */
  kind: string;
  title: string;
  members: string[];
  message_count: number;
  last_message_at?: string;
  /** Absent only for the built-in channel, which is implicit and has no row
   *  until it is renamed or pinned. */
  created_at?: string;
  pinned: boolean;
}

export const isBroadcastConversation = (c: Conversation) => c.id === BROADCAST_ID;

/** An everyone-channel: the whole fleet hears it. True for the built-in
 *  channel and for any other opened since. */
export const isEveryoneConversation = (c: Conversation) =>
  isBroadcastConversation(c) || c.kind === "broadcast";

export const isPairConversation = (c: Conversation) => c.kind === "pair";

/**
 * How recently a thread was used, for ordering. A thread that has just been
 * made has no last message; falling back to when it was opened puts it at the
 * top of its group instead of the bottom.
 */
export function conversationLastUsed(c: Conversation): number {
  const at = c.last_message_at || c.created_at;
  return at ? new Date(at).getTime() : 0;
}

/**
 * Identifies the set of people a thread is between, so several threads with
 * the same participants collapse to one row in the comms list. Every
 * everyone-channel groups together — they are between the same people.
 */
export function conversationKey(c: Conversation): string {
  if (isEveryoneConversation(c) || c.members.length === 0) return "everyone";
  return [...c.members].sort().join("|");
}

export interface MCPServer {
  id: string;
  name: string;
  transport: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  tools_count: number;
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface MCPTool {
  server_id: string;
  name: string;
  description: string;
  input_schema?: Record<string, unknown>;
}

export interface PipelineNode {
  id: string;
  name: string;
  /** Pins the node to one bot. Empty falls back to archetype_id. */
  instance_id?: string;
  archetype_id?: string;
  goal_template: string;
  params?: Record<string, string>;
}

/**
 * An edge is a dependency, optionally conditional.
 *
 * The condition vocabulary is fixed and validated server-side at save:
 * always | success | failure | contains:TEXT | not_contains:TEXT |
 * equals:TEXT | matches:REGEX. An empty condition is a plain dependency.
 */
export interface PipelineEdge {
  from_node_id: string;
  to_node_id: string;
  condition?: string;
}

export const EDGE_CONDITIONS = [
  { value: "", label: "always (plain dependency)" },
  { value: "success", label: "only if it succeeded" },
  { value: "failure", label: "only if it failed" },
  { value: "contains:", label: "only if the result contains…" },
  { value: "not_contains:", label: "only if the result does not contain…" },
  { value: "equals:", label: "only if the result is exactly…" },
  { value: "matches:", label: "only if the result matches the regex…" },
] as const;

export interface WorkflowPipeline {
  id: string;
  name: string;
  description?: string;
  nodes: PipelineNode[];
  edges: PipelineEdge[];
  /** Bounds concurrent nodes. 0 or absent means the engine default (4). */
  max_parallel?: number;
  created_at: string;
  updated_at: string;
}

export type PipelineNodeState = "waiting" | "running" | "done" | "failed" | "skipped";

export interface PipelineRun {
  id: string;
  pipeline_id: string;
  status: "running" | "completed" | "failed" | "cancelled";
  /**
   * The most recently started node. It cannot describe several nodes running at
   * once, which it now regularly does; node_states is the accurate answer.
   */
  current_node_id?: string;
  node_results?: Record<string, string>;
  /** Per-node state. "skipped" means an edge condition was not met. */
  node_states?: Record<string, PipelineNodeState>;
  started_at: string;
  finished_at?: string;
}

export interface FinancialSummary {
  total_prompt_tokens: number;
  total_completion_tokens: number;
  total_cached_tokens: number;
  total_cost_usd: number;
  avg_latency_ms: number;
  turns_count: number;
}

export interface TokenTelemetryRecord {
  id: string;
  task_id: string;
  instance_id: string;
  archetype_id?: string;
  provider_id: string;
  model_name: string;
  prompt_tokens: number;
  completion_tokens: number;
  cached_tokens: number;
  cost_usd: number;
  latency_ms: number;
  created_at: string;
}

/** Something an agent published for the others: a file, a runnable mini-app,
 *  or a workspace folder grouping them. */
export interface WorkItem {
  id: string;
  name: string;
  /** 'file' (text), 'app' (self-contained HTML) or 'workspace' (folder). */
  kind: "file" | "app" | "workspace";
  description?: string;
  content?: string;
  mime?: string;
  created_by_name?: string;
  /** Set when this item belongs inside a workspace. Empty is the top level. */
  parent_id?: string;
  /** Bumped on every write, so an edit reads as a version, not a second copy. */
  version: number;
  updated_at: string;
}

export interface ChatSession {
  id: string;
  title: string;
  pinned: boolean;
  message_count: number;
  last_message_at?: string;
  /** Absent for the implicit default chat, which is not a real row. */
  created_at?: string;
}

/** The chat holding messages from before chats could be separated. It is not
 *  a real row, so it cannot be renamed or pinned — only cleared. */
export const DEFAULT_CHAT_ID = "default";

export interface ChatMessage {
  id: string;
  role: string;
  body: string;
  /** "message" for ordinary talk, "plan" for a proposal awaiting approval.
   *  Older rows predate the column and come back absent. */
  kind?: string;
  /** "" while a plan is still open, then "approved" or "discarded". */
  plan_state?: string;
  created_at: string;
}

/** A named assignment of providers to roles (vision, reasoning, chat, …).
 *  Usable in a bot's model chain wherever a single provider is. */
export interface ModelCombo {
  id: string;
  name: string;
  description?: string;
  /** role -> provider id */
  roles: Record<string, string>;
}

export const COMBO_ROLE_SHORT: Record<string, string> = {
  vision: "Hands",
  reasoning: "Brain",
  chat: "Chat",
  summarize: "Summarise",
  refine: "Refine",
};

/** Combo roles in the order a picker should offer them. The first two are the
 *  simple pair; the rest only appear in advanced mode. */
export const COMBO_ROLES = ["vision", "reasoning", "chat", "summarize", "refine"] as const;

export const COMBO_SIMPLE_ROLES = ["vision", "reasoning"] as const;

/** What each role is for, in the operator's terms rather than the code's. */
export const COMBO_ROLE_LABELS: Record<string, string> = {
  vision: "Hands — sees the screen and clicks",
  reasoning: "Brain — plans, deduces, writes code",
  chat: "Chat — talks to you",
  summarize: "Summarise — compacts long threads",
  refine: "Refine — hardens recorded skills",
};

/** A brain-and-hands pair rather than a full assignment. */
export function isSimpleCombo(c: ModelCombo): boolean {
  const keys = Object.keys(c.roles);
  return keys.length <= 2 && keys.every((r) => r === "vision" || r === "reasoning");
}

/** Something a bot decided was worth keeping across tasks. */
export interface BotMemory {
  id: string;
  title: string;
  content: string;
  tags?: string[];
  source_task_id?: string;
  created_at: string;
}

export const api = {
  // Authentication
  login: (email: string, password: string) =>
    post<{ token: string; user: User }>("/api/auth/login", { email, password }),
  bootstrap: (email: string, password: string) =>
    post<{ token: string; user: User }>("/api/auth/bootstrap", { email, password }),
  me: () => get<User>("/api/me"),

  tiers: () => get<TierProfile[]>("/api/tiers"),
  templates: () => get<BotTemplate[]>("/api/templates"),
  template: (id: string) => get<BotTemplate>(`/api/templates/${id}`),

  /**
   * A portable archetype package: the persona, the hardware profile, the
   * fleet's recorded skills and its MCP registrations.
   *
   * Credentials are deliberately excluded — a manifest is a file people mail
   * each other, so the MCP env map comes across as key names only.
   */
  exportArchetype: (id: string, skills?: string[]) =>
    get<ArchetypeManifest>(
      `/api/archetypes/${id}/export${skills?.length ? `?skills=${skills.join(",")}` : ""}`,
    ),
  importArchetype: (body: {
    manifest: ArchetypeManifest;
    overwrite?: boolean;
    create_instance?: boolean;
    instance_name?: string;
  }) => post<ImportArchetypeResult>("/api/archetypes/import", body),
  instances: () => get<Instance[]>("/api/instances"),
  instance: (id: string) => get<Instance>(`/api/instances/${id}`),
  createInstance: (body: {
    name: string;
    archetype_id?: string;
    system_prompt?: string;
    preinstalled_tools?: string[];
    tier: Tier;
    override?: Record<string, unknown>;
    egress: EgressPolicy;
    shell_access: boolean;
  }) => post<Instance>("/api/instances", body),
  instanceAction: (id: string, action: "start" | "stop" | "pause" | "resume") =>
    post<Instance>(`/api/instances/${id}/${action}`),
  deleteInstance: (id: string) => del<void>(`/api/instances/${id}`),
  stats: (id: string) => get<InstanceStats>(`/api/instances/${id}/stats`),
  observe: (id: string) =>
    get<{ screenshot_b64: string; active_window: string; width: number; height: number }>(
      `/api/instances/${id}/observe?a11y=false`,
    ),

  startRecording: (id: string, name: string) =>
    post<{ status: string }>(`/api/instances/${id}/record/start`, { name }),
  stopRecording: (id: string) => post<Skill>(`/api/instances/${id}/record/stop`),

  skills: () => get<Skill[]>("/api/skills"),
  skill: (id: string) => get<Skill>(`/api/skills/${id}`),
  saveSkill: (skill: Partial<Skill>) =>
    skill.id ? put<Skill>(`/api/skills/${skill.id}`, skill) : post<Skill>("/api/skills", skill),
  deleteSkill: (id: string) => del<void>(`/api/skills/${id}`),
  refineSkill: (id: string, taskId?: string) =>
    post<Skill>(`/api/skills/${id}/refine`, { task_id: taskId }),

  tasks: (instanceId?: string) =>
    get<Task[]>(`/api/tasks${instanceId ? `?instance_id=${instanceId}` : ""}`),
  task: (id: string) => get<{ task: Task; live: boolean }>(`/api/tasks/${id}`),
  taskSteps: (id: string) => get<StepRecord[]>(`/api/tasks/${id}/steps`),
  createTask: (body: {
    instance_id: string;
    goal: string;
    skill_id?: string;
    params?: Record<string, string>;
    parent_task_id?: string;
    auto_refine?: boolean;
    provider_id?: string;
    max_steps?: number;
  }) => post<Task>("/api/tasks", body),
  cancelTask: (id: string) => post<void>(`/api/tasks/${id}/cancel`),
  synthesizeSkill: (taskId: string) => post<Skill>(`/api/tasks/${taskId}/synthesize-skill`),

  // Multi-Agent Swarms & Mission Control
  swarms: () => get<SwarmTeam[]>("/api/swarms"),
  swarm: (id: string) => get<SwarmTeam>(`/api/swarms/${id}`),
  createSwarm: (body: { name: string; mission: string; members?: SwarmMember[] }) =>
    post<SwarmTeam>("/api/swarms", body),
  postSwarmMessage: (
    id: string,
    body: { from_bot: string; to_bot: string; phase: string; content: string; artifacts?: string[] },
  ) => post<SwarmMessage>(`/api/swarms/${id}/messages`, body),
  deleteSwarm: (id: string) => del<void>(`/api/swarms/${id}`),
  /** Publishing sends the artifact out for review by every other member. */
  publishSwarmArtifact: (
    id: string,
    body: { title: string; author: string; category?: string; content: string },
  ) => post<SwarmArtifact>(`/api/swarms/${id}/artifacts`, body),
  reviewSwarmArtifact: (
    id: string,
    artifactId: string,
    body: { reviewer: string; approved: boolean; notes?: string },
  ) => post<SwarmArtifact>(`/api/swarms/${id}/artifacts/${artifactId}/review`, body),

  // Shared Fleet Vault & Inter-Agent Comms
  sharedSecrets: () => get<SharedSecret[]>("/api/vault/secrets"),
  putSharedSecret: (body: { key: string; value: string; scope?: string; note?: string }) =>
    post<SharedSecret>("/api/vault/secrets", body),
  deleteSharedSecret: (key: string) => del<void>(`/api/vault/secrets/${encodeURIComponent(key)}`),
  sharedSessions: (domain?: string) =>
    get<SharedSession[]>(`/api/vault/sessions${domain ? `?domain=${encodeURIComponent(domain)}` : ""}`),
  saveSharedSession: (body: {
    domain: string;
    title: string;
    cookies_json: string;
    local_storage_json?: string;
    created_by?: string;
  }) => post<SharedSession>("/api/vault/sessions", body),
  peerMessages: (instanceId?: string) =>
    get<PeerMessage[]>(`/api/vault/comms${instanceId ? `?instance_id=${instanceId}` : ""}`),
  sendPeerMessage: (body: {
    from_instance_id?: string;
    from_instance_name?: string;
    to_instance_id?: string;
    kind?: string;
    content: string;
    data?: Record<string, unknown>;
    /** Files the message into a specific thread. */
    conversation_id?: string;
  }) => post<PeerMessage>("/api/vault/comms", body),

  // Conversations: the named threads comms messages are filed into.
  conversations: () => get<Conversation[]>("/api/comms/conversations"),
  /**
   * Open a thread. Include OPERATOR_ID among the members to be in it yourself;
   * leave it out to put two bots together and watch. Pass kind "broadcast" for
   * another everyone-channel — those are heard by the whole fleet, including
   * bots added after the thread was opened, so they need no member list.
   */
  createConversation: (body: { title?: string; members: string[]; kind?: string }) =>
    post<Conversation>("/api/comms/conversations", {
      title: body.title ?? "",
      members: body.members,
      ...(body.kind ? { kind: body.kind } : {}),
    }),
  /** Rename or pin a thread. Independent fields, so pinning keeps the name. */
  updateConversation: (id: string, body: { title?: string; pinned?: boolean }) =>
    patch<Conversation>(`/api/comms/conversations/${id}`, body),
  deleteConversation: (id: string) => del<void>(`/api/comms/conversations/${id}`),
  conversationMessages: (id: string) =>
    get<PeerMessage[]>(`/api/comms/conversations/${id}/messages`),
  /** Fold a thread's history into a single summary message. The originals stay
   *  on the server; this changes what is replayed, not what happened. */
  compactConversation: (id: string) =>
    post<PeerMessage>(`/api/comms/conversations/${id}/compact`),

  // Model Context Protocol (MCP) Bridge
  mcpServers: () => get<MCPServer[]>("/api/mcp/servers"),
  registerMCPServer: (body: Partial<MCPServer>) => post<MCPServer>("/api/mcp/servers", body),
  deleteMCPServer: (id: string) => del<void>(`/api/mcp/servers/${id}`),
  mcpTools: (serverId?: string) =>
    get<MCPTool[]>(`/api/mcp/tools${serverId ? `?server_id=${serverId}` : ""}`),
  callMCPTool: (body: { server_id: string; tool_name: string; params: Record<string, unknown> }) =>
    post<unknown>("/api/mcp/call", body),

  // Workflow DAG Pipelines
  pipelines: () => get<WorkflowPipeline[]>("/api/pipelines"),
  savePipeline: (body: Partial<WorkflowPipeline>) => post<WorkflowPipeline>("/api/pipelines", body),
  pipeline: (id: string) => get<WorkflowPipeline>(`/api/pipelines/${id}`),
  deletePipeline: (id: string) => del<void>(`/api/pipelines/${id}`),
  runPipeline: (id: string) => post<PipelineRun>(`/api/pipelines/${id}/run`),
  pipelineRuns: (id: string) => get<PipelineRun[]>(`/api/pipelines/${id}/runs`),

  // Financial Telemetry
  financialSummary: () => get<FinancialSummary>("/api/telemetry/financials"),
  telemetryRecords: (limit = 50) =>
    get<TokenTelemetryRecord[]>(`/api/telemetry/records?limit=${limit}`),

  // Webhooks & Triggers
  webhooks: () => get<WebhookRecord[]>("/api/webhooks"),
  createWebhook: (body: Partial<WebhookRecord>) => post<WebhookRecord>("/api/webhooks", body),
  deleteWebhook: (id: string) => del<void>(`/api/webhooks/${id}`),
  cronTriggers: () => get<CronTriggerRecord[]>("/api/triggers/cron"),
  createCronTrigger: (body: Partial<CronTriggerRecord>) =>
    post<CronTriggerRecord>("/api/triggers/cron", body),
  deleteCronTrigger: (id: string) => del<void>(`/api/triggers/cron/${id}`),

  alerts: (openOnly = false) => get<Alert[]>(`/api/alerts?open=${openOnly}`),
  replyAlert: (id: string, reply: string) => post<void>(`/api/alerts/${id}/reply`, { reply }),

  providers: () => get<Provider[]>("/api/providers"),
  saveProvider: (p: Partial<Provider> & { api_key?: string }) =>
    p.id ? put<Provider>(`/api/providers/${p.id}`, p) : post<Provider>("/api/providers", p),
  deleteProvider: (id: string) => del<void>(`/api/providers/${id}`),
  reorderProviders: (ids: string[]) => post<Provider[]>("/api/providers/reorder", { ids }),
  probeProvider: (id: string) => post<{ ok: boolean; error?: string }>(`/api/providers/${id}/probe`),
  /**
   * Discover the models a provider actually serves, for any provider kind.
   *
   * `live` distinguishes a real answer from the built-in catalogue. The
   * catalogue is still returned on failure — an empty dropdown is worse than a
   * stale one — but the caller must say which it is showing, or an operator
   * picks a model their provider does not serve and only finds out mid-run.
   */
  dynamicModels: (kind: string, baseUrl?: string, apiKey?: string) =>
    get<{
      models: ModelDescriptor[];
      live: boolean;
      reason?: string;
      error?: string;
    }>(
      `/api/providers/models?${new URLSearchParams({
        kind,
        ...(baseUrl ? { base_url: baseUrl } : {}),
        ...(apiKey ? { api_key: apiKey } : {}),
      }).toString()}`,
    ),

  ollamaModels: (baseUrl?: string) =>
    get<{ models: string[]; error?: string }>(
      `/api/providers/ollama/models${baseUrl ? `?base_url=${encodeURIComponent(baseUrl)}` : ""}`,
    ),
  antigravityModels: (baseUrl?: string, apiKey?: string) =>
    get<{ models: AntigravityModelInfo[]; error?: string }>(
      `/api/providers/antigravity/models?${new URLSearchParams({
        ...(baseUrl ? { base_url: baseUrl } : {}),
        ...(apiKey ? { api_key: apiKey } : {}),
      }).toString()}`,
    ),

  secrets: () => get<{ ref: string; note: string; updated_at: string }[]>("/api/secrets"),
  putSecret: (ref: string, value: string, note: string) =>
    put<void>(`/api/secrets/${encodeURIComponent(ref)}`, { value, note }),
  deleteSecret: (ref: string) => del<void>(`/api/secrets/${encodeURIComponent(ref)}`),

  users: () => get<User[]>("/api/users"),
  createUser: (email: string, password: string, role: string) =>
    post<User>("/api/users", { email, password, role }),
  setRole: (id: string, role: string) => put<void>(`/api/users/${id}/role`, { role }),
  /** Reset someone's password. On a deployment with no mail server this is the
   *  only way back in for an account that is locked out. */
  setUserPassword: (id: string, password: string) =>
    put<void>(`/api/users/${id}/password`, { password }),
  /** Turn an account on or off. Disabling also stops every key it holds. */
  setUserDisabled: (id: string, disabled: boolean) =>
    put<void>(`/api/users/${id}/disabled`, { disabled }),

  // Organisations and departments.
  orgs: () => get<Org[]>("/api/orgs"),
  saveOrg: (body: { id?: string; name: string; description?: string }) =>
    body.id
      ? put<Org>(`/api/orgs/${body.id}`, { name: body.name, description: body.description ?? "" })
      : post<Org>("/api/orgs", { name: body.name, description: body.description ?? "" }),
  deleteOrg: (id: string) => del<void>(`/api/orgs/${id}`),
  orgMembers: (orgId: string) => get<OrgMember[]>(`/api/orgs/${orgId}/members`),
  setOrgMember: (orgId: string, userId: string, role: string) =>
    post<void>(`/api/orgs/${orgId}/members`, { user_id: userId, org_role: role }),
  removeOrgMember: (orgId: string, userId: string) =>
    del<void>(`/api/orgs/${orgId}/members/${userId}`),
  /**
   * Set which departments a bot belongs to. The whole set at once rather than
   * add/remove: two administrators editing at the same time should disagree
   * about the result, not silently compose into a third set neither chose.
   */
  setInstanceOrgs: (instanceId: string, orgIds: string[]) =>
    put<Instance>(`/api/instances/${instanceId}/org`, { org_ids: orgIds }),

  // Per-bot access grants — the exception layer over department roles.
  botGrants: (instanceId: string) => get<BotGrant[]>(`/api/instances/${instanceId}/grants`),
  /** A null permissions list removes the grant so the org default applies
   *  again; an empty list explicitly allows nothing, which is how one bot is
   *  hidden from someone who can see the rest of their department. */
  setBotGrant: (instanceId: string, userId: string, permissions: string[] | null) =>
    put<void>(`/api/instances/${instanceId}/grants`, {
      user_id: userId,
      permissions,
    }),

  // Long-lived access keys for scripts and CI.
  apiKeys: () => get<ApiKeyRecord[]>("/api/api-keys"),
  /** Issue a key. The returned secret is the only copy that will ever exist. */
  createApiKey: (name: string) => post<ApiKeyRecord>("/api/api-keys", { name }),
  revokeApiKey: (id: string) => del<void>(`/api/api-keys/${id}`),

  // Host telemetry: the machine running the orchestrator itself.
  hostStats: () => get<HostStats>("/api/telemetry/host"),

  chat: (instanceId: string, chatId?: string) =>
    get<ChatMessage[]>(
      `/api/chat/${instanceId}${chatId ? `?chat_id=${encodeURIComponent(chatId)}` : ""}`,
    ),
  /**
   * Talk to an agent.
   *
   * mode is "chat" (talk, no actions), "plan" (propose, still no actions) or
   * "task" (start work). Chat is the default deliberately: asking how a run is
   * going must never start one.
   */
  sendChat: (instanceId: string, body: string, mode: "chat" | "plan" | "task", chatId?: string) =>
    post<unknown>(`/api/chat/${instanceId}`, {
      body,
      mode,
      ...(chatId ? { chat_id: chatId } : {}),
    }),
  chatSessions: (instanceId: string) => get<ChatSession[]>(`/api/chat/${instanceId}/chats`),
  createChatSession: (instanceId: string, title = "") =>
    post<ChatSession>(`/api/chat/${instanceId}/chats`, { title }),
  /** Rename or pin a chat. Both optional and independent, so pinning does not
   *  clear the name. */
  updateChatSession: (
    instanceId: string,
    chatId: string,
    body: { title?: string; pinned?: boolean },
  ) => patch<ChatSession>(`/api/chat/${instanceId}/chats/${chatId}`, body),
  deleteChatSession: (instanceId: string, chatId: string) =>
    del<void>(`/api/chat/${instanceId}/chats/${chatId}`),
  /** Turn a proposed plan into a running task. */
  approvePlan: (instanceId: string, planId: string) =>
    post<void>(`/api/chat/${instanceId}/plans/${planId}/approve`),
  discardPlan: (instanceId: string, planId: string) =>
    post<void>(`/api/chat/${instanceId}/plans/${planId}/discard`),

  // Shared work catalog
  workItems: () => get<WorkItem[]>("/api/work"),
  /**
   * Create or overwrite an item. Publishing addresses an item by name and
   * folder, so saving an edit means sending the same pair back — the version
   * bumps and the item stays one item.
   */
  putWorkItem: (body: {
    name: string;
    kind: WorkItem["kind"];
    content?: string;
    description?: string;
    parent_id?: string;
    mime?: string;
  }) => post<WorkItem>("/api/work", body),
  /**
   * Rename an item, move it to another folder, or both. parent_id is
   * deliberately absent-or-present: absent leaves the item where it is, and an
   * empty string moves it to the top level.
   */
  moveWorkItem: (id: string, body: { name?: string; parent_id?: string }) =>
    patch<WorkItem>(`/api/work/${id}`, body),
  deleteWorkItem: (id: string) => del<void>(`/api/work/${id}`),

  // Per-instance controls
  /** Partial update: only the fields present change. voice "" and voice_speed 0
   *  both mean "back to the app-wide default". */
  setInstanceAccess: (
    id: string,
    body: {
      shell_access?: boolean;
      sudo_access?: boolean;
      voice?: string;
      voice_speed?: number;
      system_prompt?: string;
    },
  ) => put<Instance>(`/api/instances/${id}/access`, body),
  /** Assign a bot its own ordered model chain. Replaces the list; the order is
   *  the fallback order. Empty returns it to the fleet-wide order. */
  setInstanceModels: (id: string, providerIds: string[]) =>
    put<Instance>(`/api/instances/${id}/models`, { provider_ids: providerIds }),
  instanceMemories: (id: string) => get<BotMemory[]>(`/api/instances/${id}/memories`),
  forgetMemory: (instanceId: string, memoryId: string) =>
    del<void>(`/api/instances/${instanceId}/memories/${memoryId}`),
  modelCombos: () => get<ModelCombo[]>("/api/model-combos"),
  saveModelCombo: (combo: { id?: string; name: string; description?: string; roles: Record<string, string> }) =>
    combo.id
      ? put<ModelCombo>(`/api/model-combos/${combo.id}`, {
          name: combo.name,
          description: combo.description ?? "",
          roles: combo.roles,
        })
      : post<ModelCombo>("/api/model-combos", {
          name: combo.name,
          description: combo.description ?? "",
          roles: combo.roles,
        }),
  deleteModelCombo: (id: string) => del<void>(`/api/model-combos/${id}`),

  // Provider sign-in with a Google account rather than a pasted key.
  /** The exact redirect URI to register on an OAuth client. Stated by the
   *  server rather than derived here, because a mismatch is the single most
   *  common way an OAuth setup fails. */
  oauthRedirectUri: () =>
    get<{ redirect_uri: string }>("/api/providers/oauth/redirect").then((r) => r.redirect_uri),
  /** Begin a browser sign-in. Returns the consent page to open in a new tab
   *  and the state to poll with. */
  startAuthCodeSignIn: (id: string, body: { client_id: string; client_secret?: string }) =>
    post<{ authorize_url: string; state: string; redirect_uri: string }>(
      `/api/providers/${id}/signin/url`,
      { client_id: body.client_id, client_secret: body.client_secret ?? "" },
    ),
  /** Poll while the consent tab completes. status is 'pending' or 'signed_in';
   *  a terminal failure comes back as a thrown ApiError with the reason. */
  authCodeSignInStatus: (state: string) =>
    get<{ status: string }>(`/api/providers/signin/status?state=${encodeURIComponent(state)}`),
  /** Begin the code-on-another-device fallback. */
  startDeviceSignIn: (id: string, body: { client_id: string; client_secret?: string }) =>
    post<{ user_code: string; verification_url: string; interval: number; expires_in: number }>(
      `/api/providers/${id}/signin`,
      { client_id: body.client_id, client_secret: body.client_secret ?? "" },
    ),
  /** Poll while the operator approves on the other device. */
  deviceSignInStatus: (id: string) => get<{ status: string }>(`/api/providers/${id}/signin`),
  /** Forget a stored sign-in. The server deletes the sealed credential and
   *  switches the connection back to key authentication. */
  providerSignOut: (id: string) => del<Provider>(`/api/providers/${id}/signin`),

  health: () => get<Record<string, unknown>>("/healthz"),
};

/** URL for the authenticated desktop stream, safe to drop into an iframe src.
 *
 * noVNC builds its WebSocket URL from `path`, not from the page URL, so the
 * token has to be threaded through there as well — otherwise the page loads and
 * the socket is rejected, which looks like a hung desktop rather than an auth
 * failure. */
export function vncUrl(instanceId: string, viewOnly = false): string {
  const token = encodeURIComponent(getToken() ?? "");
  const params = new URLSearchParams({
    autoconnect: "true",
    resize: "scale",
    reconnect: "true",
    show_dot: "true",
    view_only: String(viewOnly),
    path: `vnc/${instanceId}/websockify?token=${token}`,
  });
  return `/vnc/${instanceId}/vnc.html?${params.toString()}&token=${token}`;
}

export function artifactUrl(key: string): string {
  return `/api/artifacts/${key}?token=${encodeURIComponent(getToken() ?? "")}`;
}

// ------------------------------------------------------------------- voice ---

export interface TtsVoice {
  id: string;
  name: string;
  description?: string;
  speaker?: string;
  preset?: boolean;
  default?: boolean;
}

export interface TtsCatalogue {
  available: boolean;
  reason?: string;
  default?: string;
  voices: TtsVoice[];
}

export const voice = {
  /** What the sidecar can actually say, or why it cannot. */
  list: () => get<TtsCatalogue>("/api/voice/voices"),

  /**
   * Synthesise one utterance and return it as a playable blob URL.
   *
   * The response is audio, not JSON, so it cannot go through `request()` --
   * which parses every body as JSON and would throw on a WAV. The console used
   * to skip this endpoint entirely and call `window.speechSynthesis` instead,
   * so the "Pocket TTS voice co-pilot" was the operating system's own robot
   * voice and the sidecar's six distinct speakers were never heard.
   *
   * The caller owns the returned URL and must revokeObjectURL it, or every
   * utterance leaks a blob for the lifetime of the page.
   */
  speak: async (text: string, voiceId?: string, speed?: number): Promise<string> => {
    const token = getToken();
    const headers = new Headers({ "Content-Type": "application/json" });
    if (token) headers.set("Authorization", `Bearer ${token}`);

    const res = await fetch("/api/voice/speak", {
      method: "POST",
      headers,
      body: JSON.stringify({ text, voice: voiceId, speed }),
    });
    if (!res.ok) {
      let message = `${res.status} ${res.statusText}`;
      try {
        const body = await res.json();
        if (body?.error) message = body.error;
      } catch {
        /* the error body is not always JSON */
      }
      throw new ApiError(message, res.status);
    }
    return URL.createObjectURL(await res.blob());
  },
};
