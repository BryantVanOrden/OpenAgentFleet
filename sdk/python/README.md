# OpenAgentFleet Python SDK & `fleetctl` CLI

Official Python SDK and command-line management client for
[**OpenAgentFleet**](https://github.com/BryantVanOrden/OpenAgentFleet) — the
self-hosted platform for autonomous computer-use agents, where every agent
gets its own disposable Linux desktop that streams live into your browser.

---

## 📦 Installation

```bash
pip install open-agent-fleet
```

That gives you the `agentfleet` package and the `fleetctl` CLI. To work from
source instead:

```bash
git clone https://github.com/BryantVanOrden/OpenAgentFleet.git
pip install -e OpenAgentFleet/sdk/python
```

The SDK is a client — it talks to an OpenAgentFleet orchestrator. To run one,
see the [main README](https://github.com/BryantVanOrden/OpenAgentFleet#readme)
(`make up` is the whole quickstart).

---

## ⚡ Quickstart (Python SDK)

```python
from agentfleet import FleetClient

# Connect to your local or remote orchestrator
fleet = FleetClient("http://localhost:8080", token="your-api-token")

# 1. Deploy a specialized bot archetype
bot = fleet.deploy_bot("cyber_ops", name="security-auditor")
print(f"Spawned {bot.name} (ID: {bot.id})")

# 2. Run an autonomous goal
task = bot.run("Run SAST vulnerability scan and compile CVSS briefing", wait=True)
print(f"Task status: {task.state}")
print(f"Result: {task.result}")

# 3. Launch a collaborative multi-agent swarm
swarm = fleet.launch_swarm(
    name="Fintech Mobile Audit",
    mission="Full-Stack Dev + QA Tester + CyberSec PenTester collaborative sweep",
)

# 4. Pocket TTS Voice Co-Pilot
fleet.speak("All tasks completed successfully, operator.", voice="shadow")
```

### Tickets and the org chart

Work is tickets: one assignee, the ticket it exists for, and what it waits
on. Agents report to each other in an org chart. A ticket starts as soon as
its blockers are done and its assignee is free.

```python
builder = fleet.get_bot("…builder id…")
design = fleet.create_ticket("Design the pricing page", assignee_id=builder.id)
build = fleet.create_ticket("Build it", assignee_id=builder.id,
                            blocked_by=[design["ref"]], reviewer_id="…checker id…")

fleet.ticket(build["ref"])                  # ancestry, blockers, comments, runs
fleet.comment_ticket(build["ref"], "Use the brand colours")
fleet.reopen_ticket(build["ref"], "The mobile layout overflows")
fleet.update_ticket(build["ref"], status="cancelled")   # also cancels its open parts

chart = fleet.org()                         # every agent, who it reports to, what it is on
fleet.set_profile(builder.id, title="Lead", capabilities="Full-stack web apps")

template = fleet.export_fleet()             # agents, titles, reporting lines; no secrets
fleet.import_fleet(template, dry_run=True)
```

### Claude Code, Codex and Hermes beside the desktops

```python
pc = next(d for d in fleet.devices() if "claude_code" in d["runtimes"])
fleet.add_external_agent("Claude", "claude_code",
                         {"device_id": pc["id"], "cwd": pc["roots"][0], "model": "sonnet", "autonomy": "edits"},
                         reports_to=builder.id, title="Staff engineer")
fleet.add_external_agent("Research", "webhook", {"url": "https://agents.example/run"}, token="…")
```

---

## 🖥️ Connect this PC (`fleetctl host`)

`fleetctl host` connects your machine to the fleet for two things:

- **Oaf sessions.** The fleet chat's sessions can act on your own machine,
  Claude Code-style: a session is bound to a device and a working folder,
  and Oaf reads and edits files there, runs commands there, and shows every
  step in the thread.
- **Agents that live on your PC.** If Claude Code (`claude`), Codex
  (`codex`) or Hermes (`hermes`) is installed, the host says so, and you can
  add them to the fleet as agents that work in a folder here. They take
  tickets like any desktop bot, sit in the org chart, and report cost.

```bash
fleetctl login
fleetctl host --root ~/Code/my-project          # expose one folder
fleetctl host --root ~/Code -r ~/Notes --yes    # several folders, no prompts
```

- Paths outside the exposed folders are refused on the device, whatever the
  orchestrator asks for.
- A shell command or a file write asks in this terminal first (`allow? [y/N]`),
  like Claude Code's permission prompt. `--yes` auto-approves for a session you
  trust.
- The machine polls out; nothing connects in. Ctrl+C disconnects it and Oaf
  loses the machine.
- Then, in the console or the app: open a session → device → pick this PC and
  a folder under one of its roots.

### Agents on your PC

```bash
fleetctl host --root ~/Code/app                  # keep this running
fleetctl devices                                 # the PC, and the agent CLIs it found
fleetctl agent add Claude --kind claude_code --folder ~/Code/app --model sonnet --reports-to Builder
fleetctl ticket new "Fix the failing login test" --to Claude --review-by Checker
```

- Each run starts the CLI in the agent's folder (`claude --print`,
  `codex exec`, `hermes chat -q`), streams its progress to the console, and
  can be stopped from there. A run on the same ticket resumes the CLI's
  session.
- `--autonomy edits` (the default) lets it change files but not run
  commands; `full` lets it do anything inside its folder. Each run asks in
  this terminal first unless the host was started with `--yes`.
- Claude Code runs without your `~/.claude` user settings, so an
  `additionalDirectories` entry of yours cannot widen its folder; the
  folder's own `.claude` settings still apply
  (`AGENTFLEET_CLAUDE_USER_SETTINGS=1` keeps yours).
- The text files a run creates or changes, and the ones its report names,
  are sent back and shared with the fleet under their path in the folder,
  so a desktop colleague can review them with `read_work`.
- Each run gets `$AGENTFLEET_RUN_TOKEN`, `$AGENTFLEET_TASK_ID`,
  `$AGENTFLEET_TICKET` and `$AGENTFLEET_API_URL`: enough to comment on its
  ticket, hand part of it to a colleague and finish, and nothing else.

## 🛠️ Command-Line Interface (`fleetctl`)

The `fleetctl` command-line utility provides full administrative control, live telemetry, and system diagnostics:

```bash
# 1. Configuration & Authentication
fleetctl config set-url http://localhost:8080
fleetctl login admin@example.com

# 2. Comprehensive Diagnostics & Health Check
fleetctl diagnostics

# 3. List and Inspect Instances
fleetctl list
fleetctl inspect <instance_id>

# 4. Deploy Bots
fleetctl deploy --archetype cyber_ops --name "nightly-scanner"

# 5. Execute Tasks & Stream Live Execution
fleetctl run <instance_id> "Refactor backend authentication and run unit tests" --wait

# 6. Multi-Agent Swarm Mission Control
fleetctl swarm list
fleetctl swarm launch --name "Security Sweep" --mission "Penetration test staging endpoints"
fleetctl swarm watch <swarm_id>

# 7. Real-Time Pocket TTS Voice
fleetctl voice list
fleetctl voice speak "System operational and standing by." --voice shadow

# 8. Webhook & Autopilot Sinks
fleetctl webhooks list
fleetctl webhooks trigger github-pr-sync --data '{"pr": 42}'

# 9. Tickets
fleetctl tickets                                  # open work, who has it, what it cost
fleetctl tickets --status blocked -a Builder
fleetctl ticket new "Ship the pricing page" --to Builder --verify-by Checker --budget 5
fleetctl ticket show T-12
fleetctl ticket comment T-12 "Use the brand colours"
fleetctl ticket reopen T-12 "The mobile layout overflows"
fleetctl ticket move T-12 cancelled                # also cancels its open parts and checks

# 10. The org chart and external agents
fleetctl org                                      # who reports to whom, and what each is on
fleetctl agent set Claude --reports-to Builder --title "Staff engineer" --budget 20
fleetctl agent add Gateway --kind openclaw --url wss://claw.example --ask-token
fleetctl agent add Research --kind webhook --url https://agents.example/run --token-env RESEARCH_TOKEN

# 11. Fleet templates
fleetctl fleet export -o fleet.json               # agents, titles, reporting lines, budgets; no secrets
fleetctl fleet import fleet.json --dry-run
```

The design behind tickets, the org chart and external agents is in
[docs/ORG-AND-TICKETS.md](https://github.com/BryantVanOrden/OpenAgentFleet/blob/master/docs/ORG-AND-TICKETS.md).
