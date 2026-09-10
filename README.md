<h1 align="center">OpenAgentFleet</h1>

<p align="center">
  <img src="https://img.shields.io/badge/Self--Hosted-100%25-8B5CF6?style=for-the-badge" alt="Self-hosted" />
</p>

<table align="center">
  <tr>
    <td align="right" valign="middle">
      <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go" /></a><br /><br />
      <a href="https://python.org"><img src="https://img.shields.io/badge/Python-3.11+-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python" /></a>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;<br /><br />
      <a href="https://flutter.dev"><img src="https://img.shields.io/badge/Flutter-3.27+-02569B?style=for-the-badge&logo=flutter&logoColor=white" alt="Flutter" /></a>
    </td>
    <td align="center" valign="middle">
      <img src="docs/images/oaf_mascot_clean.png" alt="OpenAgentFleet Mascot (Oaf)" width="150" />
    </td>
    <td align="left" valign="middle">
      <a href="https://react.dev"><img src="https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black" alt="React" /></a><br /><br />
      &nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;<a href="https://docker.com"><img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker" /></a><br /><br />
      <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License" /></a>
    </td>
  </tr>
</table>

<p align="center">
  <img src="https://img.shields.io/badge/Linux_Desktop-Sandboxed_XFCE-E95420?style=for-the-badge&logo=linux&logoColor=white" alt="Linux Desktop" />
</p>

<p align="center">
  <strong>Self-hosted computer-use agents. Every agent gets its own Linux desktop.</strong><br>
  Watch them work live in your browser — and click into the screen to take over at any moment.<br>
  Your hardware, your models, phone alerts when an agent needs a human.
</p>

<p align="center">
  <img src="docs/demo/demo.gif" alt="A real agent run, uncut: the goal is typed into the console, a local vision model opens Firefox on its own sandboxed desktop, navigates to Hacker News, and reports the #1 story" width="920">
</p>

<p align="center"><em>A real run, recorded uncut from the console and played at 8× speed: the goal is typed, a <strong>local</strong> vision model<br>
(qwen3.8-27b over Ollama — no cloud, no API key) opens Firefox on its own desktop with <strong>shell access disabled</strong>,<br>
navigates to Hacker News, reads the #1 story, and reports it. Sixteen GUI actions, zero cuts —<br>
the real-time recording is attached to <a href="https://github.com/BryantVanOrden/OpenAgentFleet/releases/tag/v1.0.0">the release</a>.</em></p>

<p align="center">
  <img src="docs/images/desktop-live-dark.png" alt="An agent's own desktop, streaming live into the console: Firefox open on its sandboxed XFCE desktop, with the task panel and teach-by-demonstration controls beside it" width="920">
</p>

<p align="center"><em>The same view, still: a real XFCE session in a hardened container, streaming live.<br>
It is <strong>interactive</strong> — click the stream and you are driving its mouse and keyboard; let go and the agent carries on.<br>
Do a task yourself once with the recorder on, and it compiles into a skill the agent can repeat.</em></p>

---

## Why this exists

Most agent frameworks give a model some tools. OpenAgentFleet gives each model a
**computer** — a disposable Linux desktop it perceives through screenshots and
an accessibility tree, and drives with a mouse and keyboard, exactly the way
you would. That one decision is where everything else comes from:

- **You can see what it's doing.** Every agent's desktop streams into the
  console over noVNC. No log-file archaeology — you watch the cursor move.
- **You can take over, any time.** The stream is interactive for operators.
  Agent stuck on a weird dialog? Click, fix it, let go. Want to teach it the
  task instead? Do it once with the recorder on — keystrokes, clicks and the
  accessible element behind each one become a `SKILL.md` the agent follows.
- **It runs on your machines, against your models.** Point it at Ollama on
  your own GPU and nothing ever leaves the building; a run costs $0.00. Cloud
  models plug in behind the same interface with per-turn cost accounting.
- **It escalates like an employee, not a cron job.** A CAPTCHA, an MFA prompt,
  a payment screen — the agent parks the task and pings your phone with the
  screen frozen at that moment. Answer from the lock screen; it resumes.

<table align="center">
<tr>
<td>

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

</td>
</tr>
</table>

---

## What it does

- **One chat with the whole fleet.** The home screen of both the console and the
  phone app is a single conversation with every agent. Say what you want done and
  the running bots divide it between themselves — each claims a part, sees what
  colleagues already took, hands work on, and reports back. Name a bot and it
  answers first. Type `/` for commands (`/bots`, `/task @bot …`, `/mission …`,
  `/alerts`, `/ack`, `/run`, `/help`) — parsed and executed server-side, so both
  clients offer exactly the same verbs. Missions are not a separate screen:
  `/mission <goal>` gives every free bot a role from its archetype, they plan
  first, then execute, and their artifacts are peer-reviewed in the same chat.
- **The chat is an agent, with sessions.** Beside the fleet channel, the home
  screen keeps *sessions*: each is a named conversation with Oaf scoped, like a
  Claude Code session, to a device and a working folder. Attach your PC with
  `fleetctl host --root <folder>` (it asks in your terminal before running a
  command or writing a file), or the phone from Settings. Oaf then reads and
  edits files there, runs commands there, runs any fleet command, asks bots
  questions and hands the fleet work — every tool call shows in the thread as
  it happens. Drop, pick or paste files and images and Oaf sees them. `/goal`
  keeps it working until something is reached; `/loop 30m …` repeats. Voice
  mode listens, sends and reads the answer back. Threads the bots keep between
  themselves are listed right there too.
- **First run does itself.** Until a model is connected, a bot exists and
  something has been asked of it, the chat leads with one card and one button.
  *Find my model* scans the machine for Ollama, LM Studio, llama.cpp or vLLM
  (or takes an address), picks the best model, measures whether it can see
  screenshots by showing it a red square, and registers it. *Create a bot*
  provisions a desktop. Then you type what you want. The same `/setup` and
  `/new` verbs are there for people who prefer typing.
- **Runs that take as long as they take.** A step budget is a checkpoint, not a
  kill switch: when a window closes with the goal unfinished the agent writes a
  handover note to itself and carries on in a fresh window, for hours or days,
  until it has *seen* the result work on screen. Each window is its own task —
  steps and screenshots stay browsable — and every boundary files a progress
  alert you can cancel from. In a private chat, a bot that needs a colleague
  asks them directly (`ASK Builder: …`) and the answer lands in fleet comms.
- **Sandboxed desktops.** Each bot is a container running Ubuntu with XFCE, under
  cgroup limits on CPU and memory with swap disabled, an optional GPU device
  request, and an optional nftables egress policy. See [Security](docs/SECURITY.md)
  for what that isolation is and is not worth.
- **A perceive-decide-act loop.** Each turn the agent gets a WebP frame with
  Set-of-Marks badges over interactive elements, the AT-SPI accessibility tree,
  and its own history; it replies with one JSON action. Coordinate conventions
  are calibrated per model automatically, because vision models disagree about
  whether a coordinate is a pixel or a fraction and will not tell you which. A
  chain with no sighted model still drives: the screenshot is dropped and the
  agent works from the element marks and the tree, and is told so.
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
  loosely, so a typo or a split word still finds the right bot, and naming
  anybody keeps everybody else out of it. Finishing a
  part wakes whoever the next one belongs to and hands them what was produced;
  testing and reviewing wait for something to exist rather than starting on
  nothing. A review hands back to whoever built. Bounded at six rounds, because
  agents starting each other without a limit is the failure that costs money.
- **A shared work catalog.** Agents publish what they make for each other and
  for you: files to build on, workspaces to group a piece of work, and *apps* —
  one self-contained HTML document each — that the phone renders and runs from
  a tab in Vault. They reach it with `publish_work` and `read_work`, keyed by
  name, so improving a colleague's work is an edit that bumps its version
  rather than a second copy nobody notices. `read_work` also opens what it
  finds on the agent's own desktop, so a bot asked to test an app can run it
  rather than only read its source.
- **A file browser over the catalog.** The Vault tab walks folders with a
  breadcrumb, and you can create, rename, move, edit and delete anything in it.
  The editor colours HTML, CSS, JavaScript, JSON, Dart, Go, Python, shell, SQL,
  YAML and Markdown. Runnable items keep their tap for "play" and put the rest
  behind a long press, because a game is still a file.
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
- **Pipelines.** Multi-bot DAG workflows. Independent stages run in parallel
  (bounded per pipeline), and each dependency can be conditional on how the stage
  before it ended — `success`, `failure`, or a match against its result — so a
  graph can have an error branch that only fires on an error. A stage whose
  condition is not met is skipped rather than failed, and skipping propagates.
  Built and edited in the console.
- **Swarms.** A mission, the bots assigned to it, and a shared blackboard.
  Creating one starts a real task per member, each told the mission, its own role
  and its teammates. Publishing an artifact sends it out for peer review by every
  other member, and the mission completes when they have all signed off.
- **MCP.** A real client: JSON-RPC 2.0 over stdio (a child process) or
  Streamable HTTP (JSON or SSE responses), with the `initialize` handshake and
  paginated `tools/list`. Agents call these tools with `call_mcp`, and the tools
  available are listed in the system prompt. Registrations persist.
- **Triggers.** Webhook ingress on a per-webhook token, with the signature scheme
  the named sender actually uses — GitHub's `X-Hub-Signature-256`, Stripe's
  timestamped `Stripe-Signature` with a replay window — and a payload summary so
  an agent is told what happened rather than handed raw JSON. Plus a cron
  scheduler. Both persist across restarts.
- **Cost telemetry.** Prompt, completion and cached tokens, latency and a dollar
  estimate per call, from a hand-maintained price table. Cache reads are priced
  at the discounted rate. Durable, so the totals survive a deploy.
- **Archetype packages.** Export an archetype — persona, hardware profile,
  recorded skills, MCP registrations — as a portable `.agentfleet.yaml`, and
  install one someone sent you, optionally provisioning a bot from it.
  Credentials are never included in a package.
- **A phone app.** Flutter, on Android, iOS, macOS, Linux and Windows, with push
  alerts and interactive takeover on all five.

### Design limits, stated plainly

This project's habit is to say what a feature does not do in the same breath
as shipping it, so the boundaries live here rather than waiting to be
discovered. First, three capabilities whose design is worth knowing, because
each makes a deliberate trade:

- **Semantic memory ships by default.** The compose stack includes a small
  local embedding sidecar (model2vec `potion-base-8M`, ~30 MB, CPU-only,
  weights baked into the image so air-gapped fleets get it too), so a fleet
  with no embedding-capable provider — Anthropic-only, or none configured yet
  — still gets semantic recall rather than keyword matching. Configured
  providers that can embed are preferred for quality; `/api/memory/fleet`
  reports which scheme is live, and `EMBED_BASE_URL=off` selects the hashed
  keyword fallback deliberately. Search is an exact linear scan over the
  working set — brute force is what vector search *is* at this scale, with
  none of an approximate index's recall loss — sized at 50,000 records and
  benchmarked at that size (~55 ms per query on a modest 4-core box, against
  agent steps that each cost seconds of inference; the benchmark ships in
  `backend/internal/memory/engine_bench_test.go`).
- **The QEMU tier is a real virtual machine.** `"driver": "qemu"` on instance
  create boots the sandbox behind a hardware(-emulated) boundary — the guest
  disk is converted from the container image at build time (`make
  sandbox-vm`), so the VM runs byte-for-byte the same agentd and desktop as
  the container tier and cannot drift from it. KVM-accelerated when the host
  has `/dev/kvm`; TCG software emulation otherwise (same guest, slower boot,
  and the runner logs which it chose).
- **Egress policies on the VM tier are enforced where the guest can't reach
  them.** Every connection the guest opens leaves through QEMU's SLIRP
  sockets in the runner container's network namespace, dialled to the
  destination the guest asked for — so the same nftables program the
  container tier uses is applied there, before QEMU starts, fail-closed (if
  the policy can't be programmed, the VM doesn't boot). This placement is
  *stronger* than the container tier's: an agent with root inside the guest
  can flush its own tables all day and never touch the policy, and the guest
  is never even told a policy exists.

And the limits that remain, stated as limits:

- **Marathon runs are bounded only by what you set.** `AGENT_MARATHON_MAX_WINDOWS`
  defaults to 0 — unbounded, because that is what "until it is done" means — and
  the handover note between windows is written by the summarising model, so a
  weak summariser can lose detail across a very long run. The progress alert at
  every boundary is the operator's cancel handle; nothing stops a run for you.
- **No GPU on the VM tier.** `vfio-pci` passthrough needs IOMMU hardware this
  project's CI cannot exercise, and shipping untested code is against the
  point of this section — so it stays roadmap. The container tier's GPU
  request (with its no-runtime fallback) is unchanged.
- **Container-grade isolation remains the default.** The docker driver is the
  shipped security model, documented in [SECURITY.md](docs/SECURITY.md); the
  VM driver is the stronger boundary for workloads that warrant the boot cost.
- **Egress hostname rules resolve once, at policy time.** A CDN-backed host
  whose addresses rotate drifts out of an allow-list; use a CIDR or an egress
  proxy for those. `block_local` covers IPv4 only — the shipped networks are
  v4-only, and [SECURITY.md](docs/SECURITY.md) spells out when that matters.


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

A Python client and command-line utility, published on PyPI as
[`open-agent-fleet`](https://pypi.org/project/open-agent-fleet/):

```bash
pip install open-agent-fleet
```

Or from a checkout, `pip install -e ./sdk/python`.

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

# Let Oaf work on this machine, inside one folder, asking before it runs anything
fleetctl host --root ~/projects/site

# Multi-agent swarms & voice synthesis
fleetctl swarm list
fleetctl voice speak "All systems operational." --voice shadow
```

---

## Hardware profiles and isolation tiers

OpenAgentFleet ships with four pre-configured hardware tiers:

| Tier | vCPU | Memory | Disk | GPU | Primary Use Case |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `micro` | 1.0 | 2 GB | 15 GB | No | Headless CLI, file processing, light terminal scripts |
| `standard` | 2.0 | 4 GB | 30 GB | No | Web browsing, form entry, SaaS navigation, documentation |
| `power-user` | 4.0 | 8 GB | 60 GB | No | Multi-window desktop workflows, heavy browser automation |
| `developer-heavy` | 8.0 | 16 GB | 150 GB | Requested | Compiling large codebases, game engines, SWE-bench tasks |

These are the values in [`backend/internal/fleet/tiers.go`](backend/internal/fleet/tiers.go); every field can be
overridden per instance. They are what the tier *asks for*, and the orchestrator
reduces them to what the host can actually provide rather than failing. Four
caveats worth stating rather than discovering:

- **A tier larger than your host is clamped, not refused.** Asking Docker for
  more CPUs than the machine has is a hard error, so `developer-heavy` (8 vCPU)
  could not start at all on a 4-core laptop — after pulling the image. vCPU is
  now clamped to the host's core count and memory to 80% of host RAM, the
  reduction is logged, and the instance's stored profile shows what it actually
  got rather than what the tier wanted. A developer-heavy bot on a small machine
  is slower than intended and otherwise exactly what was asked for.
- **GPU is requested, and dropped if the host cannot provide one.**
  `developer-heavy` asks Docker for an NVIDIA device. This cannot be detected in
  advance — Docker Desktop registers the `nvidia` runtime whether or not any
  adapter exists, so the runtime list says yes and the prestart hook says no.
  The container is started, and if it fails specifically on the NVIDIA tooling
  the orchestrator retries once without the GPU and labels the instance
  `gpu_unavailable`. You get a CPU-only sandbox, which is what the tier is on a
  machine without a GPU anyway — its compilers do not need one.
- **`developer-heavy` uses a different image.** It runs
  `agentfleet/sandbox:latest-dev`, built by `make sandbox-dev`: the base desktop
  plus gcc, clang, Rust, Go, a JDK, ccache and the GPU loader hints. Build it
  before selecting the tier, or the instance stops on a missing image naming the
  command that fixes it.
- **Disk limits need overlay2 on XFS with pquota.** Anywhere else Docker rejects
  the quota and the orchestrator provisions without it, logging that it did.

---

## What it looks like

Real screenshots of a running fleet — console (`make screenshots`), live desktop
(`make hero-shot`), and the companion app (`make app-screenshots`).

<p align="center">
  <img src="docs/images/chat-dark-amber.png" alt="The fleet chat: one conversation with every agent, Oaf's system notes, and slash commands" width="900">
</p>

<p align="center"><em>The home screen is one chat with the whole fleet. Say what you want done and the bots divide it up; type <code>/</code> for commands; Oaf narrates what the platform did.</em></p>

<p align="center">
  <img src="docs/images/session-dark-amber.png" alt="A session with Oaf beside the fleet channel: sessions rail, device and folder header, and the composer with attachments and voice" width="900">
</p>

<p align="center"><em>Beside the channel, sessions: each is a chat with Oaf bound to a device and a folder — your PC through <code>fleetctl host</code>, or the phone — where it reads, edits, runs commands, asks bots and shows every step.</em></p>

<p align="center">
  <img src="docs/images/fleet-dark-amber.png" alt="Fleet dashboard" width="900">
</p>

<p align="center"><em>The fleet: every machine, its hardware envelope, and what its agent is doing right now.</em></p>

### The companion app

The same fleet from your pocket, on Android, iOS, macOS, Linux and Windows —
the same features as the console, down to the slash commands: the fleet chat,
live CPU/memory per bot, the shared vault, and the alerts that let you unblock
an agent from wherever you are. Oaf keeps you company.

<table>
  <tr>
    <td width="33%"><img src="docs/images/app-chat-dark.png" alt="The fleet chat on the phone — Oaf's notes, slash commands, one conversation with every agent"></td>
    <td width="33%"><img src="docs/images/app-fleet-dark.png" alt="Fleet on the phone"></td>
    <td width="33%"><img src="docs/images/app-alerts-dark.png" alt="Alerts on the phone"></td>
  </tr>
  <tr>
    <td align="center"><sub><strong>Chat</strong> — the whole fleet in one conversation, <code>/</code> for commands</sub></td>
    <td align="center"><sub><strong>Fleet</strong> — every bot, live meters</sub></td>
    <td align="center"><sub><strong>Alerts</strong> — what needs a human (nothing, ideally)</sub></td>
  </tr>
</table>

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
git clone https://github.com/BryantVanOrden/OpenAgentFleet.git
cd OpenAgentFleet

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
# Provisions a sandbox, verifies observation, injects input, checks the screen changed
make smoke

# The same, plus a real autonomous task against a real model
make smoke-agent

# The subsystems that were once stubs: registers a real MCP server over JSON-RPC
# and calls a tool on it, checks pipeline conditions and cycles are validated,
# checks a swarm refuses invented members, and signs a Stripe delivery the way
# Stripe signs it (and checks the old scheme is refused)
VERIFY_EMAIL=you@example.com VERIFY_PASSWORD=... make verify-features
```

`verify-features` exists because unit tests could not have caught what was wrong
with most of those: the MCP bridge passed every test it had while speaking no
protocol at all, and Stripe webhooks were rejected on 100% of real deliveries
while the generic HMAC tests stayed green. It talks to a running stack and starts
a real MCP server in a container.

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
make sandbox-dev      # Build the developer-heavy image (compilers, Rust, Go, GPU hints)
make up               # Build the sandbox image and start the whole stack
make down             # Stop the stack (sandboxes are left running)
make nuke             # Stop everything and delete all data
make logs             # Tail orchestrator logs
make psql             # Open a database shell
make smoke            # End-to-end test against a running stack
make smoke-agent      # The same, plus a real autonomous task against a real model
make verify-features  # MCP, pipelines, swarms, webhooks, memory, archetype packages
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

OpenAgentFleet is open source under the **[MIT License](LICENSE)**. Use it, fork it,
ship it — personally or commercially — with attribution.

---

## Supporting the project

If OpenAgentFleet is useful to you, you can support development with XRP or Bitcoin.

<div align="center">

| 🪙 **XRP (Ripple)** | ₿ **Bitcoin (BTC)** |
| :---: | :---: |
| <img src="docs/assets/xrp_qr.png" alt="XRP QR Code" width="160" /> | <img src="docs/assets/btc_qr.png" alt="BTC QR Code" width="160" /> |
| **Address:**<br><code>rf82s1CDagppvM6ATqc1nSrL6GackzHJrm</code> | **Address:**<br><code>bc1qvre807vxh08puxwc2z5adnm59tta7v5mqmky45</code> |
| **Destination Tag / Memo (Required):**<br><code>796343731</code> | *No memo required* |

</div>

> [!IMPORTANT]
> **A destination tag is required** when sending XRP to this deposit address (`796343731`). Without it the transfer will not be credited.

