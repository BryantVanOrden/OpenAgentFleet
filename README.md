# AgentFleet

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/Python-3.10+-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python" />
  <img src="https://img.shields.io/badge/Flutter-3.24+-02569B?style=for-the-badge&logo=flutter&logoColor=white" alt="Flutter" />
  <img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker" />
  <img src="https://img.shields.io/badge/License-NonCommercial_1.0-orange?style=for-the-badge" alt="License" />
</p>

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

## What it does

- **Sandboxed desktops.** Each bot is a container running Ubuntu with XFCE, under
  cgroup limits on CPU and memory with swap disabled, an optional GPU device
  request, and an optional nftables egress policy. See [Security](docs/SECURITY.md)
  for what that isolation is and is not worth.
- **A perceive-decide-act loop.** Each turn the agent gets a WebP frame with
  Set-of-Marks badges over interactive elements, the AT-SPI accessibility tree,
  and its own history; it replies with one JSON action. Coordinate conventions
  are calibrated per model automatically, because vision models disagree about
  whether a coordinate is a pixel or a fraction and will not tell you which.
- **Model combinations.** Name a mapping from roles — vision, reasoning, chat,
  summarize, refine — to different models, and use it anywhere a single provider
  would go. A bot's fallback chain can mix single providers and combinations, so
  "these two models, and if neither answers, that one" is one list.
- **Organisations, departments and per-bot permissions.** Beyond the three
  platform roles, organisations have their own members and org roles, and
  individual bots carry permission grants. A bot can belong to several
  departments at once — support and engineering sharing a triage bot is the
  ordinary shape of a company — and a member of any of them can reach it, at
  whatever that department's role allows. A per-bot grant still overrides all
  of it, including when it is empty, which is how one machine is hidden from
  someone who can otherwise see the department.
- **Agents that divide work and hand it on.** Name agents in a fleet message —
  "Builder writes it, ToolCheck tests it, Auditor reviews it" — and they answer
  in that order, each taking the part addressed to it. Names are matched
  loosely, so a typo or a split word still finds the right bot. Finishing a
  part wakes whoever the next one belongs to and hands them what was produced;
  testing and reviewing wait for something to exist rather than starting on
  nothing. A review hands back to whoever built. Bounded at six rounds, because
  agents starting each other without a limit is the failure that costs money.
- **A shared work catalog.** Agents publish what they make for each other and
  for you: files to build on, workspaces to group a piece of work, and *apps* —
  one self-contained HTML document each — that the phone renders and runs from
  a tab in Vault. They reach it with `publish_work` and `read_work`, keyed by
  name, so improving a colleague's work is an edit that bumps its version
  rather than a second copy nobody notices.
- **Per-bot personality and voice.** Each bot carries its own personality,
  prefilled at creation from the personality its archetype ships with and
  editable at any time; it is read when a prompt is built, so a change lands on
  the next reply. Voice and speaking pace are per bot too, which is what makes
  a fleet legible by ear.
- **Administration in its own place.** Users, departments, AI connections and
  API keys sit on an Admin tab that only administrators see. Provider
  connections and model chains are admin-only: they decide what the fleet costs
  and which model sees a bot's screen, which is not an operator's business.
- **API keys.** Long-lived credentials for scripts and CI, so nothing
  automated has to hold a person's password. A key acts with its owner's role,
  its secret is shown once and stored only as a hash, and revoking the key —
  or disabling the person — stops it immediately.
- **Teaching by demonstration.** Record a human doing the task; the trace is
  compiled into a `SKILL.md` of semantic steps, editable in the console. After a
  successful run the agent can rewrite its own skill, replacing brittle
  coordinate clicks with accessible labels.
- **Escalation to a human.** An unchanged screen across several actions, or an
  action the agent should not take alone — a CAPTCHA, an MFA prompt, a payment —
  parks the task and raises an alert, with the screen as it was at the moment it
  stopped. Answer from the console or the phone and the run continues.
- **A stateful Python REPL in the sandbox.** Variables, handles and browser
  sessions persist across turns, so a workflow that would be forty clicks can be
  a few lines instead.
- **Custom tools.** Choose which tools an archetype installs, and add your own
  recipes.
- **Workspace snapshots.** A tarball of the agent's working directory, taken
  before a risky step and restored when the agent asks to roll back.
- **Sub-agents.** An agent can spawn children, bounded by depth, siblings per
  task and total concurrent tasks. Children run on the same desktop as the
  parent, so they suit parallel research rather than parallel GUI work.
- **Chat, conversations and identity.** Per-bot named chat sessions that can be
  pinned; fleet-wide conversations with membership; and a sender identity on each
  message, so an agent knows who is speaking rather than reading an
  undifferentiated stream. A bot can propose a plan and wait for approval instead
  of acting immediately.
- **Pipelines.** Multi-bot DAG workflows, executed in topological order and
  resumable after an orchestrator restart. See the note below on what the engine
  does not yet do.
- **Triggers.** Webhook ingress on a per-webhook token with optional HMAC, and a
  cron scheduler. Both persist across restarts.
- **Cost telemetry.** Prompt and completion tokens, latency and a dollar estimate
  per call, from a hand-maintained price table.
- **A phone app.** Flutter, on Android, iOS, macOS, Linux and Windows, with push
  alerts and interactive takeover on all five.

### What is partly built

Stated here rather than left to be discovered. None of the following is finished,
and some of it is a stub behind a working-looking screen.

- **MCP bridge.** `backend/internal/mcp` registers servers and lists tools, but it
  speaks no MCP: there is no JSON-RPC, no stdio or SSE transport, and `CallTool`
  returns a canned response without executing anything. The transport, command
  and URL fields are stored and never read, registrations do not survive a
  restart, and agents cannot reach it at all — the `call_mcp` action is not in the
  parser's accepted set. Treat the MCP Hub screen as a placeholder.
- **Swarms.** The coordinator is an in-memory CRUD store with a shared message
  list. Creating a swarm does not create tasks, start instances or run anything,
  and with no members specified it fabricates three bots that do not exist. There
  is no peer review and no route to publish an artifact.
- **Pipeline execution.** The engine runs nodes one at a time, in topological
  order. It does not run independent nodes in parallel, and it does not evaluate
  edge conditions — a `condition` on an edge is stored and displayed and nothing
  acts on it. There is also no graph builder: the console's create button posts a
  fixed three-node pipeline, and everything else is a viewer.
- **Archetype export/import.** The SDK writes an `.agentfleet.json` manifest
  (not `.agentfleet.yaml`), with tools and recorded skills left empty. `hub
  import` reads the file and prints a summary; it does not create anything. There
  is no endpoint and no UI.
- **Voice.** The Pocket TTS sidecar is real and serves speech over
  `/api/voice/speak`. But the agent's own `speak` action does not reach it — it
  falls through to a tone generator in the sandbox whose output is discarded —
  and the console's voice co-pilot uses the browser's own speech synthesis. The
  default voice is `echo`, not `shadow`.
- **Memory.** Persistent and useful, but it is a bag-of-words index scanned
  linearly, not embeddings, and it is per-bot: nothing ever writes to the shared
  namespace, so one agent's discovery is not retrievable by another.
- **Cached-token accounting.** The column exists and is summed; nothing ever
  populates it, so it is always zero.
- **Webhooks are generic.** One token endpoint that renders a goal template. There
  is no GitHub, Stripe or CRM specific parsing; Stripe's signature scheme in
  particular is not understood.
- **`developer-heavy` needs an image that is not built here.** The tier asks for
  `agentfleet/sandbox:latest-dev`, and no Dockerfile or make target produces that
  tag. Build it yourself or override the image per instance.

## Bot archetypes

Eleven role-specialised personas, each with a default tier, a tool list and an
operating playbook. The tool lists below are what the archetype *asks* for.
Installation happens at container start and some of it fails — the orchestrator
reconciles the agent's prompt against what actually landed, so the model is told
what it really has rather than what the template hoped for.

For the vCPU and memory behind each tier name, see
[Hardware profiles](#hardware-profiles-and-isolation-tiers) below. Earlier
revisions of this table carried its own numbers, and they were wrong.

| Archetype | Icon | Category | Default tier | Tooling the archetype asks for |
| :--- | :---: | :--- | :--- | :--- |
| **Fleet Manager & Commander** | 🎯 | Management | `standard` | `tmux`, `git`, `gh`, `ripgrep`, `jq`, `curl`, `n8n`, `htop`, `tree` — decomposes goals & delegates to specialists |
| **CyberSec PenTester** | 🛡️ | Security | `standard` | `nmap`, `wireshark`, `ffuf`, `metasploit`, `ghidra`, `semgrep`, `sqlmap`, `burpsuite`, `nuclei`, `subfinder`, `SecLists` |
| **Full-Stack Architect** | 💻 | Engineering | `developer-heavy` | VS Code, Node/Bun/pnpm, Go, Python, Rust, Docker CLI, PostgreSQL, Redis, Playwright, `gh`, `lazygit`, `ripgrep` |
| **DevOps & Cloud SRE** | ⚙️ | DevOps | `developer-heavy` | `kubectl`, `helm`, `terraform`, `ansible`, `k9s`, `docker`, `aws-cli`, `gcloud`, `promql-cli`, `grafana-cli`, `trivy` |
| **QA & UI/UX Auditor** | 🎨 | QA & Design | `standard` | Playwright, Cypress, Lighthouse CI, Pa11y, Axe-Core, GIMP, Figma Web, ImageMagick, Screenkey |
| **Game Dev & 3D Engine** | 🎮 | Gaming & 3D | `developer-heavy` | Godot Engine 4, Blender 3D, Aseprite, Pygame, GLTF validator, Shader compiler, RenderDoc, MeshLab |
| **Social Media & Growth** | 📱 | Marketing | `micro` | Chromium Multi-Profile, Postiz/Buffer CLI, Photopea, FFmpeg Short-Clipper, `yt-dlp`, Whisper |
| **Media Studio & Video** | 🎬 | Creative | `power-user` | FFmpeg, Kdenlive, Audacity, Whisper AI Transcriber, ImageMagick, ComfyUI, OBS Studio, HandBrake |
| **Agentic CRM (Comp AI)** | 🤝 | Sales & CRM | `standard` | Comp AI CRM (`trycompai/crm`), PostgreSQL, Email Drafting Engine, Lead Enrichment API, DuckDB, `n8n` |
| **Data Scientist & Quant** | 📈 | Data & Finance | `developer-heavy` | JupyterLab, Polars, DuckDB, Pandas, yfinance, Plotly, SciPy, Quarto, TA-Lib, Scikit-Learn |
| **Deep Academic Researcher**| 🔬 | Research | `standard` | Zotero, Pandoc, Typst, LaTeX, PDFMiner, BeautifulSoup4, WeasyPrint, Calibre |

---

## Python SDK and the `fleetctl` CLI

A Python client and command-line utility live in `sdk/python`. They are not on
PyPI yet, so install from the repository:

```bash
pip install -e ./sdk/python
```

### Python
```python
from agentfleet import FleetClient

# Connect to orchestrator
fleet = FleetClient("http://localhost:8080", token="your-api-token")

# Deploy bot archetype and run goal
bot = fleet.deploy_bot("cyber_ops", name="security-auditor")
task = bot.run("Run SAST vulnerability scan and compile CVSS briefing", wait=True)
print(f"Result: {task.result}")

# Launch multi-agent swarm
swarm = fleet.launch_swarm("Fintech Audit", "Full-Stack + QA + CyberSec collaborative team")

# Speech through the TTS sidecar
fleet.speak("Task execution finished.", voice="shadow")
```

### Command line
```bash
# Run comprehensive platform diagnostics
fleetctl diagnostics

# Deploy specialized bot
fleetctl deploy --archetype cyber_ops --name "nightly-scanner"

# Run task and stream live execution
fleetctl run <bot_id> "Refactor backend authentication and run tests" --wait

# Multi-agent swarms & voice synthesis
fleetctl swarm list
fleetctl voice speak "All systems operational." --voice shadow
```

---

## Hardware profiles and isolation tiers

AgentFleet ships with four pre-configured hardware tiers:

| Tier | vCPU | Memory | Disk | GPU | Primary Use Case |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `micro` | 1.0 | 2 GB | 15 GB | No | Headless CLI, file processing, light terminal scripts |
| `standard` | 2.0 | 4 GB | 30 GB | No | Web browsing, form entry, SaaS navigation, documentation |
| `power-user` | 4.0 | 8 GB | 60 GB | No | Multi-window desktop workflows, heavy browser automation |
| `developer-heavy` | 8.0 | 16 GB | 150 GB | Requested | Compiling large codebases, game engines, SWE-bench tasks |

These are the values in [`backend/internal/fleet/tiers.go`](backend/internal/fleet/tiers.go); every field can be
overridden per instance. Two caveats worth stating rather than discovering:

- **GPU is "requested", not guaranteed.** `developer-heavy` asks Docker for an
  NVIDIA device. On a host without the container toolkit — including Docker
  Desktop on macOS and Windows — the request is ignored and you get a CPU-only
  sandbox.
- **Disk limits need overlay2 on XFS with pquota.** Anywhere else Docker rejects
  the quota and the orchestrator provisions without it, logging that it did.

---

## What it looks like

Screenshots of the running console, regenerated with `make screenshots`.

<p align="center">
  <img src="docs/images/fleet-dark-amber.png" alt="Fleet dashboard" width="900">
</p>

<p align="center"><em>The fleet: every machine, its hardware envelope, and what its agent is doing right now.</em></p>

<table>
  <tr>
    <td width="50%"><img src="docs/images/launch-dark-amber.png" alt="Launch an agent"></td>
    <td width="50%"><img src="docs/images/alerts-dark-amber.png" alt="Resolution centre"></td>
  </tr>
  <tr>
    <td align="center"><strong>Launch an agent</strong><br><sub>Describe the task; it provisions the machine and starts the agent in one action.</sub></td>
    <td align="center"><strong>Resolution centre</strong><br><sub>Everything an agent is blocked on, with the screen at the moment it stopped.</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/images/engines-light-blue.png" alt="AI engines"></td>
    <td width="50%"><img src="docs/images/skills-light-blue.png" alt="Skill timeline editor"></td>
  </tr>
  <tr>
    <td align="center"><strong>AI engines</strong><br><sub>An ordered fallback chain across local and cloud providers.</sub></td>
    <td align="center"><strong>Skill editor</strong><br><sub>Prune a recorded demonstration into the procedure you meant to show.</sub></td>
  </tr>
</table>

### Ten themes

Light and dark, each in five accents, switchable without a reload — and carried
identically into the Flutter companion app.

<table>
  <tr>
    <td><img src="docs/images/theme-dark-amber.png" alt="Dark amber"></td>
    <td><img src="docs/images/theme-dark-blue.png" alt="Dark blue"></td>
    <td><img src="docs/images/theme-dark-purple.png" alt="Dark purple"></td>
  </tr>
  <tr>
    <td><img src="docs/images/theme-light-green.png" alt="Light green"></td>
    <td><img src="docs/images/theme-light-red.png" alt="Light red"></td>
    <td><img src="docs/images/theme-light-blue.png" alt="Light blue"></td>
  </tr>
</table>

More in the [interface gallery](docs/UI.md).

## Quick start

You need Docker with Compose v2, and `make`. Everything else runs in containers.

```bash
git clone https://github.com/BryantVanOrden/AgentFleet.git
cd AgentFleet

make doctor   # Docker, RAM, ports, model endpoints
make env      # writes .env with fresh JWT_SECRET and MASTER_KEY, and your docker GID
make up       # builds the sandbox image, then starts the stack
```

Use the `make` path rather than `scripts/quickstart.sh`. The script does not
compute `DOCKER_GID`, does not build the sandbox image, and writes two keys the
orchestrator does not read — so the first bot you try to launch will fail. It
also ships without the executable bit set, so it needs `bash scripts/quickstart.sh`
rather than `./scripts/quickstart.sh`.

The first `make up` builds the TTS image, which is Torch-based and takes a while.
That is expected.

Then:

1. Open the admin console at <http://localhost:8081>. The API is on 8080 and
   serves no UI.
2. Create the first administrator. The bootstrap endpoint is available only while
   there are no users.
3. Under **AI Connections**, add at least one model provider — an API key, an
   in-app sign-in, or a local Ollama endpoint.
4. Launch an agent.

On Windows, `scripts/quickstart.ps1` exists but carries the same gaps; running
the three `make` targets under WSL or Git Bash is the better path. See
[Platforms](docs/PLATFORMS.md).

---

## Smoke tests

```bash
# Basic smoke test: provisions sandbox, verifies observation, injects input, checks screen dhash change
make smoke

# Full end-to-end agent smoke test with real autonomous model execution
make smoke-agent
```

---

## Directory layout

| Directory | Description |
| :--- | :--- |
| [`backend/`](backend/) | High-performance Go orchestrator (fleet supervisor, agent perceive-decide-act loop, model gateway, vault, SQLite/Postgres persistence, WebSocket event bus) |
| [`sandbox/`](sandbox/) | Ubuntu desktop container image (XFCE, Xvfb, AT-SPI accessibility bus, x11vnc, noVNC proxy, and the `agentd` daemon) |
| [`admin/`](admin/) | React 19 + TypeScript + Vite + Tailwind CSS operator console (fleet dashboard, live VNC streams, skill timeline editor, AI refinement studio) |
| [`mobile/`](mobile/) | Flutter companion app (real-time desktop streaming, interactive agent chat, resolution centre, FCM/APNs push notifications) |
| [`scripts/`](scripts/) | Preflight check (`doctor.sh`), end-to-end test (`smoke.sh`), bootstrap scripts, and the screenshot generator |
| [`sdk/`](sdk/) | Python client library and the `fleetctl` CLI |
| [`tts/`](tts/) | CPU text-to-speech sidecar |
| [`docs/`](docs/) | In-depth technical guides, architecture specifications, and security model |

---

## The perceive-decide-act loop

```
1. Observe   ──► agentd captures a WebP frame, the active window title, the AT-SPI
                 tree, and a perceptual hash of the screen.
2. Guard     ──► If the hash is unchanged across AGENT_STALL_THRESHOLD actions
                 (3 by default), the run parks and an alert goes to the operator.
3. Prompt    ──► Goal, parameters, SKILL.md, peer messages, the badged screenshot
                 with its Set-of-Marks element list, and recent history.
4. Decide    ──► The vision role of the bot's chain returns one JSON action.
5. Act       ──► Coordinates are mapped from the model's convention into desktop
                 pixels — or skipped entirely if the model answered with a mark —
                 and executed through agentd.
6. Persist   ──► Screenshot, duration, token counts and outcome into the audit
                 trail.
7. Refine    ──► On success only, and only if the task set auto-refine or carries
                 a skill: the trace goes to the refine role, which rewrites the
                 skill. A failed run is not analysed.
```

---

## Available commands

`make help` lists everything. The ones you will use:

```bash
make doctor           # Check Docker, RAM, ports, secrets and model availability
make env              # Create .env from the example, generate secrets, set DOCKER_GID
make sandbox          # Build the sandbox desktop image on its own
make up               # Build the sandbox image and start the whole stack
make down             # Stop the stack (sandboxes are left running)
make nuke             # Stop everything and delete all data
make logs             # Tail orchestrator logs
make psql             # Open a database shell
make smoke            # End-to-end test against a running stack
make smoke-agent      # The same, plus a real autonomous task against a real model
make clean-sandboxes  # Destroy every sandbox container this platform created
make test             # Go, Python agentd, Python SDK, admin build, flutter analyze
make screenshots      # Recapture the documentation screenshots
```

`make test` needs Go, Python, Node and Flutter all present.

---

## Documentation

* 📓 [**Changelog**](CHANGELOG.md) — What changed in each release.
* 🛡️ [**Security Review**](docs/SECURITY-REVIEW.md) — A point-in-time adversarial audit. Several findings have since been addressed; the document says which.
* ♿ [**Accessibility & Contrast**](docs/ACCESSIBILITY.md) — WCAG 2.1 AA contrast ratios measured across all ten themes.
* 📱 [**Flutter Companion Guide**](docs/FLUTTER_CROSS_PLATFORM_GUIDE.md) — Step-by-step instructions for Linux, Windows, Apple macOS/iOS, and Android runners.
* 💻 [**Platform Guide**](docs/PLATFORMS.md) — Running on Linux, macOS and Windows, and what is genuinely not portable.
* 🖼️ [**Interface Gallery**](docs/UI.md) — Every screen, all ten themes, and how the theming is built.
* 🧠 [**Agent Philosophy**](docs/AGENT_PHILOSOPHY.md) — How the work of driving a desktop is split across models, what ships, and what is only intended.
* 📐 [**Architecture Guide**](docs/ARCHITECTURE.md) — Detailed component design, data flow, schema, and networking model.
* 🔌 [**REST & WebSocket API Reference**](docs/API.md) — Complete endpoint specifications, request/response schemas, and real-time events.
* 🎓 [**Skills & Continual Refinement**](docs/SKILLS_AND_REFINEMENT.md) — Learn-by-demonstration recorder, AT-SPI compilation, and AI self-healing workflows.
* 🔒 [**Security & Threat Model**](docs/SECURITY.md) — Sandboxing guarantees, egress firewall rules, credential sealing, and prompt injection defenses.
* 🛠️ [**Developer Guide**](docs/DEVELOPER_GUIDE.md) — Local development workflow, running tests, and building mobile/admin frontends.
* 🗺️ [**Roadmap**](docs/ROADMAP.md) — Current capabilities and upcoming milestones.

---

## License

AgentFleet is distributed under the **[PolyForm Noncommercial License 1.0.0](LICENSE)**.

* **Non-Commercial Use**: Free for personal, research, academic, and non-commercial evaluation use.
* **Commercial & Enterprise Use**: For commercial deployments, SaaS integration, or commercial redistributions, a commercial license is required. Contact **Bryant VanOrden** (`supermanismebvo123@gmail.com`) for enterprise terms.

---

## Supporting the project

If AgentFleet is useful to you, you can support development with XRP or Bitcoin.

<div align="center">

| 🪙 **XRP (Ripple)** | ₿ **Bitcoin (BTC)** |
| :---: | :---: |
| <img src="docs/assets/xrp_qr.png" alt="XRP QR Code" width="160" /> | <img src="docs/assets/btc_qr.png" alt="BTC QR Code" width="160" /> |
| **Address:**<br><code>rf82s1CDagppvM6ATqc1nSrL6GackzHJrm</code> | **Address:**<br><code>bc1qvre807vxh08puxwc2z5adnm59tta7v5mqmky45</code> |
| **Destination Tag / Memo (Required):**<br><code>796343731</code> | *No memo required* |

</div>

> [!IMPORTANT]
> **A destination tag is required** when sending XRP to this deposit address (`796343731`). Without it the transfer will not be credited.

