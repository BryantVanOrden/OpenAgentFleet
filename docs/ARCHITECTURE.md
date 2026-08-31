# Architecture

## Components

### Orchestrator (`backend/`, Go)

One binary, one process, several concerns kept in separate packages:

| Package       | Responsibility                                                       |
| ------------- | -------------------------------------------------------------------- |
| `fleet`       | Provisions and supervises sandboxes over the Docker Engine REST API   |
| `agent`       | The perceive → decide → act loop, stall detection, recursive sub-agents, and continual self-refinement |
| `connectors`  | Normalises chat completion across OpenAI, Anthropic, Gemini, Antigravity, Ollama and OpenAI-compatible gateways; owns the fallback chain and role-based routing through model combinations |
| `pipeline`    | Multi-bot DAG workflows. Independent stages run concurrently (bounded); each edge carries a condition that decides whether its downstream stage runs or is skipped |
| `swarm`       | Shared-blackboard swarms. Starts a real task per member and routes artifacts through peer review |
| `memory`      | Episodic memory behind `remember` and `recall`, private per bot plus a shared fleet pool. Real embeddings when a provider offers them, a hashed keyword index otherwise |
| `mcp`         | Model Context Protocol client: JSON-RPC 2.0 over stdio or Streamable HTTP, with tool discovery and invocation |
| `voice`       | Client for the text-to-speech sidecar, shared by the API and the agent loop |
| `schedule`    | Cron parsing and the trigger scheduler                                |
| `telemetry`   | Token counting (including cached reads) and cost accounting            |
| `config`      | Environment configuration and defaults                                |
| `recorder`    | Compiles a raw demonstration trace into a semantic skill              |
| `vault`       | AES-256-GCM sealed credential storage                                 |
| `artifacts`   | Screenshots and recordings, on a filesystem or S3/MinIO               |
| `notify`      | FCM and APNs push dispatch                                            |
| `bus`         | In-process fan-out to WebSocket clients                               |
| `httpapi`     | REST, the event socket, and the authenticated desktop proxy           |
| `store`       | PostgreSQL persistence with embedded migrations                       |

The Docker driver speaks the engine's REST API directly rather than through the official SDK. The surface needed is small — create, start, stop, pause, inspect, stats, exec, network — and avoiding the SDK keeps the dependency graph at five direct modules, which matters for something that holds a socket with root-equivalent authority.

### Sandbox (`sandbox/`)

An Ubuntu image running, under supervisord in dependency order:

```
Xvfb → dbus → at-spi-bus-launcher → xfce4 → x11vnc → websockify(noVNC) → agentd
                                          ↘ x11vnc -viewonly → websockify(noVNC view)
```

The second, view-only VNC pair is what makes the auditor role real. Auditors are
proxied there rather than being asked not to interact, because read-only enforced
in the client would not survive an edited URL.

`agentd` is the only component with access to the virtual input devices, the framebuffer, the accessibility bus, and the stateful Python REPL engine. It exposes:

| Endpoint         | Purpose                                                      |
| ---------------- | ------------------------------------------------------------ |
| `GET /health`    | Readiness — grabs a frame, so "X is up" is not mistaken for "the desktop is up" |
| `POST /observe`  | WebP frame, active window, flattened AT-SPI tree, difference hash |
| `POST /act`      | Execute one action from the shared vocabulary (`click`, `type`, `python`, `shell`, `snapshot`, …). Note `spawn_agent` is **not** among them — it is handled entirely orchestrator-side and never reaches agentd |
| `POST /record/*` | Start and stop a demonstration capture                        |
| `POST /keyring`  | Inject a run-scoped credential into `/var/run/agentfleet/keyring` (mode 0700). Not tmpfs: only `/tmp` is mounted as tmpfs, so a credential written here is on the container's writable layer. See SECURITY.md, "Credentials" |
| `DELETE /keyring`| Drop it again                                                 |
| `GET /voice/voices`, `POST /voice/speak` | The TTS surface           |

It has no authentication of its own, exactly like a kubelet: the port is never published, the sandbox network is not routable from outside, and operator authentication happens at the orchestrator's proxy.

### Admin console (`admin/`) and companion app (`mobile/`)

The console is for administration — provisioning, recording, engine configuration, skill timeline editing, and AI refinement. The phone app is for triage — watch, unblock, take over, stop.

---

## The Agent Loop & Advanced Capabilities

```
        ┌──────────────────────────────────────────┐
        │  observe: frame + a11y tree + dhash      │
        └───────────────┬──────────────────────────┘
                        │
              hash unchanged 3x? ──yes──► escalate, wait for a human
                        │ no
        ┌───────────────▼──────────────────────────┐
        │  prompt: goal + SKILL.md + history +     │
        │          screenshot + REPL state         │
        └───────────────┬──────────────────────────┘
                        │
        ┌───────────────▼──────────────────────────┐
        │  model → one structured JSON action      │
        └───────────────┬──────────────────────────┘
                        │
       ┌────────────────┼──────────────────────────┬────────────────────────┐
       ▼                ▼                          ▼                        ▼
[GUI / AT-SPI]    [python REPL]             [spawn_agent]              [ask_human]
Click/Type/Key    Stateful code execution   Delegate to child task     Escalate to phone
       │                │                          │                        │
       └────────────────┴─────────────┬────────────┴────────────────────────┘
                                      │
                        ┌─────────────▼────────────┐
                        │   Task complete / Succeeded?
                        └─────────────┬────────────┘
                                      │
                                      ▼
                        ┌──────────────────────────┐
                        │ Continual AI Refinement  │
                        │ (Self-heals SKILL.md v2) │
                        └──────────────────────────┘
```

### Core Design Decisions

1. **Structured JSON, not tool calling.** Asking for one JSON object and parsing it strictly works uniformly across Ollama, OpenAI, Anthropic, and open-source models.
2. **Labels over coordinates.** The action schema accepts both, but strongly biases toward `target` (accessible labels). Coordinates survive as a fallback when AT-SPI is unavailable.
3. **Perceptual diff hash (`dhash`).** Stall detection uses a 64-bit difference hash over the desktop, distinguishing real UI transitions from cursor blinks.
4. **Persistent Python REPL Substrate.** Inspired by Prime Agent, `agentd` embeds a persistent Python session (`repl.py`) where variables, custom functions, and accessibility queries persist across steps.
5. **Recursive Sub-Agents (`spawn_agent`).** Complex multi-stage goals can be broken into focused sub-tasks and delegated to child agents either synchronously or asynchronously.
6. **Continual Harness & AI Self-Refinement.** When tasks complete, the Refinement Engine inspects the trajectory to replace drifted coordinates with stable accessibility selectors, updating `SKILL.md` automatically.

---

## Data Model

```
users ──< devices                      push targets for the companion app
      ──< tasks ──< tasks (parent/child sub-agents)
                ──< task_steps         one row per turn, with the screenshot key
instances ──< tasks
          ──< chat_messages
skills (with version & refinement notes)
providers ──> secrets                  by ref; the value is sealed, never inline
alerts                                 escalations, with the operator's reply
```

Migrations are embedded in the binary and applied automatically on boot in filename order.

---

## Networking & Security

Two Docker networks:
* `control`: Holds database, orchestrator API, and admin console.
* `agentfleet_sandbox`: Holds the sandboxes; the orchestrator joins both to proxy the desktop and speak to `agentd`.
* Sandbox containers have no published host ports. Egress filtering with
  `nftables` is available but **off unless the instance carries a policy** —
  an allow list, a deny list, or `block_local`. A bot provisioned without one
  can reach whatever its network can reach, including RFC1918 addresses. This
  used to read "blocking RFC1918 private networks by default", which was the
  reassuring direction to be wrong in; SECURITY.md has always said otherwise.
