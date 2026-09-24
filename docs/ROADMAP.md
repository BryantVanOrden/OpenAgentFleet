# Roadmap

## Built

**Phase 1 — sandbox and remote view.** Ubuntu + XFCE + Xvfb + x11vnc + noVNC
image; Docker driver over the engine REST API; four hardware tiers with cgroup
enforcement; instance lifecycle (provision, start, stop, freeze, destroy) with
reconciliation against what the engine actually reports; authenticated desktop
proxy with WebSocket tunnelling.

**Phase 2 — connectors and the agent loop.** Ollama, OpenAI, Anthropic, Gemini
and any OpenAI-compatible gateway behind one interface, arranged as a
priority-ordered fallback chain with a penalty box for failing endpoints;
`agentd` observe/act daemon; the perceive → decide → act loop with a closed
action vocabulary, strict JSON parsing, stall detection on a perceptual hash, and
step-budget enforcement; full audit trail with per-step screenshots.

**Phase 3 — record and replay.** X RECORD input capture paired with AT-SPI
element context; compilation into semantic steps and a rendered `SKILL.md`;
timeline editor in the console for pruning steps and parameterising typed values.

**Phase 4 — companion app and alerts.** Flutter app with fleet dashboard, live
stats, embedded desktop stream, agent chat with a deliberate ask/run split,
and the resolution centre; FCM and APNs dispatch with time-sensitive delivery
for critical alerts; human-in-the-loop escalation that parks a task until someone
answers.

**Cross-cutting.** AES-256-GCM credential vault; nftables egress policy;
artifact storage on filesystem or S3/MinIO; in-process event bus with WebSocket
fan-out.

**Phase 5 — organisations and access.** RBAC grew past the three
platform roles into organisations (departments) with their own members and org
roles, plus per-bot permission grants and an endpoint that reports a caller's
effective permissions rather than making each client re-derive them.

**Phase 5 — model combinations.** Role-based routing: a named mapping
from `vision` / `reasoning` / `chat` / `summarize` / `refine` to providers, usable
anywhere a single provider was usable, so a bot's fallback chain can mix the two.

**Phase 5 — pipelines, comms and custom tools.** Multi-bot DAG
pipelines that survive an orchestrator restart; conversations with membership and
sender identity; per-bot named chat sessions; and archetype tool selection with
operator-supplied custom tool recipes.

**Phase 6 — the chat is an agent.** The home screen of both clients is one
conversation with the fleet plus named **sessions with Oaf**, each bound to
one of the operator's own devices and a working folder: `fleetctl host
--root <folder>` attaches a PC (approval prompt in the terminal for commands
and writes), the phone attaches from Settings. Oaf reads and edits files
there, runs commands, runs any fleet command, asks bots and hands the fleet
work, with every tool call shown in the thread; files and images attach by
pick, drop or paste; `/goal` keeps working with check-ins, `/loop` repeats.
Voice lives in the chat. First run is a one-button setup card that finds a
local model and measures its vision; text-only models still drive from the
element marks and accessibility tree. Provisioning answers `202` and boots
in the background.

**Phase 7 — tickets, the org chart and external agents.** Work is durable
tickets instead of hand-offs inferred from chat: one owner, a parent, the
tickets it waits on, a chain up to the request that every brief quotes, and
reviews that end in a pass or fail verdict. Blocked work goes up the org
chart; every open ticket is checked for a next move; verifiers reopen what
is not finished; monthly budgets hold an agent at its ceiling; low-trust
agents are fenced. Agents report to each other, drawn as a chart in both
clients. Claude Code, Codex and Hermes on the operator's PC (through
`fleetctl host`), OpenClaw gateways and webhooks sit in the same chart and
take the same tickets, and what a PC agent makes is shared to the catalog
for its desktop colleagues. Fleets export and import as templates. Design:
[ORG-AND-TICKETS.md](ORG-AND-TICKETS.md).

## Next

### Phase 5 — heavy workloads and real isolation

- ~~QEMU/KVM driver behind the existing `Driver` interface~~ — **built**, with
  deliberate boundaries. `"driver": "qemu"` on instance create boots the
  sandbox as a real VM: the guest disk is converted from the container image
  at build time (`make sandbox-vm`), so the VM runs byte-for-byte the same
  agentd and desktop and cannot drift. KVM-accelerated when the host has
  `/dev/kvm`, TCG software emulation otherwise (same guest, slow boot — the
  runner logs which). Copy-on-write already: each boot runs a qcow2 overlay
  over the pristine base disk. Egress policies are enforced on this driver —
  in the runner's netns, where QEMU's SLIRP sockets originate every guest
  connection and where no code path from inside the guest can flush the
  rules; the same `egress.sh` as the container tier, applied before QEMU
  starts, fail-closed. Still ahead, in honesty order:
  - **`vfio-pci` GPU passthrough** — the VM tier has no GPU story yet.
  - **Whole-machine snapshot/rewind** — workspace-level `snapshot`/`rollback`
    still ship on both drivers; VM disk snapshots do not.

### Phase 6 — WebRTC streaming

noVNC over WebSocket is reliable and roughly 200–400 ms round trip. A
`selkies-gstreamer` pipeline gets that under 100 ms with hardware encoding, which
is the difference between "watchable" and "usable" for manual takeover on a
phone. The signalling port is already reserved in the image.

### Phase 7 — multi-node

The two things standing in the way, both deliberately narrow:

- Replace the in-process bus with Redis pub/sub (three methods).
- Give the fleet manager a scheduler that can place an instance on one of several
  hosts and a lease so two orchestrators do not both drive the same task.

### Phase 8 — better perception

- OCR fallback for windows that expose nothing to AT-SPI — canvas apps, games,
  video — so `wait_for` and `assert` are not blind there.
- ~~Set-of-marks overlays~~ — **built.** Interactive elements are badged on the
  frame itself (`sandbox/agentd/som.py`) and the model answers with a mark rather
  than a coordinate. Also built alongside it, and not previously on this list:
  automatic coordinate-space calibration, because vision models disagree about
  whether a coordinate means a pixel or a 0–1000 fraction and will not say
  which.
- Frame diffing between steps, so the model is told what changed rather than
  having to work it out from two full screenshots.

### Phase 9 — operations

- Session video export (the frames are already stored; they need muxing).
- ~~Cost accounting per task and per instance~~ — **built.** Token counts are
  costed and exposed at `/api/telemetry/financials` and `/api/telemetry/records`.
- ~~Scheduled and event-triggered runs~~ — **built.** Cron triggers and webhook
  ingress, both persisted across a restart.
- Skill versioning with a diff view, so an edit to a recording is reviewable.

### Pipelines, from here

- ~~**Parallel execution of independent nodes.**~~ — **built.** Every stage whose
  dependencies have settled runs concurrently, bounded by the pipeline's
  `max_parallel` (default 4, because each stage starts a real task on a real
  desktop).
- ~~**Conditional edges.**~~ — **built.** `always`, `success`, `failure`,
  `contains:`, `not_contains:`, `equals:` and `matches:`, validated at save so a
  misspelling is a 400 rather than a branch that silently never fires. A stage
  whose conditions are not met is skipped rather than failed, and skipping
  propagates downstream.
- ~~**A graph builder.**~~ — **built.** The console's create button used to post a
  fixed three-node pipeline and everything else was a viewer, so an existing
  pipeline could not be edited at all.

What is left:

- ~~**Runs do not survive a restart.**~~ — **built.** Runs persist on every node
  transition, and a run interrupted mid-flight resumes at boot from its last
  settled node: settled results and already-decided branches are kept, and the
  node that was mid-flight is re-dispatched (its half-finished task cannot be
  rejoined — re-running work beats losing it).
- **No fan-out over a collection.** A stage runs once. "Run this stage for each
  item the previous stage returned" needs a map construct the graph has no way to
  express.

### Tickets and external agents, from here

- **Live coverage of every adapter.** Claude Code has been run end to end
  against a real fleet. Codex and Hermes are tested against fakes that print
  what the real CLIs print, and OpenClaw against a fake gateway speaking
  protocol 4; each wants a live soak.
- **Missions and pipelines on tickets.** `/mission` and pipelines still keep
  their own state; filing their stages as tickets would put them on the same
  board, budgets and verifiers.

## Deliberately not planned

- **A company run by a CEO agent.** The org chart is for arranging agents and
  routing their work, and it ends at a person: blocked work with no manager
  goes to the operator, not to an agent with authority over the rest.

- **A hosted multi-tenant service.** The security model assumes the operator owns
  the host. Multi-tenancy would need a different isolation story from the ground
  up, starting with never handing anything the Docker socket.
- **Fully unattended operation with no escalation path.** The `ask_human`
  mechanism is not a limitation to engineer away; it is the safety property.
