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

export interface EgressPolicy {
  allow?: string[];
  deny?: string[];
  block_local: boolean;
}

export interface Instance {
  id: string;
  name: string;
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
  kind: "openai" | "ollama" | "anthropic" | "gemini" | "openai-compatible";
  base_url?: string;
  model: string;
  api_key_ref?: string;
  vision: boolean;
  temperature: number;
  max_tokens: number;
  priority: number;
  enabled: boolean;
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

export const api = {
  login: (email: string, password: string) =>
    post<{ token: string; user: User }>("/api/auth/login", { email, password }),
  bootstrap: (email: string, password: string) =>
    post<{ token: string; user: User }>("/api/auth/bootstrap", { email, password }),
  me: () => get<User>("/api/me"),

  tiers: () => get<TierProfile[]>("/api/tiers"),
  instances: () => get<Instance[]>("/api/instances"),
  instance: (id: string) => get<Instance>(`/api/instances/${id}`),
  createInstance: (body: {
    name: string;
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

  alerts: (openOnly = false) => get<Alert[]>(`/api/alerts?open=${openOnly}`),
  replyAlert: (id: string, reply: string) => post<void>(`/api/alerts/${id}/reply`, { reply }),

  providers: () => get<Provider[]>("/api/providers"),
  saveProvider: (p: Partial<Provider> & { api_key?: string }) =>
    p.id ? put<Provider>(`/api/providers/${p.id}`, p) : post<Provider>("/api/providers", p),
  deleteProvider: (id: string) => del<void>(`/api/providers/${id}`),
  probeProvider: (id: string) => post<{ ok: boolean; error?: string }>(`/api/providers/${id}/probe`),
  ollamaModels: (baseUrl?: string) =>
    get<{ models: string[]; error?: string }>(
      `/api/providers/ollama/models${baseUrl ? `?base_url=${encodeURIComponent(baseUrl)}` : ""}`,
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
