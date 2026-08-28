# AgentFleet

<p align="center">
  <strong>Self-hosted platform for autonomous computer-use agents.</strong><br>
  Spin up isolated Linux desktops, record tasks by demonstration, let AI workers execute and continually self-improve, and get alerted on your phone when human judgment is required.
</p>

---

```
  Flutter Companion App               Admin Console (React 19)
             \                                  /
              \                                /
        ┌──────▼──────────────────────────────▼──────┐
        │             Orchestrator (Go)              │
        │   Fleet Manager · Agent Loop · Vault       │
        │   Refinement Engine · Multi-Model Gateway  │
        └──────┬──────────────────────────────┬──────┘
               │ Docker API                   │ HTTP / Internal Net
        ┌──────▼──────┐                ┌──────▼───────────────┐
        │ Sandbox     │      ...       │ Sandbox              │
        │ XFCE + Xvfb │                │  agentd (AT-SPI/GUI) │
        │ noVNC Proxy │                │  Persistent Py REPL  │
        └─────────────┘                └──────────────────────┘
```

---

## Key Superpowers

* 🛡️ **Hard-Sandboxed Desktops**: Containerized Ubuntu XFCE sessions governed by strict cgroup envelopes (CPU, RAM, disk quotas), optional GPU passthrough, and kernel-level `nftables` network egress policies.
* 👁️ **Hybrid Visual & Accessibility Perception**: Blends high-resolution WebP visual frames with AT-SPI semantic accessibility trees. Automatic image-to-display coordinate mapping ensures pixel-perfect interaction across arbitrary resolutions.
* 🧠 **Continual Harness & AI Self-Refinement**: Inspired by recursive agent research, AgentFleet's post-task refinement engine inspects execution trajectories to self-heal fragile coordinate clicks into robust accessible selectors, automatically evolving `SKILL.md` workflows from `v1` to `v2`.
* 🔀 **Recursive Sub-Agent Orchestration**: Agents can dynamically spawn and coordinate child sub-agents (`spawn_agent`) to handle parallel research, compilation, or verification tasks with full parent-child hierarchy tracking.
* 🐍 **Persistent Python REPL Substrate**: Embeds a stateful, interactive Python REPL inside the sandbox daemon (`agentd`). Agents can manipulate data, query the AT-SPI bus programmatically, and maintain state variables across turns.
* 📱 **Mobile Human-in-the-Loop Triage**: First-class Flutter mobile companion with time-sensitive APNs/FCM push dispatch. When agents encounter CAPTCHAs, 2FA prompts, or perceptual stalls, they pause and request authoritative instruction from your phone.
* 🔐 **Cryptographic Credential Vault**: AES-256-GCM sealed credential storage. Secrets are never exposed in prompts or audit logs and are injected strictly at runtime into unprivileged sandbox tmpfs keyrings.
* 🌐 **Universal Model Fallback Chain**: Works out-of-the-box with local vision models (Ollama / `qwen2.5vl:7b`), OpenAI, Anthropic, Google Gemini, or any OpenAI-compatible gateway (vLLM, LiteLLM) configured as a resilient fallback chain.

## Pre-Configured Bot Archetypes

AgentFleet includes 8 out-of-the-box, role-specialized agent personas equipped with domain tools, repos, and tailored prompts:

| Archetype | Icon | Category | Recommended Hardware | Pre-installed Tooling & Repos |
| :--- | :---: | :--- | :--- | :--- |
| **CyberSec PenTester** | 🛡️ | Security | `standard` (4 vCPU, 8 GB) | `nmap`, `wireshark`, `ffuf`, `metasploit`, `ghidra`, `semgrep`, `sqlmap`, `burpsuite` |
| **Full-Stack Developer** | 💻 | Engineering | `developer-heavy` (8 vCPU, 16 GB) | VS Code, Node/Bun/pnpm, Go, Python, Rust, Docker CLI, PostgreSQL, Playwright |
| **QA & UI/UX Auditor** | 🎨 | QA & Design | `standard` (4 vCPU, 8 GB) | Playwright, Cypress, Lighthouse CI, Pa11y, Axe-Core, GIMP, Figma Web |
| **Game Dev & 3D Engine** | 🎮 | Gaming & 3D | `developer-heavy` (8 vCPU, 32 GB, GPU) | Godot Engine 4, Blender 3D, Aseprite, Pygame, GLTF validator, Shader compiler |
| **Social Media & Growth** | 📱 | Marketing | `micro` (2 vCPU, 4 GB) | Chromium Multi-Profile, Postiz/Buffer CLI, Photopea, FFmpeg Short-Clipper |
| **Media Studio & Video** | 🎬 | Creative | `power-user` (8 vCPU, 24 GB, GPU) | FFmpeg, Kdenlive, Audacity, Whisper AI Transcriber, ImageMagick, ComfyUI |
| **Agentic CRM (Comp AI)** | 🤝 | Sales & CRM | `standard` (4 vCPU, 8 GB) | Comp AI CRM (`trycompai/crm`), PostgreSQL, Email Drafting Engine, Lead Enrichment API |
| **Data Scientist & Quant** | 📈 | Data & Finance | `developer-heavy` (8 vCPU, 16 GB) | JupyterLab, Polars, DuckDB, Pandas, yfinance, Plotly, SciPy, Quarto |

---

## Hardware Profiles & Isolation Tiers

AgentFleet ships with four pre-configured hardware tiers:

| Tier | vCPU | Memory | Disk | GPU | Primary Use Case |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `micro` | 1.0 | 1 GB | 10 GB | No | Headless CLI, file processing, light terminal scripts |
| `standard` | 2.0 | 4 GB | 25 GB | No | Web browsing, form entry, SaaS navigation, documentation |
| `power-user` | 4.0 | 8 GB | 50 GB | No | Multi-window desktop workflows, heavy browser automation |
| `developer-heavy` | 8.0 | 16 GB | 100 GB | Optional | Compiling large codebases, game engines, SWE-bench tasks |

---

## Quick Start

### Prerequisites
* **Docker & Docker Compose** (v24+)
* **Ollama** with a vision model pulled (e.g. `ollama pull qwen2.5vl:7b`) OR a cloud API key (OpenAI, Anthropic, Gemini).

```bash
# 1. Clone repository and verify environment
git clone https://github.com/BryantVanOrden/AgentFleet.git
cd AgentFleet

# 2. Run preflight doctor (checks Docker, RAM, ports, secrets, vision models)
make doctor

# 3. Build sandbox images and start platform
make up
```

Once started:
1. Open the Admin Console at **<http://localhost:8081>**.
2. Complete the one-time administrator bootstrap.
3. Under **Engines**, test your local Ollama endpoint or add cloud API keys.
4. Click **⚡ Launch an Agent** to start your first autonomous workflow!

---

## Verifying with Smoke Tests

```bash
# Basic smoke test: provisions sandbox, verifies observation, injects input, checks screen dhash change
make smoke

# Full end-to-end agent smoke test with real autonomous model execution
make smoke-agent
```

---

## Project Architecture & Directory Layout

| Directory | Description |
| :--- | :--- |
| [`backend/`](backend/) | High-performance Go orchestrator (fleet supervisor, agent perceive-decide-act loop, model gateway, vault, SQLite/Postgres persistence, WebSocket event bus) |
| [`sandbox/`](sandbox/) | Ubuntu desktop container image (XFCE, Xvfb, AT-SPI accessibility bus, x11vnc, noVNC proxy, and the `agentd` daemon) |
| [`admin/`](admin/) | React 19 + TypeScript + Vite + Tailwind CSS operator console (fleet dashboard, live VNC streams, skill timeline editor, AI refinement studio) |
| [`mobile/`](mobile/) | Flutter companion app (real-time desktop streaming, interactive agent chat, resolution centre, FCM/APNs push notifications) |
| [`scripts/`](scripts/) | Preflight health check (`doctor.sh`) and end-to-end integration test (`smoke.sh`) |
| [`docs/`](docs/) | In-depth technical guides, architecture specifications, and security model |

---

## The Perceive → Decide → Act Loop

```
1. Observe   ──► agentd grabs WebP frame, active window title, AT-SPI tree, and perceptual dhash.
2. Guard     ──► If screen hash is unchanged across 3 actions, the agent pauses and escalates to your phone.
3. Prompt    ──► Assembles goal, SKILL.md guide, recent history, REPL variables, and downscaled screenshot.
4. Decide    ──► Model returns exactly one structured JSON action.
5. Act       ──► Translates coordinates to desktop pixel space and executes via agentd (or runs persistent Python / spawns child agent).
6. Persist   ──► Stores screenshot, duration, prompt/completion token metrics, and outcome in audit trail.
7. Refine    ──► Upon task success, the Continual Refinement Engine self-heals selectors and updates SKILL.md.
```

---

## Available Commands

```bash
make doctor           # Check Docker, RAM, ports, secrets, and model availability
make up               # Build sandbox image and start entire stack
make down             # Stop the stack (sandboxes are preserved)
make smoke            # Run end-to-end integration smoke test
make smoke-agent      # Run live autonomous task against an AI model
make clean-sandboxes  # Terminate and destroy all managed sandbox containers
make logs             # Tail orchestrator logs
make test             # Run Go test suites and React production build
```

---

## In-Depth Documentation

* 📐 [**Architecture Guide**](docs/ARCHITECTURE.md) — Detailed component design, data flow, schema, and networking model.
* 🔌 [**REST & WebSocket API Reference**](docs/API.md) — Complete endpoint specifications, request/response schemas, and real-time events.
* 🎓 [**Skills & Continual Refinement**](docs/SKILLS_AND_REFINEMENT.md) — Learn-by-demonstration recorder, AT-SPI compilation, and AI self-healing workflows.
* 🔒 [**Security & Threat Model**](docs/SECURITY.md) — Sandboxing guarantees, egress firewall rules, credential sealing, and prompt injection defenses.
* 🛠️ [**Developer Guide**](docs/DEVELOPER_GUIDE.md) — Local development workflow, running tests, and building mobile/admin frontends.
* 🗺️ [**Roadmap**](docs/ROADMAP.md) — Current capabilities and upcoming milestones.

---

## License

AgentFleet is open source under the [MIT License](LICENSE).
