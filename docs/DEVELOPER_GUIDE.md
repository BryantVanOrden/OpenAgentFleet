# AgentFleet Developer Guide

This guide covers setting up the development environment, running components locally, executing test suites, and understanding the codebase architecture.

---

## 1. Prerequisites

* **Docker & Docker Compose** (v24+)
* **Go** 1.25+ — `backend/go.mod` declares `go 1.25.0`, so an older toolchain
  will either refuse to build or silently download a newer one. Note that CI is
  currently pinned to Go 1.23, which is a mismatch worth fixing.
* **Node.js** (v20+) & **npm** — the admin image builds on Node 22.
* **Python** (3.10+)
* **Flutter** 3.24+ *(optional, for the mobile app)* — `mobile/pubspec.yaml`
  requires Dart `>=3.5.0`, which means Flutter 3.24 or newer.
* **Ollama** *(optional, for local vision model testing with `qwen2.5vl:7b`)*

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
```

### End-to-End Smoke Tests
```bash
# Smoke test (provisions sandbox, tests observation/action, verifies screen dhash change)
make smoke

# Full autonomous agent smoke test with real LLM
make smoke-agent
```

---

## 5. Codebase Directory Map

| Path | Purpose |
| :--- | :--- |
| `backend/cmd/server/` | Orchestrator entrypoint |
| `backend/internal/agent/` | Perceive-decide-act loop, prompt building, action parsing, stall detection, sub-agent delegation, and continual refinement engine |
| `backend/internal/fleet/` | Docker Engine REST API client, hardware tier management, cgroups and quota enforcement |
| `backend/internal/connectors/` | Model gateways (OpenAI, Anthropic, Gemini, Antigravity, Ollama, OpenAI-compatible), the fallback chain, and role-based routing through model combinations |
| `backend/internal/pipeline/` | Multi-bot DAG pipelines. Topological order, executed one node at a time |
| `backend/internal/swarm/` | Shared-blackboard multi-bot swarms |
| `backend/internal/memory/` | Per-instance episodic memory behind the `remember` and `recall` actions |
| `backend/internal/mcp/` | MCP bridge for custom tool servers |
| `backend/internal/schedule/` | Cron expression parsing and the trigger scheduler |
| `backend/internal/telemetry/` | Token counting and cost accounting |
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
| `scripts/` | `doctor.sh` preflight, `smoke.sh` end-to-end test, `quickstart.sh` / `quickstart.ps1` bootstrap, `screenshots.mjs` |
