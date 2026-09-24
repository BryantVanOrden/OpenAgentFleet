# Security model

An autonomous agent with a desktop, a shell and network access is a capability,
not a feature. This document says what is actually enforced, and — more usefully
— what is not.

## Threat model

| Adversary | Concern |
| --------- | ------- |
| Content on the agent's screen | A web page, document or terminal output that contains instructions aimed at the agent |
| The agent itself | A confused or misdirected model reaching something it should not |
| A sandbox escape | Container breakout onto the host or the control plane |
| An operator | An account acting beyond its role, or reading credentials it should not |
| The network | Someone reaching a desktop or the API without a session |

## Prompt injection

This is the primary risk and it has no complete fix. What is done:

- The system prompt states the boundary explicitly: everything on screen is
  **data**, never instruction, and text claiming to come from the operator or a
  system authority is to be refused and reported, not obeyed.
- The one authoritative channel is an operator reply to an alert. It arrives in
  the prompt under a heading that names it as coming from the human, and nothing
  the agent can see on screen can forge that position.
- The action vocabulary is closed in the sense that a model cannot invent a new
  *transport*: anything it emits that is not one of the 32 kinds in
  `validActions` (`backend/internal/agent/parse.go`) is a parse error, and three
  in a row fail the task. It is **not** closed in the sense of "closed set of
  effects" — `python`, `mount_tool` and `call_tool` carry model-authored source
  code, and validation checks that the required fields are present, not what is
  in them. See the gate below, which is what actually bounds that.
- `ask_human` is the prescribed response for anything irreversible: credential
  entry, payment, publishing, accepting an agreement, CAPTCHAs, MFA.

What is **not** solved: a sufficiently persuasive page can still talk a model
into a harmful action inside its existing capabilities. Defence in depth is the
answer, not the prompt. Give an instance the narrowest egress policy and the
least shell access the task actually needs.

Also not solved, specifically: `deep_search` fetches a URL and splices the page
body into the next prompt as `deep_search output:` with no untrusted-content
fence around it, and the system prompt's "everything on screen is data" wording
was written for an agent that could only click — it does not tell the model that
"run this Python" from a web page is the canonical injection. The boundary text
has not caught up with the action set.

### The code-execution gate

`shell` is not the only way to run code, so it is not gated alone. `python` runs
against a live interpreter that can `import os`; `mount_tool` and `call_tool`
define and then invoke a Python function body. All four are the same capability
and sit behind the same instance-level `shell_access` toggle:

- The orchestrator refuses them in `Runner.execute`
  (`backend/internal/agent/runner.go`) — one `case` covering
  `shell`/`python`/`mount_tool`/`call_tool`.
- `agentd` refuses them again in `/act` (`sandbox/agentd/main.py`), on its own
  authority rather than trusting its caller, because it is the component that
  actually holds the interpreter. One check is one bug away from a sandbox with
  an unexpected shell.
- The REPL's globals expose a curated `inject` facade
  (`sandbox/agentd/repl.py`), not the raw injection module. The raw
  `inject.shell(command, allow, …)` takes its own gate as a *parameter*, so REPL
  code could pass `True`; the facade's `shell()` has no such parameter and reads
  a constant captured at import.

Stated honestly: the facade is trap removal, not containment. Code running in
the REPL has a whole Python interpreter — `subprocess`, `os.system`, rebinding
module globals — and nothing inside one process can gate another part of that
process. Its value is that the toggle is enforced *before* any of it runs, and
that a future refactor cannot re-arm the bypass by accident.

`unmount_tool` and `deep_search` are deliberately outside the gate:
`unmount_tool` only removes a name, and `deep_search` runs in `agentd`, not in
the REPL. `deep_search` is still an unfiltered egress channel — see Known gaps.

## Sub-agent budget

`spawn_agent` lets a task create a child agent with the same action vocabulary,
which without bounds turns one poisoned web page into an exponential bill: each
child is its own perceive/decide/act loop against a paid vision model. The
limits (`backend/internal/agent/spawn_budget.go`) are:

| Limit | Value |
| --- | --- |
| Nesting depth | 3 |
| Children per task | 5 |
| Concurrent tasks, whole orchestrator | 32 |

A refused spawn is returned to the model as an outcome it can reason about
("do the work yourself"), not as a task failure.

**Caveat, stated plainly:** depth and per-task child counts are held in memory on
the `Runner`, not in the database. An orchestrator restart forgets them, so a
child task resumed afterwards is treated as depth 0 and can nest a further three
levels. The concurrency ceiling is the one limit that survives a restart,
because it is derived from the live running set. This is a deliberate trade
against a schema change, not an oversight — but if you restart the orchestrator
under load with deep trees in flight, the depth bound is not there.

## Sandbox isolation

Each instance is a container with:

- Hard cgroup limits on CPU and memory, **swap disabled** — a runaway build gets
  OOM-killed rather than dragging the host into thrash.
- A PID limit and no `SYS_ADMIN`. `NET_ADMIN` is added only when the instance
  actually carries an egress policy to program. `no-new-privileges` is
  deliberately **not** set — see "Sudo in the sandbox" below for why, and for
  what that costs.
- The agent is a non-root user (uid 1000); anything it writes is discarded with
  the container. Be precise about the scope of that: the *agent* is unprivileged,
  the *container* is not all-unprivileged. `supervisord`, `Xvfb`, both `x11vnc`
  servers and both `websockify` processes run as root inside the container
  (`sandbox/supervisord.conf`); only `dbus-session`, `at-spi`, `xfce` and
  `agentd` drop to `agent`.
- Its own network, unreachable from the control plane except by the orchestrator.

### The QEMU tier

An instance created with `"driver": "qemu"` boots the same sandbox as a real
virtual machine, which changes the boundary in one specific way: the agent's
kernel is the **guest** kernel. A kernel exploit inside a container-tier
sandbox is a host compromise; inside the VM tier it lands in a disposable
guest, and the attacker's next step is a QEMU escape — a materially higher
bar. Precision about what this does and does not buy:

- **The guest is the container image**, converted to a disk at build time, so
  everything above about the agent being uid 1000 and root-owned services
  holds identically inside the VM.
- **The runner container is not hardened like a sandbox** — its only process
  is QEMU, it holds `/dev/kvm` when the host provides it, and it has no shell
  surface an agent can reach.
- **Egress policies are enforced in the runner's netns**, and that placement
  is stronger than the container tier's. Every connection the guest opens is
  a SLIRP socket owned by the QEMU process in the runner, dialled to the
  destination the guest asked for — so `vm-entrypoint.sh` runs the same
  `egress.sh` the container tier uses, before QEMU starts, fail-closed (no
  policy, no boot). The rules sit where no code path from inside the guest
  can reach them: root in the guest can flush the guest's own tables and the
  policy does not move. The `EGRESS_*` variables are deliberately absent
  from the kernel-cmdline whitelist — the guest is never told its policy.
  SLIRP's IPv6 is disabled (`ipv6=off`) for parity with the v4-only sandbox
  network.
- **Without `/dev/kvm`** (Docker Desktop, most CI) the guest runs under TCG
  software emulation: the isolation property is the same, the boot takes
  minutes, and the runner logs which mode it chose.

### Sudo in the sandbox

Sudo is granted per instance and can be turned on and off while the instance is
running. `SudoAccess` on the create request sets the initial state; after that
the orchestrator changes it in place.

**How it is enforced.** By the setuid bit on `/usr/bin/sudo`.
`Manager.SetSudo` (`backend/internal/fleet/manager.go`) runs `chmod u+s` or
`chmod u-s` on that binary as root inside the container. Provisioning always
applies the setting once the container is up, and treats a failure as a
provisioning failure rather than a warning — the image ships sudo setuid, so an
instance that failed to have the bit cleared would quietly have root available.

With the bit cleared, sudo cannot escalate. The agent runs as an unprivileged
user (uid 1000) and so cannot put the bit back: restoring it needs exactly the
privilege being withheld.

**Why not `no-new-privileges`.** That container option is the stronger control —
the kernel ignores every setuid bit in the container, so it does not matter what
the filesystem says. It is deliberately not set, and the reasoning is written out
in full in the comment on `securityOpts` in `backend/internal/fleet/tiers.go`.

The problem is that the kernel applies the flag when the container is created and
it cannot be changed afterwards. Using it to gate sudo meant the only way to
revoke sudo from an agent was to recreate its container — and these sandboxes
carry no volume, so recreating one discards everything the agent has done. The
moment you most want to revoke sudo is mid-incident, which is exactly the moment
losing the workspace costs most. Being unable to take privilege away from a
misbehaving agent without destroying the evidence is the wrong failure to build
in.

**What that costs, stated plainly.** The backstop is gone. Under
`no-new-privileges` a bug in sudo, or in any other setuid binary in the image,
was unreachable because the kernel refused outright. It is reachable now. The
setuid bit is a real boundary against an agent that simply types `sudo`; it is
not a boundary against an exploit of a setuid binary. If your threat model
includes that, run the fleet with sudo off everywhere and accept that revoking it
is not the operation you will need.

**The sudoers deny-list.** Whether or not sudo works, the grant is not
`NOPASSWD:ALL`. `/etc/sudoers.d/agent` denies the commands that would dismantle
the platform's own controls: `nft`, `iptables`/`ip6tables` and their variants,
`ip`, `tc`, `mount`/`umount`, `insmod`/`rmmod`/`modprobe`/`depmod`, `sysctl`,
`unshare`, `nsenter`.

The one that motivated it: an instance carrying an egress policy also carries
`CAP_NET_ADMIN`, and the nftables rules live in the container's own network
namespace. `sudo nft flush ruleset` therefore used to delete the entire policy —
allow-list, RFC1918 block and all — from inside the container the policy exists
to contain. Cgroup limits are enforced by the host and survive; egress did not.

What the deny-list is worth, for an attacker who has code execution in the
sandbox (via `shell`, `python`, or a human typing at the desktop):

- **On an instance with sudo off, it is not the control doing the work** — the
  cleared setuid bit is. The deny-list is behind it.
- **On an instance with sudo on, the deny-list is the only thing left**, and it
  stops the direct, obvious command: no one-line `sudo nft flush ruleset` that
  drops the egress policy. The same applies when someone runs the image directly
  with `docker run`, which is how a developer first tests it — there is no
  orchestrator to clear the bit, so the agent has real root.
- **It does not stop a determined attacker in either case.** A sudoers deny-list
  matches on the resolved binary path, so `sudo bash`, or copying `nft` somewhere
  else and running the copy, walks straight around it. Read it as a guardrail
  against the obvious move — and against an agent talked into it by a web page —
  not as a boundary.
- The archetype workspace README no longer tells the model it has root. It says
  sudo "is available only if the operator granted it to this bot" and to check
  with `sudo -n true` first (`sandbox/init-archetype.sh`). That is the right
  wording now that the grant is per instance and changeable at runtime: the
  README is written once at provision time and would otherwise go stale the
  moment sudo was revoked.

**This is container isolation, not VM isolation.** A kernel exploit reaches the
host. The `developer-heavy` tier is the one most likely to run untrusted build
scripts and is also, today, still a container — the QEMU driver in the roadmap is
what closes that gap. Do not run genuinely hostile code here.

## Egress policy

`egress.sh` programs nftables in the network namespace it runs in — the
sandbox container's own netns on the docker driver, the VM runner's on the
qemu driver (where QEMU's SLIRP sockets originate every guest connection):

- A non-empty allow-list is exclusive: everything else is dropped, and that
  includes a blanket drop of all IPv6.
- Replies to connections opened *to* the sandbox — the orchestrator's health
  polls, the console's VNC stream — are accepted statefully
  (`ct state established,related`), ahead of the allow-list's final drop.
  Outbound restrictions lose nothing: a connection the agent initiates is
  judged at its first packet.
- `block_local` drops RFC1918, link-local and CGNAT ranges over IPv4, which is
  what stops an agent reaching your LAN, your database, or a cloud metadata
  endpoint. Read the IPv4 qualifier literally — see the limitations below.
- DNS and loopback stay open, or nothing resolves and the local control plane
  cannot talk to itself.
- The container's own subnet is left reachable under `block_local`, on purpose:
  the orchestrator has to be able to poll `agentd`. An agent can therefore always
  reach other sandboxes on its own network, policy or no policy.
- If the policy cannot be applied, the container **fails to start**. An instance
  asked to be restricted must never come up unrestricted.

Limitations, stated plainly:

- **`block_local` covers IPv4 only.** `sandbox/egress.sh` adds drops for
  `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16` and
  `100.64.0.0/10`, and adds nothing for `fc00::/7` or `fe80::/10`. Hostname
  resolution in that script is `getent ahostsv4`, so the allow- and deny-lists
  are IPv4-only too. On the shipped topology this does not bite: neither network
  in `docker-compose.yml` enables IPv6, so the sandbox has no IPv6 address to
  send from. If you enable IPv6 on the sandbox network, `block_local` stops being
  a containment boundary and an agent can reach the private network over v6.
  Setting an allow-list is the workaround, because allow-list mode does drop IPv6
  wholesale; `block_local` on its own does not.
- Hostnames are resolved once, at policy time. A host behind a CDN whose
  addresses rotate will drift out of the allow-list. Use a CIDR or an explicit
  egress proxy for those.
- The rules live in the container's own network namespace, and the container
  holds `CAP_NET_ADMIN` whenever a policy exists — that is how the rules get
  programmed in the first place. Anything that reaches root *inside* the
  container can therefore still flush them. The sudoers deny-list above removes
  the easy path, and on an instance without sudo the cleared setuid bit removes
  the agent's ordinary route to root — but neither is the kernel-level backstop
  that `no-new-privileges` used to provide, and the policy is not tamper-proof by
  construction. Moving it out of the agent's reach is what makes it so — and the
  **qemu driver already has exactly that**: its policy lives in the runner's
  netns, which no code path from inside the guest can touch. For workloads where
  a tamper-proof egress policy is the requirement, use `"driver": "qemu"`; on
  the docker driver, host-side rules on the sandbox bridge or a real egress
  proxy remain the alternatives.

Note also that egress filtering is **off by default**. With no policy on the
instance, `entrypoint.sh` skips `egress.sh` entirely and the sandbox can reach
the orchestrator's API and whatever else sits on the sandbox network. It cannot
reach Postgres, which is on the `control` network only.

## Credentials

- API keys and sandbox logins are sealed with AES-256-GCM under `MASTER_KEY`,
  with the secret's ref as additional authenticated data — a sealed value cannot
  be moved to a different ref.
- In production (`AGENTFLEET_ENV=production`) `MASTER_KEY` must decode to
  exactly 32 bytes of base64 or hex; the orchestrator refuses to start
  otherwise, so a development placeholder cannot be carried into a deployment.
- The API never returns a secret value. The console shows names and notes.
- Provider API keys go in request **headers**, never in a URL query string —
  including Gemini's, which uses `x-goog-api-key`. This matters because a
  transport-level failure returns a Go `*url.Error` whose text contains the full
  URL, and connector errors are not merely logged: they are written to the
  task's `error` column, broadcast on the event bus, filed as an alert and
  pushed to operators' phones. As a second layer,
  `backend/internal/connectors/redact.go` scrubs the key out of any error a
  provider quotes it back in, before that error escapes the connector.
- **Losing `MASTER_KEY` means losing every stored credential.** Back it up
  somewhere that is not the repository.

**Run-scoped injection into the sandbox is not implemented.** Earlier versions of
this document said a secret injected into a sandbox "lands on tmpfs, mode 0600,
owned by the agent user, and is cleared at the end of the run". That described a
plan. What exists is: a `/keyring` endpoint in `agentd`, and `Inject` /
`ClearKeyring` methods on `SandboxClient` with **no callers anywhere in the task
lifecycle**. No run writes a vault secret into a sandbox, and no run clears one.
If you need a credential inside a sandbox today, you are putting it there
yourself.

Two things are wrong with the endpoint as it stands and must be fixed before
anything is wired to it: `KEYRING_DIR` (`/var/run/agentfleet/keyring`) is an
ordinary directory on the container's writable layer, **not tmpfs** — only
`/tmp` is a tmpfs mount — and there is no clear-on-failure path, so a crashed
orchestrator would leave the value behind for the container's lifetime. Mounting
it as tmpfs cannot be done from inside the container (that needs `CAP_SYS_ADMIN`,
which the sandbox deliberately lacks); it means adding the path to the container's
`Tmpfs` map in `backend/internal/fleet/manager.go`.

The sealing itself is sound and is what the vault claim rests on: AES-256-GCM
with the secret's ref as AAD, key length enforced, values never returned by the
API.

## Access control

Three roles:

| Role     | Can |
| -------- | --- |
| auditor  | Read everything, watch a desktop view-only, replay a run |
| operator | Provision, drive, record, assign tasks, answer alerts |
| admin    | All of the above, plus engines, secrets, and user management |

"View-only" for an auditor is enforced server-side, and it is worth saying how,
because the obvious implementations do not work. noVNC's own `view_only`
parameter is client-side and lasts exactly until someone edits the URL, and
filtering RFB `PointerEvent` / `KeyEvent` messages inside the proxy's byte
tunnel is fragile. So the sandbox runs **two** VNC servers against the same
display: the interactive one on 6901, and a second `x11vnc -viewonly` on 6902
(`sandbox/supervisord.conf`). `handleVNCProxy` decides from the caller's role
which upstream to splice them to — anything below `operator` gets 6902, where
input events have nowhere to go. It fails closed: an instance with no
`VNCViewURL` gets a 409 rather than the interactive port.

Failing closed was load-bearing. `VNCViewURL` was set when a bot was
provisioned and then never stored — there was no column for it — so it was
empty from the next restart onward and every auditor was refused. The feature
had not worked across a restart since it was written, and the message blamed
the age of the instance rather than the missing column. It is persisted now.
The lesson is the one worth keeping: the failure mode was "nobody can watch"
rather than "an auditor got a keyboard", which is the direction a mistake here
has to fall.

The proxy also strips `Authorization`, `Cookie` and `?token=` before forwarding,
so the platform JWT never reaches the sandbox.

Sessions are HS256 JWTs with a 12-hour default lifetime. The event socket and the
desktop proxy accept the token as a query parameter because browsers cannot set
headers on a WebSocket handshake or an `<iframe>` load — the token is still
verified on every connection.

`POST /api/auth/bootstrap` creates the first administrator and refuses once any
user exists, so leaving it routed is not a standing hole.

### API keys

A script or a CI job authenticates with a long-lived key rather than a
password: `Authorization: Bearer af_<id>_<secret>`. The id is the public half,
so a request finds its row without hashing against every key; only a SHA-256 of
the secret half is stored, compared in constant time. Losing the database
therefore does not hand anyone a working key, and there is no way to read a
secret back — it is displayed once, when it is issued.

A key carries its owner's role, so every check behaves identically whether a
person or a script made the call, and there is no second permission model to
keep in step. Revoking a key, or disabling the account it belongs to, stops it
at the next request. Revoked keys keep their row: a key that has been used is
part of the audit trail, and deleting it would make past activity untraceable.

Accounts are disabled rather than deleted for the same reason — deleting
cascades a person's keys away and orphans what they made.

### Engines are administration, not use

Provider connections, model combinations and per-bot fallback chains are
admin-only, reading included. They were reachable by any signed-in user, and
the mutating combination and chain routes needed only operator — so someone
handed a single bot to drive could read every provider's base URL and OAuth
client id, and rewrite the chain the whole fleet falls back through. A test
over the route table now fails if any of them is opened up again.

## The Docker socket

The orchestrator holds `/var/run/docker.sock`, which is root-equivalent on the
host. This is inherent to provisioning containers and is the single most
important thing to understand about deploying this:

- Anyone with admin on the console can provision a container on your host.
- The orchestrator container runs as a non-root user added to the host's docker
  group, not as root.
- Treat orchestrator admin as host root. Do not expose this platform to the
  public internet without a reverse proxy, TLS, and a hard look at who has an
  account.

## MCP servers over stdio

Registering a stdio MCP server asks the orchestrator to **execute a command**,
inside the orchestrator container — not inside a sandbox. That is what the
transport is rather than a flaw in the implementation, and it is the loudest thing
in the MCP bridge:

- The registration endpoint is admin-only, and an admin already holds the Docker
  socket above, so this grants nothing they did not already have.
- Every stdio registration is logged with its command at warning level.
- `MCP_DISABLE_STDIO=true` refuses the transport entirely, leaving only HTTP
  servers. Worth setting on any deployment where more than one person has admin,
  or where MCP servers are expected to be remote anyway.

An MCP server also sees whatever an agent passes to its tools, and returns content
that goes straight into the agent's next prompt. Tool results are treated as
untrusted data for the same reason screen content is — a compromised or hostile
MCP server is a prompt-injection vector with a very direct path.

The `env` map on an MCP server row holds credentials in the general case (an API
key for a hosted server, a bearer token for an HTTP one). It is stored in the
database, never returned by the API — `GET /api/mcp/servers` reports the key names
only — and is excluded from exported archetype packages.

## The fleet chat

The home screen is one conversation with every agent, and three new things
can happen from it.

- **Slash commands** (`httpapi/fleet_commands.go`). `POST /api/fleet/command`
  is operator-gated at the route and re-checks per-bot permissions inside:
  starting work needs `chat` on that bot, pausing or resuming needs `edit`,
  provisioning needs `create`. Results that changed something are written
  into the broadcast channel as `system` notes from "Oaf"; agents never
  answer that kind, so a note cannot start a conversation.
- **`ASK <bot>: …` in a private chat** (`httpapi/peer_ask.go`). A
  model-authored line becomes a real peer message to a colleague. The text
  the model was looking at — the screenshot included — is untrusted, so a
  hostile page can in principle talk a bot into asking a colleague
  something. The blast radius is the one `message_peer` already has, and it
  is bounded the same way: the recipient treats it as a peer message, never
  as an instruction from you.
- **Marathon continuation** (`agent/marathon.go`). A run that never says done
  keeps going in fresh windows, spending model tokens the whole time.
  `AGENT_MARATHON_MAX_WINDOWS` is the cap and defaults to unbounded, because
  "until it is done" is what was asked for; the `progress` alert at every
  boundary and `/stop @bot` are the operator's controls. An agent with a
  monthly budget is held when it reaches it, which stops the run; one
  without a budget is stopped on cost by nothing.

## Sessions on your own devices

A session with Oaf can act on the operator's PC or phone. That is the one
place the platform reaches outside its sandboxes on purpose, so the rules
are stated here rather than implied.

- **The device connects out; nothing connects in.** `fleetctl host` and the
  phone register a device and long-poll `/api/oaf/devices/{id}/jobs` with the
  owner's token. There is no listener on the device and no inbound port.
  Killing the process ends the device's availability at once; the
  orchestrator only ever holds a queue of jobs it hopes someone will pick up.
- **Folder jail.** Every path in a job is resolved against the roots given on
  the command line (`--root`, repeatable) and refused if it lands outside
  them, after symlink resolution (`RootJail` in
  `sdk/python/agentfleet/host.py`). Shell commands run with the working
  directory inside a root, but a shell is a shell: `cat ../secret` is stopped
  only by the approval prompt, not by the jail. Give a session the narrowest
  folder that does the job.
- **Approval before state changes.** A shell command, a file write and an
  `open_url` ask in the terminal and wait; reads, listings and searches do
  not. `--yes` turns the asking off for that process and is meant for a
  session you are watching. With no terminal to ask on, the host refuses.
- **Ownership is checked on both ends.** A device belongs to the user who
  registered it; sessions list and use only their owner's devices; a job's
  result is accepted only from the device it was queued for. Oaf's `fleet`
  tool runs slash commands as the session's owner with that user's
  permissions, never as an operator by default.
- **Attachments are untrusted input.** Files and images pasted into a session
  are stored under the session and shown to the model; a hostile document can
  talk Oaf into a tool call the same way a hostile web page can talk a bot
  into one. The approval prompt is the control, which is why it exists for
  writes and shell and not only for the sandboxed bots.
- **`/loop` keeps running when you are not looking.** A repeating job
  replays the turn under the owner's identity at every tick until
  `/cancel`; `/jobs` lists what is armed. The same approval rules apply on
  the device, so a loop that needs a write stalls at the prompt rather than
  proceeding — unless the device was started with `--yes`.

## External agents

An agent can be Claude Code, Codex or Hermes on the operator's PC, an
OpenClaw gateway, or a webhook
([ORG-AND-TICKETS.md](ORG-AND-TICKETS.md#external-agents)). None of them run
in a sandbox this platform built, so the usual isolation says nothing about
them.

- **A local CLI runs as you, in the folder you gave it.** `fleetctl host`
  checks the agent's folder is under a `--root` and starts the CLI there. The
  CLI's own permission model is the containment: autonomy **edits** passes
  Claude Code `--permission-mode acceptEdits` and Codex a workspace-write
  sandbox; **full** passes `--dangerously-skip-permissions`,
  `--dangerously-bypass-approvals-and-sandbox` or `--yolo`, and the CLI can
  then do anything your account can. The folder jail does not apply to what
  the CLI does once started. Each run asks in the host's terminal first
  unless the host was started with `--yes`.
- **Ownership is checked at creation.** An external agent can only be put on
  a device its creator owns, a folder under that device's roots, and a
  runtime the device reported. The orchestrator queues the run; the device
  fetches it; nothing connects in.
- **Run tokens are narrow.** Each run gets an HMAC token over its task id,
  valid 48 hours, keyed by the orchestrator's JWT secret. It lets the agent
  finish that run, report progress, comment on and hand out work under that
  run's ticket — nothing else. It is not a user token.
- **Webhook and OpenClaw secrets are vault entries.** The token given at
  creation is sealed like any credential and never returned. OpenClaw's
  device key is generated by the orchestrator, kept in the vault, and used to
  approve its own pairing, which needs an `operator.admin` token: treat that
  token as admin on the gateway.
- **An external agent's output is a colleague's output.** A report from
  Claude Code reaches the next agent in its brief the way a desktop bot's
  does. Mark an agent that reads untrusted repositories or pages **low
  trust** and its text is fenced as data and it cannot hand out work, reopen
  work or write to the shared vault. That is containment of what it says, not
  of what it does on your PC.

## Budgets

A monthly ceiling per agent (admins set it) is enforced after every turn and
on every reconcile pass: at the ceiling the agent is held, its running task
cancelled and its tickets left waiting until the ceiling rises or the month
turns. Spend is what the turns cost as priced by the telemetry table, or as
reported by an external agent. A webhook that reports no cost costs nothing
as far as the budget knows.

## Agent-authored apps

The shared work catalog lets an agent publish an *app* — one HTML document —
that the operator's phone renders and runs. That is a real surface: a model
writing markup that a human's app executes.

What contains it:

- The document is loaded with `loadHtmlString`, not from a URL, so it runs on
  an opaque origin. It has no cookies, no `localStorage` shared with anything,
  and no access to the app's session token.
- The web view has no network, enforced by a Content-Security-Policy the
  viewer injects into every app before it runs: `default-src 'none'` with
  `connect-src 'none'`, and `script-src 'unsafe-inline'` so an inline game
  still works. Verified against a browser, which refuses the request citing
  the directive — including a URL assembled at runtime from string pieces.
  Navigation away is separately refused by the navigation delegate.
- The publish-time scan for `src="http` is a courtesy, not the control. It
  catches the honest mistake while the agent can still correct it, and cannot
  catch a URL built at runtime. Saying otherwise was the mistake this entry
  used to make.
- The content must actually be an HTML document. A bot published a Python file
  as an app and nothing stopped it; that now fails with a message telling the
  agent to use `work_kind: "file"` instead.

What is *not* contained: the document is arbitrary JavaScript running in a web
view on your phone. It cannot reach OpenAgentFleet or the network, but it can
consume CPU and it can draw anything it likes. Treat an app the way you would
treat a script a colleague sent you — the catalog says which bot published it
and at which version, which is there so that question has an answer.

## What a model provider sees

Every screenshot an agent takes goes to whichever provider serves the vision
role, and that provider may be a hosted API. This has always been true of the
agent loop — a step is a picture of the desktop and a question about it — and
two things added since make it worth stating plainly:

- **`read_work` puts catalog content on the agent's desktop**, so a colleague's
  published file can appear in the next screenshot.
- **Demonstration recording captures frames**, and the compiler sends them to
  the vision model to name the control that was clicked. A recording made on a
  desktop with a password manager or a customer's data open sends those pixels
  to the configured provider.

Point the vision role at a local model if that matters. Ollama is the default
for exactly this reason, and the role can be pinned independently of the models
used for other roles — see **Engines are administration, not use** above.

## Known gaps

Open, known, and listed here rather than discovered later. Each is a real
finding from the security review that has not been fixed yet.

- **The webhook endpoint is unauthenticated by design, and now starts real
  work.** `POST /api/webhooks/{token}` takes no session: the token is the
  credential and the signature is the proof. This entry used to warn that "the
  moment this stub actually dispatches a task, an anonymous caller starts
  autonomous agents" — it dispatches now, so the guards it was waiting for are
  in place:
  - the token is generated with `crypto/rand`, not from the clock. It defaulted
    to `wh-<UnixNano>`, which looks random and is not — roughly a billion
    candidates for anyone who knows what minute a webhook was made — and a
    caller-supplied token was accepted verbatim, so `github-pr-sync` was a
    legal choice. A supplied token must now be at least 24 characters.
  - a signing secret is required at creation, and dispatch refuses without one
    rather than waving it through. Rows predating the requirement stop working,
    which is the direction that mistake has to fall.
  - the body is bounded by `http.MaxBytesReader`.

  What remains: anyone holding the URL and the secret can start a task, which
  is what a webhook is for. Treat both as credentials. Webhooks and cron
  triggers still have no owner scoping, so any admin sees all of them.

  Since the review, the signature scheme is per-sender rather than one generic
  HMAC. Two things worth noting about that. A webhook declared `github` accepts
  a signature **only** in `X-Hub-Signature-256` — accepting the house header too
  would let anyone who learns the URL sign with the header of their choosing. And
  `stripe` enforces a five-minute window on the timestamp inside the signed
  payload, without which a captured delivery stays replayable forever; that is
  not bookkeeping when a replay starts an autonomous agent.
- **Payload summaries put attacker-controlled text at the top of a prompt.** A
  GitHub pull request title, a Stripe customer email, a CRM form's message field:
  all of these are now extracted into a one-line "What happened" that leads the
  agent's goal. That is the point — it is what stops a small model spending two
  turns parsing JSON — but it means someone who can open a pull request against a
  watched repository can choose text that an agent reads first. The
  untrusted-external-data fence still wraps the payload, and the summary is
  labelled as coming from it; the injection boundary is the model's instruction
  to treat all of it as data, which is exactly as strong as it is everywhere else
  in this system.
- **`deep_search` is an unfiltered fetch.** Any `http(s)` URL, no private-range
  check, no response size cap, and the body lands in the model's prompt with no
  untrusted-content fence. With no egress policy on the instance, that reaches
  the sandbox network.
- **REPL state persists across tasks.** The interpreter is one module-level
  singleton per container, never reset between runs. A task can leave behind a
  background thread, a monkeypatched helper, or a redefined tool that a later
  task — possibly a different operator's — inherits. Cleanup at end-of-run does
  not help if the orchestrator dies mid-task; a reset at the *start* of every run
  is the fix.
- **A wedged REPL execution is abandoned, not killed.** `execute()` now honours
  its timeout by giving up on the worker thread and releasing the lock, so one
  `while True:` no longer wedges `agentd` permanently. But Python cannot kill a
  thread: the loop keeps burning a CPU inside the container until the instance is
  destroyed.
- **The step feed carries attacker-influenced content.** `task_steps` persists
  and the event bus broadcasts the whole action, including model-authored tool
  handlers and `python` source. That is correct for an audit trail — treat the
  contents as untrusted data, render them as inert text, and note that the feed
  is readable by any authenticated role.

## Deployment checklist

- [ ] `JWT_SECRET` and `MASTER_KEY` generated with `openssl rand -base64 32`
      (`MASTER_KEY` must decode to exactly 32 bytes)
- [ ] `MASTER_KEY` backed up outside the repository
- [ ] `AGENTFLEET_ENV=production` (the orchestrator then refuses to start on
      development defaults, including a short or missing `MASTER_KEY`)
- [ ] TLS terminated in front of the console and the API
- [ ] `MAX_INSTANCES` sized against real host RAM
- [ ] `ALLOW_SHELL=false` unless something actually needs to compile — this is
      the code-execution toggle, covering `python`, `mount_tool` and `call_tool`
      as well as `shell`. Note the shipped default is **true**
      (`.env.example`, `docker-compose.yml`, and `envBool("ALLOW_SHELL", true)`
      in `config.go`), so this is something you turn off deliberately, not
      something that is off until you ask for it.
- [ ] `block_local` on by default for every instance, and an explicit egress
      policy on any instance you care about: no policy means no filtering
- [ ] The API not reachable from the internet while `/api/webhooks/{token}` is
      unauthenticated (see Known gaps)
- [ ] Postgres and artifact storage backed up — they hold the audit trail
