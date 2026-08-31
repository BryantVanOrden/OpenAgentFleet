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
}

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
  role: "admin" | "operator" | "auditor";
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
    hint: "HMAC-SHA256 of the body in X-Hub-Signature-256 or X-AgentFleet-Signature.",
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
  created_at: string;
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
  }) => post<PeerMessage>("/api/vault/comms", body),

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

  chat: (instanceId: string) =>
    get<{ id: string; role: string; body: string; created_at: string }[]>(`/api/chat/${instanceId}`),
  sendChat: (instanceId: string, body: string, asTask: boolean) =>
    post<unknown>(`/api/chat/${instanceId}`, { body, as_task: asTask }),

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
