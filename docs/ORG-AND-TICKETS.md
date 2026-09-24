# The org chart, tickets and external agents

Work in OpenAgentFleet is **tickets**. A ticket has one assignee, the ticket
it exists for (its parent), and the tickets it waits on (its blockers). Agents
are arranged in an **org chart**: each reports to another agent or to you.
And an agent is either a **desktop** this fleet provisions or an **external
agent** — Claude Code, Codex or Hermes on your PC, an OpenClaw gateway, or any
webhook — sitting in the same chart and taking the same tickets.

The ideas come from [Paperclip](https://github.com/paperclipai/paperclip)'s
control plane; the implementation is our own and keeps what this project is
about: agents with their own computers, watched live and taken over by hand.

## How work moves

1. **You ask the fleet** in chat. Each agent answers with the part it will
   take. The request becomes a root ticket and every part a ticket under it
   (`backend/internal/httpapi/fleet_parts.go`):

   | Part's stage | Its ticket |
   |---|---|
   | design | a work ticket; unstarted builds wait for it |
   | build | a work ticket that waits for design; testers become its **reviewers** |
   | test | no ticket of its own — it reviews the builds, one review ticket per round |
   | review | the request's **verifier**: checks the whole tree when it stops |

   A tester who answers before any builder waits in a placeholder the first
   build adopts. Whoever you name first starts at once.

2. **The engine runs what is ready** (`backend/internal/tickets`). A todo
   ticket whose blockers are done, whose assignee is free, not held and
   reachable, is checked out atomically and started. One run per agent at a
   time. The run's brief says what to do, **why** (the chain of tickets up to
   your request), what the tickets it waited on produced (closing report,
   what was published, the author's last message), the last reviewer's
   findings, and who is on the team.

3. **A run ending finishes its ticket.**
   - Work with a reviewer goes to **in review**; a review ticket runs; its
     **verdict** is the deliverable. Pass finishes the work. Fail sends it back
     with the findings, then to review again — three rounds at most.
   - Work that created tickets of its own (`create_ticket`) waits for them,
     then comes back to its assignee with their results.
   - A failure that says nothing about the work (a malformed reply, a
     provider outage, a stall, a PC that dropped off) is retried twice with a
     note. Anything else blocks the ticket.
   - Finishing wakes whatever waited on it. When every part of your request
     is finished, the request closes and says so in chat.

   **Handing work down.** An agent passes part of its ticket to someone
   who reports to it with `create_ticket`. A ticket that names one of the
   agent's reports ("have Claude write…") opens its brief with that
   instruction; finishing without having handed that report anything is
   answered once, asking for the reason. When the handing-on leaves the
   ticket waiting, the run ends there — there is nothing to do until the
   work comes back, and the ticket brings the agent back with it, with a
   brief that says where the work is. Handing the same job to the same
   colleague again (the same file, or mostly the same words) files nothing
   and points at the ticket that has it, or at what it produced.

4. **Blocked work goes up the org chart.** The assignee's manager gets an
   *unblock* ticket; its answer puts the work back in the queue with a note.
   With no manager, you get an alert and a push.

5. **Nothing is left ownerless.** Every pass checks that each open ticket has
   a next move — a live run, a blocker that is itself healthy, a reviewer, an
   unblock ticket, a person. A ticket without one is reported, once per
   stopped state: nobody owns it, its assignee was deleted or is held at its
   budget or offline, it waits on a cancelled ticket, it is parked for work
   nobody is making. An unassigned ticket gets two minutes first: a
   request's root exists a moment before its first part is filed under it,
   and a ticket made on the board is often assigned a moment later.

6. **A verifier checks stopped work.** Give a ticket a verifier and, when its
   whole subtree comes to rest in a state not checked before, the verifier
   gets a verify ticket listing every claim. It reopens (`reopen_ticket`)
   anything not genuinely finished. Five verifications at most.

7. **Cancelling and reopening follow the tree.** Cancelling a ticket
   cancels its open parts and any open review or verify of them, and stops
   their runs; finished parts stay finished. Reopening finished work sends
   it back to its assignee, and whatever above it had finished waits again.
   Reopening a request — a ticket nobody is assigned — reopens its finished
   work parts, since those are what its claims were.

Everything is re-derived from the rows every twenty seconds, so a restart
loses nothing: a run that finished while the orchestrator was down is read
from its row and handed on, and a run on a PC that was interrupted is
resumed through its adapter.

### Where to see it

- **Console:** *Work* is the board (backlog → done, with a drawer per
  ticket); *Org* is the chart, where dragging an agent onto another changes
  who it reports to. `T-12` anywhere in chat links to the ticket.
- **App:** *Work* and *Org chart* from the Fleet screen; long-press and drag
  on the chart to re-parent.
- **Chat:** `/tickets [@agent]`, `/ticket @agent <what to do>`, `/org`.
- **Command line:** `fleetctl tickets`, `fleetctl ticket new|show|comment|reopen|move`,
  `fleetctl org`, `fleetctl agent add|set`, `fleetctl fleet export|import`.

## Budgets

Each agent can have a monthly spend ceiling. At the warning percentage (80%
by default) you are told once; at the ceiling the agent is **held**: its runs
are stopped, its tickets wait, and you get a push. Raising the ceiling — or
the month turning — releases it. A ticket can also carry its own budget; a
run that spends it is stopped and the ticket blocked. Spend is what the
turns cost: priced from the model for desktops and Codex, and reported by
Claude Code, OpenClaw and webhooks themselves. Budgets are set by an admin.

## Trust

An agent that reads hostile input — web pages, external tickets, untrusted
repositories — can be marked **low trust**. What it writes reaches other
agents fenced as data (`<<<untrusted: ...>>>`), in hand-off briefs, review
briefs, comments and peer messages. It cannot hand work to others
(`create_ticket`, `delegate_task`), reopen work, or write to the shared vault
or sessions. This is containment, not a sandbox: the desktop isolation is
what it always was (see [SECURITY.md](SECURITY.md)).

## External agents

| Kind | Where it runs | How a run reaches it |
|---|---|---|
| Claude Code | your PC, in a folder | `fleetctl host` runs `claude --print --output-format stream-json` |
| Codex | your PC, in a folder | `fleetctl host` runs `codex exec --json` |
| Hermes | your PC, in a folder | `fleetctl host` runs `hermes chat -q` |
| OpenClaw | its gateway | the orchestrator dials the gateway WebSocket (protocol 4) |
| Webhook | anywhere | a POST; it answers at once or calls back |

**On your PC.** Start the host in the folders you want agents to work in:

```bash
fleetctl host --root ~/projects
```

It reports which CLIs it finds. Add an agent in the console (Org or Fleet →
Add agent → Claude Code), or from the command line:

```bash
fleetctl agent add Claude --kind claude_code --folder ~/projects/app --model sonnet --reports-to Builder
```

pick the PC and a folder under one of its roots. Each run asks in the host's
terminal before it starts, unless you ran the host with `--yes`. A run
resumes the CLI's previous session on the same ticket. Autonomy **edits**
lets the CLI change files but not run commands; **full** lets it do
anything inside its folder.

- **What it makes is shared.** A desktop colleague cannot reach your PC.
  When a run finishes, the host sends back the text files it created or
  changed, and the files in its folder its report names (20 files, 256 KiB
  each, 1 MiB in all; binaries and larger files are listed, not sent). They
  are published to the work catalog under their path in the folder, so
  `notes/plan.md` in the report is `notes/plan.md` to a colleague's
  `read_work`, and the ticket records what was published.
- **Not your user settings.** Claude Code is started with
  `--setting-sources project,local`: your `~/.claude` settings — extra
  allowed directories, allow rules, hooks, MCP servers — are for you at
  your desk, and an `additionalDirectories` entry there let an **edits**
  agent write outside its folder in testing. The folder's own `.claude`
  settings apply. `AGENTFLEET_CLAUDE_USER_SETTINGS=1` on the host keeps
  yours.

**OpenClaw.** Give the gateway's `ws://` or `wss://` address and a token. The
agent presents an Ed25519 device identity kept in the vault and approves its
own pairing the first time, which needs a token with `operator.admin`.

**Webhook.** The agent receives:

```json
{
  "agent": {"id": "...", "name": "Research"},
  "run_id": "…task id…",
  "ticket": "T-12",
  "prompt": "the full brief",
  "callback": {
    "token": "afr_…",
    "complete_url": "https://fleet.example/api/runs/…/complete",
    "progress_url": "…/progress",
    "tickets_url": "…/tickets",
    "comments_url": "…/comments",
    "context_url": "…"
  }
}
```

Answer `200 {"status":"done","result":"…"}` to finish at once, or `202` and
later `POST complete_url` with `Authorization: Bearer <token>` and the same
body. `status` is `done` or `failed`; `verdict` (`pass`/`fail`), `cost_usd`,
`input_tokens`, `output_tokens` and `model` are optional.

**Run tokens.** Every external run gets a token for that run alone (HMAC over
the task id, 48 hours). Local CLIs find it as `$AGENTFLEET_RUN_TOKEN` with
`$AGENTFLEET_TASK_ID` and `$AGENTFLEET_TICKET`, and `$AGENTFLEET_API_URL` set
by the host. With it an agent can hand part of its ticket to a colleague,
comment, report progress and finish — and nothing else.

## Agent actions

| Action | Fields | What it does |
|---|---|---|
| `create_ticket` | `target`, `title`, `text` | A child ticket for a colleague; the current ticket waits for it |
| `reopen_ticket` | `ticket` (T-12), `text` | Sends work back with what is missing. Verifiers, reviewers and managers only |
| `done` | `summary`, `verdict` | Review and verify tickets must give `verdict: pass` or `fail` |

## API

All under `/api`, with a user token unless noted. `{id}` for a ticket accepts
its id or its reference (`T-12`).

| Method | Path | Body / query | Returns |
|---|---|---|---|
| GET | `/tickets` | `?status=todo,in_progress&assignee=&parent=&roots=1&limit=` | `[TicketView]` |
| POST | `/tickets` | `{title, description, kind, status, priority, assignee_id, parent_id, blocked_by[], reviewer_id, verifier_id, budget_usd, thread}` | `TicketView` |
| GET | `/tickets/{id}` | | `{ticket, ancestry[], children[], blockers[], dependents[], comments[], runs[]}` |
| PATCH | `/tickets/{id}` | any of `{title, description, status, priority, assignee_id, reviewer_id, verifier_id, parent_id, budget_usd, blocked_by[]}` | `TicketView` |
| DELETE | `/tickets/{id}` | | 204 |
| POST | `/tickets/{id}/comments` | `{body}` | `TicketComment` |
| POST | `/tickets/{id}/reopen` | `{reason}` | `TicketView` |
| GET | `/org` | | `{nodes: [OrgNode], kinds: [{kind, label, on_device}]}` |
| PUT | `/instances/{id}/profile` | any of `{title, capabilities, reports_to, budget_month_usd, budget_warn_pct, trust, connection, token}` | `Instance` |
| POST | `/instances` | as before, plus `kind`, `title`, `reports_to`, `capabilities`, `budget_month_usd`, `budget_warn_pct`, `trust`, `connection`, `token` | `Instance` |
| GET | `/fleet/export` | admin | `FleetTemplate` |
| POST | `/fleet/import` | admin; `{template, dry_run, rename}` | `{created[], skipped[], renamed[], notes[]}` |

**Run callbacks** (run token, not a user):
`GET /runs/{taskId}`, `POST /runs/{taskId}/progress {text}`,
`POST /runs/{taskId}/complete {status, result, …}`,
`POST /runs/{taskId}/tickets {target, title, text}`,
`POST /runs/{taskId}/comments {body}`.

**Devices:** `POST /oaf/devices` takes `runtimes[]`;
`POST /oaf/devices/{id}/jobs/{jobId}/progress {events: [{kind, text}]}`
answers `{cancel: bool}`.

**Events** on the WebSocket bus: `ticket` (payload `Ticket`),
`ticket.comment` (payload `TicketComment`), plus the usual `task.state` and
`task.step`, which external runs emit exactly as desktops do.

### Shapes

```jsonc
// TicketView
{
  "id": "…", "ref": "T-12", "number": 12, "title": "…", "description": "…",
  "kind": "work|review|verify|unblock",
  "status": "backlog|todo|in_progress|in_review|blocked|done|cancelled",
  "priority": 0, "parent_id": "…", "target_id": "…",
  "assignee_id": "…", "assignee_name": "Builder",
  "reviewer_id": "…", "reviewer_name": "Checker", "verifier_id": "…", "verifier_name": "…",
  "thread": "broadcast", "origin": "the operator's words", "stage": "build",
  "task_id": "…live run…", "attempts": 0, "rounds": 1, "wakes": 0,
  "verdict": "pass|fail|", "result": "closing report", "blocked_reason": "…",
  "budget_usd": 0, "cost_usd": 0.42, "blocked_by": ["…"],
  "created_at": "…", "updated_at": "…", "started_at": "…", "done_at": "…"
}
// TicketComment
{ "id": "…", "ticket_id": "…", "author_id": "…", "author_name": "Checker",
  "kind": "comment|system|result|verdict|published|retry|review_brief", "body": "…", "created_at": "…" }
// OrgNode
{ "id": "…", "name": "Builder", "title": "Engineer", "kind": "desktop|claude_code|codex|hermes|openclaw|webhook",
  "state": "running", "reports_to": "…|", "capabilities": "…", "trust": "standard|low", "hold": "|budget",
  "archetype_id": "…", "busy": true, "ticket_ref": "T-12", "ticket_title": "…",
  "spend_month_usd": 3.1, "budget_month_usd": 10, "open_tickets": 2, "online": true }
// AgentConnection (Instance.connection)
{ "device_id": "…", "cwd": "C:/work/app", "model": "sonnet", "args": [], "url": "wss://…",
  "token_ref": "…", "agent_id": "main", "autonomy": "edits|full", "timeout_sec": 0 }
```
