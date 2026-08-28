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
- The action vocabulary is closed. A model cannot invent a new capability; it can
  only emit one of fifteen actions, each validated before execution.
- `shell` is refused unless the instance opted in, checked twice — once in the
  orchestrator, once in `agentd`. One check is one bug away from a sandbox with an
  unexpected shell.
- `ask_human` is the prescribed response for anything irreversible: credential
  entry, payment, publishing, accepting an agreement, CAPTCHAs, MFA.

What is **not** solved: a sufficiently persuasive page can still talk a model
into a harmful action inside its existing capabilities. Defence in depth is the
answer, not the prompt. Give an instance the narrowest egress policy and the
least shell access the task actually needs.

## Sandbox isolation

Each instance is a container with:

- Hard cgroup limits on CPU and memory, **swap disabled** — a runaway build gets
  OOM-killed rather than dragging the host into thrash.
- `no-new-privileges`, a PID limit, and no `SYS_ADMIN`. `NET_ADMIN` is added only
  when the instance actually carries an egress policy to program.
- A non-root user inside; anything it writes is discarded with the container.
- Its own network, unreachable from the control plane except by the orchestrator.

**This is container isolation, not VM isolation.** A kernel exploit reaches the
host. The `developer-heavy` tier is the one most likely to run untrusted build
scripts and is also, today, still a container — the QEMU driver in the roadmap is
what closes that gap. Do not run genuinely hostile code here.

## Egress policy

`egress.sh` programs nftables inside the sandbox's own network namespace:

- A non-empty allow-list is exclusive: everything else is dropped.
- `block_local` drops RFC1918, link-local and CGNAT ranges, which is what stops
  an agent reaching your LAN, your database, or a cloud metadata endpoint.
- DNS and loopback stay open, or nothing resolves and the local control plane
  cannot talk to itself.
- If the policy cannot be applied, the container **fails to start**. An instance
  asked to be restricted must never come up unrestricted.

Limitation, stated plainly: hostnames are resolved once, at policy time. A host
behind a CDN whose addresses rotate will drift out of the allow-list. Use a CIDR
or an explicit egress proxy for those.

## Credentials

- API keys and sandbox logins are sealed with AES-256-GCM under `MASTER_KEY`,
  with the secret's ref as additional authenticated data — a sealed value cannot
  be moved to a different ref.
- The API never returns a secret value. The console shows names and notes.
- A secret injected into a sandbox lands on tmpfs, mode 0600, owned by the agent
  user, and is cleared at the end of the run. It never enters a prompt, a step
  record or a log.
- **Losing `MASTER_KEY` means losing every stored credential.** Back it up
  somewhere that is not the repository.

## Access control

Three roles:

| Role     | Can |
| -------- | --- |
| auditor  | Read everything, watch a desktop view-only, replay a run |
| operator | Provision, drive, record, assign tasks, answer alerts |
| admin    | All of the above, plus engines, secrets, and user management |

Sessions are HS256 JWTs with a 12-hour default lifetime. The event socket and the
desktop proxy accept the token as a query parameter because browsers cannot set
headers on a WebSocket handshake or an `<iframe>` load — the token is still
verified on every connection.

`POST /api/auth/bootstrap` creates the first administrator and refuses once any
user exists, so leaving it routed is not a standing hole.

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

## Deployment checklist

- [ ] `JWT_SECRET` and `MASTER_KEY` generated with `openssl rand -base64 32`
- [ ] `MASTER_KEY` backed up outside the repository
- [ ] `AGENTFLEET_ENV=production` (the orchestrator then refuses to start on
      development defaults)
- [ ] TLS terminated in front of the console and the API
- [ ] `MAX_INSTANCES` sized against real host RAM
- [ ] `ALLOW_SHELL=false` unless something actually needs to compile
- [ ] `block_local` on by default for every instance
- [ ] Postgres and artifact storage backed up — they hold the audit trail
