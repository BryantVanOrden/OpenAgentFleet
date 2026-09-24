# OpenAgentFleet Developer Guide

This guide covers setting up the development environment, running components locally, executing test suites, and understanding the codebase architecture.

---

## 1. Prerequisites

* **Docker & Docker Compose** (v24+)
* **Go** 1.25+ — `backend/go.mod` declares `go 1.25.0`, so an older toolchain
  will either refuse to build or silently download a newer one. CI uses 1.25.
* **Node.js** (v20+) & **npm** — the admin image builds on Node 22.
* **Python** (3.10+)
* **Flutter** 3.24+ *(optional, for the mobile app)* — `mobile/pubspec.yaml`
  requires Dart `>=3.5.0`, which means Flutter 3.24 or newer.
* **Ollama** *(optional, for local vision model testing with `qwen3.5:4b`)* —
  note it reports coordinates on a 0-1000 scale where `qwen2.5vl` reports
  pixels; `AGENT_COORD_SPACE=auto` covers both.

There is no `.tool-versions`, `mise.toml` or `.nvmrc` in the repository, so
nothing pins these for you.

---

## 2. Quick Setup with Docker Compose

1. **Preflight Check:**
   ```bash
   make doctor
   ```
   Checks Docker availability, system RAM, required open ports, and configured model endpoints.

2. **Generate Secrets & Environment:**
   ```bash
   make env
   ```
   Generates a `.env` file with cryptographically secure random `JWT_SECRET` and `MASTER_KEY` values.

3. **Build and Run Full Stack:**
   ```bash
   make up
   ```
   * Admin Console: `http://localhost:8081`
   * Orchestrator API: `http://localhost:8080/healthz`

   `make up` builds the sandbox image first, which `docker compose up` on its own
   does not. Without `agentfleet/sandbox:latest` present, provisioning the first
   bot fails.

   The `tts` service is built on every `make up` and is a multi-gigabyte
   Torch-based image. The first build takes a while; that is expected, not a
   hang.

   MinIO is behind the `s3` compose profile and is **not** started by `make up`.
   It only comes up with `COMPOSE_PROFILES=s3`, and its console is then on
   `http://localhost:9001`. Setting `ARTIFACT_BACKEND=s3` without starting it
   makes the orchestrator fail to boot rather than fall back.

---

## 3. Running Services Locally for Development

### Go Orchestrator (`backend/`)

The orchestrator needs a reachable PostgreSQL database. Migrations run
automatically at boot from embedded SQL, so the database needs to exist but not
to be prepared.

Two things trip this up, neither obvious:

- **The compose `db` service publishes no ports.** The Go default DSN points at
  `localhost:5432`, which will never reach the container. Either add a
  `ports: ["5432:5432"]` mapping to the `db` service, or point `DATABASE_URL` at
  a Postgres you are running yourself.
- **`ARTIFACT_DIR` defaults to `/var/lib/agentfleet/artifacts`**, which the
  orchestrator creates at start. As an ordinary user that is a permission error
  and the process exits. Set `ARTIFACT_DIR` somewhere writable.

```bash
# Postgres, reachable from the host
docker compose up -d db   # after adding a port mapping, or use your own

# Run the orchestrator
cd backend
DATABASE_URL='postgres://agentfleet:agentfleet@localhost:5432/agentfleet?sslmode=disable' \
ARTIFACT_DIR=/tmp/agentfleet-artifacts \
go run ./cmd/server
```

### React Admin Console (`admin/`)
```bash
cd admin
npm install
npm run dev
```
Runs the Vite development server with hot module replacement on
`http://localhost:5180` (set in `admin/vite.config.ts`, not Vite's 5173 default).

### In-Sandbox Daemon (`sandbox/agentd/`)
To test `agentd` locally outside Docker:
```bash
cd sandbox/agentd
pip install -r requirements.txt
python -m uvicorn main:app --port 7900
```

7900 is the port the orchestrator expects `agentd` on (`PortAgentd` in
`backend/internal/fleet/tiers.go`). Much of `agentd` needs an X display and a
running AT-SPI bus, so outside the container the GUI paths will not work.

### Flutter Companion App (`mobile/`)
```bash
cd mobile
flutter pub get
flutter run
```

---

## 4. Running Test Suites

### Backend Go Unit Tests
```bash
cd backend
go test -v ./...
```

The store tests, and the tests that exercise it through the API, skip
unless `AGENTFLEET_TEST_DSN` points at a throwaway Postgres (they create and
drop tables):

```bash
docker run -d --name af-testdb -e POSTGRES_PASSWORD=test -e POSTGRES_DB=agentfleet_test -p 55439:5432 postgres:16
AGENTFLEET_TEST_DSN=postgres://postgres:test@127.0.0.1:55439/agentfleet_test?sslmode=disable go test ./...
```

The ticket engine (`internal/tickets`) and the external-agent adapters
(`internal/external`) test against in-memory fakes, so they need neither a
database nor a CLI; run them with `-race`, since both are concurrent.

### Python SDK and `fleetctl`
```bash
cd sdk/python
python -m unittest discover -s tests
```

`test_runtimes.py` drives the host's Claude Code, Codex and Hermes runners
against fake CLIs placed with `AGENTFLEET_<RUNTIME>_BIN`, so no agent CLI
needs to be installed.

### Sandbox Python tests
```bash
cd sandbox/agentd
python -m unittest discover -p "test_*.py"
```

`python test_repl.py` runs one file of eleven.

### Admin Frontend Build & Type Check
```bash
cd admin
npm run build
npx vitest run
```

### Flutter app
```bash
cd mobile
flutter analyze
flutter test
```

### End-to-End Smoke Tests
```bash
# Provisions a sandbox, tests observation and action, verifies the screen changed
make smoke

# The same, plus a real autonomous task against a real model
make smoke-agent

# The subsystems that were once stubs. Needs an admin account.
VERIFY_EMAIL=you@example.com VERIFY_PASSWORD=... make verify-features
```

The VM tier's end-to-end test is deliberate and manual — a TCG boot takes
minutes, which is too slow for every CI run:

```bash
make sandbox-vm
# then create an instance with {"driver": "qemu"} and watch it reach running;
# verify-features covers the automated parts: a policied qemu instance is
# accepted, the runner's netns carries the nftables rules, the image exists.
```

The fleet chat has three layers of test. `go test ./internal/httpapi` covers
the command parser, the catalogue's integrity (every verb dispatched and
documented), the bot resolvers and the `ASK <bot>:` parser; `verify-features`
exercises `GET /api/fleet/commands` and `POST /api/fleet/command` against the
running stack; and `make app-chat-e2e` drives the phone's chat from the Flutter
web build (a quick chip, the command panel, `/help`). Marathon continuation is
pinned in `go test ./internal/agent` (policy, handover, prompt block); the live
check is a task created with a tiny `max_steps` (3) on a running bot — its
state should become `continued`, a successor with `params.window = 2` should
appear, and a `progress` alert should be filed.

`verify-features` registers a real MCP server in a container and calls a tool on
it, checks that pipeline conditions and cycles are rejected at save, that a swarm
refuses members that are not real instances, and that a Stripe delivery signed
the way Stripe signs it is accepted while the old body-only HMAC is not.

It exists because unit tests could not have caught what was wrong with most of
those subsystems. The MCP bridge passed every test it had while returning
invented tools and canned results; the tests asserted the shape of the response,
which was correct, rather than that anything had been executed. If you add a
subsystem that talks to something outside this process, add a check here as well
as a unit test.

---

## 5. Codebase Directory Map

| Path | Purpose |
| :--- | :--- |
| `backend/cmd/server/` | Orchestrator entrypoint |
| `backend/internal/agent/` | Perceive-decide-act loop, prompt building, action parsing, stall detection, sub-agent delegation, and continual refinement engine |
| `backend/internal/fleet/` | Docker Engine REST API client, hardware tier management, cgroups and quota enforcement |
| `backend/internal/connectors/` | Model gateways (OpenAI, Anthropic, Gemini, Antigravity, Ollama, OpenAI-compatible), the fallback chain, and role-based routing through model combinations |
| `backend/internal/pipeline/` | Multi-bot DAG pipelines. Independent stages run concurrently; edge conditions decide whether a stage runs or is skipped |
| `backend/internal/tickets/` | The ticket engine: checkout, blockers, reviews and verdicts, escalation up the org chart, liveness, verifiers, budgets |
| `backend/internal/external/` | Runs tickets on external agents: Claude Code, Codex and Hermes through `fleetctl host`, OpenClaw over its gateway protocol, webhooks; per-run callback tokens |
| `backend/internal/swarm/` | Shared-blackboard multi-bot swarms, with a task per member and peer review of artifacts |
| `backend/internal/memory/` | Episodic memory behind `remember` and `recall`: private per bot, plus a shared fleet pool |
| `backend/internal/mcp/` | MCP client — JSON-RPC 2.0 over stdio and Streamable HTTP |
| `backend/internal/voice/` | Text-to-speech sidecar client, shared by the API and the agent loop |
| `backend/internal/schedule/` | Cron expression parsing and the trigger scheduler |
| `backend/internal/telemetry/` | Token counting (including cached reads) and cost accounting |
| `backend/internal/notify/` | Push notification dispatch (FCM, APNs) |
| `backend/internal/artifacts/` | Screenshot and artifact storage, filesystem or S3 |
| `backend/internal/config/` | Environment configuration and its defaults |
| `backend/internal/recorder/` | Raw input trace compiler and `SKILL.md` generator |
| `backend/internal/store/` | PostgreSQL repository and embedded schema migrations |
| `backend/internal/vault/` | AES-256-GCM sealed credential vault |
| `backend/internal/bus/` | WebSocket broadcast event bus |
| `backend/pkg/protocol/` | Shared Go types and JSON wire schemas |
| `sandbox/` | Ubuntu desktop Dockerfile, supervisord configuration, and `egress.sh` nftables firewall |
| `sandbox/agentd/` | In-sandbox daemon (`main.py`, `a11y.py`, `capture.py`, `inject.py`, `recorder.py`, `repl.py`, `som.py`, `snapshot.py`, `search.py`, `voice.py`) |
| `admin/` | React 19 + TypeScript + Vite + Tailwind CSS admin console |
| `mobile/` | Flutter companion app with live desktop streaming and push notification triage |
| `sdk/python/` | The `open-agent-fleet` package: the Python SDK, the `fleetctl` CLI, and `fleetctl host` with its agent-CLI runners (`runtimes.py`) |
| `scripts/` | `doctor.sh` preflight, `smoke.sh` end-to-end test, `quickstart.sh` / `quickstart.ps1` bootstrap, `screenshots.mjs` |
