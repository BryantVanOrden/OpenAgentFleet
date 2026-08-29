# AgentFleet API Reference

The AgentFleet platform exposes a REST API and a real-time WebSocket event bus. Operator and admin clients (the React Admin Console and the Flutter Mobile Companion) communicate with the Go orchestrator through these endpoints.

Base URL: `http://<host>:8080` (default)

---

## Authentication & Authorization

AgentFleet uses JWT bearer tokens for session authentication. Pass the token in the `Authorization` header:

```http
Authorization: Bearer <token>
```

### Roles
- **admin**: Full system control (provider credentials, vault secrets, user management, fleet settings).
- **operator**: Instance lifecycle, launching tasks, skill recording/refinement, answering alerts, manual takeover.
- **auditor**: Read-only access to instance telemetry, task audit logs, and replay traces.

---

## 1. Authentication Endpoints

### `POST /api/auth/bootstrap`
Creates the initial administrator account. Available only when the database contains zero users.
* **Request**: `{"email": "admin@example.com", "password": "secure-password"}`
* **Response `201`**: `{"user": User, "token": "jwt..."}`

### `POST /api/auth/login`
Authenticates an existing user and issues a JWT session token.
* **Request**: `{"email": "operator@example.com", "password": "password"}`
* **Response `200`**: `{"user": User, "token": "jwt..."}`

### `GET /api/me`
Returns the profile and role of the currently authenticated user.
* **Response `200`**: `{"id": "...", "email": "...", "role": "admin", "created_at": "..."}`

---

## 2. Fleet & Instance Management

### `GET /api/tiers`
Lists available hardware resource tiers (`micro`, `standard`, `power-user`, `developer-heavy`).

### `GET /api/instances`
Lists all sandboxed desktop instances.

### `POST /api/instances`
Provisions and starts a new containerized or virtualized desktop instance.
* **Request**:
  ```json
  {
    "name": "build-sandbox-1",
    "tier": "developer-heavy",
    "shell_access": true,
    "egress": {
      "allow": ["github.com", "registry.npmjs.org"],
      "block_local": true
    },
    "override": {
      "memory_mb": 16384,
      "vcpu": 8
    }
  }
  ```
* **Response `201`**: `Instance` object.

### `GET /api/instances/{id}`
Returns details for a single instance, including runtime state, connection URLs, and error states.

### `DELETE /api/instances/{id}`
Terminates and destroys the sandbox container and its associated temporary volumes.

### Lifecycle Actions
* `POST /api/instances/{id}/start` — Starts a stopped container.
* `POST /api/instances/{id}/stop` — Stops a running container.
* `POST /api/instances/{id}/pause` — Freezes the container's cgroup processes.
* `POST /api/instances/{id}/resume` — Unfreezes a paused container.

### `GET /api/instances/{id}/stats`
Returns point-in-time CPU, RAM, and network I/O metrics.

### `GET /api/instances/{id}/observe`
Captures an observation frame directly from the in-sandbox `agentd` daemon without advancing the agent loop.
* **Query Params**: `a11y=true|false`, `max_width=1280`
* **Response `200`**: `Observation` object (screenshot base64, active window, a11y tree, perceptual dhash).

---

## 3. Skills & Continual Refinement

### `GET /api/skills`
Lists all registered skill workflows.

### `GET /api/skills/{id}`
Retrieves full details for a skill, including semantic steps, parameters, rendered `SKILL.md`, version, and refinement notes.

### `POST /api/skills` / `PUT /api/skills/{id}`
Creates or updates a skill definition.
* **Request**:
  ```json
  {
    "name": "Deploy Staging App",
    "description": "Opens browser, logs into dashboard, and triggers staging deployment.",
    "params": ["environment", "build_id"],
    "steps": [
      {
        "index": 1,
        "kind": "focus",
        "window": "Firefox"
      },
      {
        "index": 2,
        "kind": "click",
        "role": "push button",
        "label": "Deploy"
      }
    ]
  }
  ```

### `DELETE /api/skills/{id}`
Deletes a recorded skill.

### `POST /api/skills/{id}/refine`
Triggers AI Continual Self-Refinement on an existing skill.
* **Request**: `{"task_id": "optional-specific-task-id"}`
* **Response `200`**: Refined `Skill` object with incremented version and self-healed selectors.

### Demonstration Recording Studio
* `POST /api/instances/{id}/record/start` — Begins capturing input and AT-SPI accessibility events (`{"name": "Procedure Name"}`).
* `POST /api/instances/{id}/record/stop` — Stops capture, automatically compiles raw events into a semantic `Skill`, renders `SKILL.md`, and persists the recording artifact.

---

## 4. Autonomous Tasks & Sub-Agents

### `POST /api/tasks`
Launches an autonomous task on an instance.
* **Request**:
  ```json
  {
    "instance_id": "inst-123",
    "goal": "Build the frontend package and verify tests pass",
    "skill_id": "optional-skill-id",
    "params": {"branch": "main"},
    "parent_task_id": "optional-parent-task-id",
    "auto_refine": true,
    "max_steps": 60,
    "provider_id": "optional-provider-id"
  }
  ```
* **Response `201`**: `Task` object.

### `GET /api/tasks`
Lists tasks with optional filtering by `instance_id`.

### `GET /api/tasks/{id}`
Returns task metadata, status (`queued`, `running`, `awaiting_human`, `succeeded`, `failed`, `cancelled`), step counter, error/result summaries, and live execution status.

### `GET /api/tasks/{id}/steps`
Returns the complete replay trail of step records:
* Step index
* Agent decision (`action`, `thought`, `target`, `coordinates`, `code`, etc.)
* Screenshot observation key
* Outcome text and execution latency
* Prompt and output token usage

### `POST /api/tasks/{id}/synthesize-skill`
Synthesizes a brand new reusable `SKILL.md` workflow from a completed scratch task execution trace.
* **Response `201`**: Created `Skill` object.

### `POST /api/tasks/{id}/cancel`
Cancels an in-flight task immediately.

---

## 5. Alerts & Human-in-the-Loop Escalation

### `GET /api/alerts`
Lists alert notifications with optional filter `open=true|false`.

### `POST /api/alerts/{id}/reply`
Submits an operator's authoritative instruction to an awaiting agent (resolving CAPTCHAs, MFA codes, or policy confirmations).
* **Request**: `{"reply": "Use code 482910 to complete the login"}`
* **Response `200`**: `{}`

---

## 6. AI Model Providers & Vault

### `GET /api/providers`
Lists configured LLM/VLM gateways.

### `POST /api/providers` / `PUT /api/providers/{id}`
Configures a model endpoint (OpenAI, Anthropic, Gemini, Ollama, or OpenAI-compatible gateways like vLLM/LiteLLM).
* **Request**:
  ```json
  {
    "name": "Local Qwen 2.5-VL",
    "kind": "ollama",
    "base_url": "http://localhost:11434",
    "model": "qwen2.5vl:7b",
    "vision": true,
    "priority": 10,
    "enabled": true
  }
  ```

### `POST /api/providers/reorder`
Atomically reorders the multi-tier fallback priority chain in a single database transaction.
* **Request**: `{"ids": ["prov-claude", "prov-antigravity", "prov-ollama"]}`
* **Response `200`**: Updated list of `Provider` objects sorted by `priority ASC`.

### `GET /api/providers/models`
Discovers and lists available models dynamically for any provider kind (Ollama, OpenAI, Anthropic, Gemini, Google Antigravity, OpenAI-compatible).
* **Query Params**: `kind=antigravity|openai|anthropic|gemini|ollama|openai-compatible`, `base_url=...`, `api_key=...`
* **Response `200`**: `{"models": [ModelDescriptor], "live": true}`

### `POST /api/providers/{id}/probe`
Sends a test probe to confirm connectivity, latency, and multimodal capabilities.

### `PUT /api/instances/{id}/providers`
Assigns a tailored, per-bot tiered fallback chain (e.g. `["prov-claude", "prov-antigravity", "prov-ollama"]`).

### `GET /api/providers/oauth/{kind}/start`
Initiates OAuth + PKCE authentication flow for cloud providers (Google Antigravity, OpenAI, etc.).
* **Response `200`**: `{"auth_url": "...", "state": "..."}`

### `POST /api/providers/oauth/{kind}/exchange`
Exchanges the authorization code for tokens and seals them into the vault.
* **Request**: `{"code": "...", "state": "...", "code_verifier": "..."}`
* **Response `200`**: Created `Provider` object.

### `GET /api/secrets`
Lists secret reference names and notes (plaintext secrets are never returned).

### `PUT /api/secrets/{ref}`
Stores or rotates an AES-256-GCM encrypted secret in the orchestrator vault (`{"value": "secret-token", "note": "GitHub PAT"}`).

---

## 7. Chat Sessions & Comms Threads

### `GET /api/chat/{instanceId}/sessions`
Lists isolated, named chat sessions for an instance.

### `POST /api/chat/{instanceId}/sessions`
Creates a fresh scoped chat session (`{"name": "Database Migration Planning"}`).

### `PATCH /api/chat/{instanceId}/sessions/{sessionId}`
Renames or pins a chat session (`{"name": "...", "pinned": true}`).

### `DELETE /api/chat/{instanceId}/sessions/{sessionId}`
Deletes a chat session and purges its conversation context.

### `GET /api/instances/{id}/memories`
Retrieves long-term episodic vector memories for an instance.

---

## 8. Voice & Pocket TTS Co-Pilot

### `POST /voice/speak`
Synthesizes speech using Kyutai Labs' Pocket TTS across 6 curated voice models.
* **Request**: `{"text": "Task execution finished.", "voice": "shadow"}`
* **Response `200`**: `{"status": "spoken", "voice": "shadow"}`

---

## 9. Real-Time WebSocket Event Stream

Connect via WebSocket to `/api/events` with your bearer token:

```
ws://localhost:8080/api/events?token=<jwt-token>
```

### Event Payload Schema
```json
{
  "type": "task.step",
  "instance_id": "inst-123",
  "task_id": "task-456",
  "payload": {
    "step": 4,
    "action": {
      "thought": "Locating submit button",
      "action": "click",
      "target": "Submit Form"
    },
    "provider": "anthropic",
    "model": "claude-3-7-sonnet"
  },
  "at": "2026-08-28T19:50:00Z"
}
```

### Event Types
| Event | Trigger |
| :--- | :--- |
| `task.step` | Emitted every turn with the model's action, latency, and token attribution. |
| `task.state` | State transition (`running`, `awaiting_human`, `succeeded`, `failed`, `cancelled`). |
| `task.spawned` | Emitted when an agent spawns a child sub-agent. |
| `skill.refined` | Emitted when a skill is self-healed and updated with a new version. |
| `skill.synthesized` | Emitted when a scratch run is synthesized into a reusable workflow. |
| `instance.state` | Instance lifecycle state change (`provisioning`, `running`, `paused`, `stopped`). |
| `alert` | Emitted when an agent hits a stall or requires operator intervention. |
| `record.started` / `record.stopped` | Fired during demonstration recording sessions. |
| `stats` | Periodic CPU, memory, and bandwidth utilization samples. |

