# AgentFleet Developer Guide

This guide covers setting up the development environment, running components locally, executing test suites, and understanding the codebase architecture.

---

## 1. Prerequisites

* **Docker & Docker Compose** (v24+)
* **Go** (1.22+)
* **Node.js** (v20+) & **npm**
* **Python** (3.10+)
* **Flutter** (3.22+) *(optional, for mobile app development)*
* **Ollama** *(optional, for local vision model testing with `qwen2.5vl:7b`)*

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
   * MinIO Console (optional): `http://localhost:9001`

---

## 3. Running Services Locally for Development

### Go Orchestrator (`backend/`)
The orchestrator requires a reachable PostgreSQL database:
```bash
# Start Postgres in Compose
docker compose up -d db

# Run orchestrator
cd backend
go run ./cmd/server
```

### React Admin Console (`admin/`)
```bash
cd admin
npm install
npm run dev
```
Runs the Vite development server with Hot Module Replacement on `http://localhost:5173`.

### In-Sandbox Daemon (`sandbox/agentd/`)
To test `agentd` locally outside Docker:
```bash
cd sandbox/agentd
pip install -r requirements.txt
python -m uvicorn main:app --port 8088
```

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

### Sandbox Python REPL Tests
```bash
cd sandbox/agentd
python test_repl.py
```

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
| `backend/internal/connectors/` | Multimodal model gateways (OpenAI, Anthropic, Gemini, Ollama, OpenAI-compatible) |
| `backend/internal/recorder/` | Raw input trace compiler and `SKILL.md` generator |
| `backend/internal/store/` | PostgreSQL repository and embedded schema migrations |
| `backend/internal/vault/` | AES-256-GCM sealed credential vault |
| `backend/internal/bus/` | WebSocket broadcast event bus |
| `backend/pkg/protocol/` | Shared Go types and JSON wire schemas |
| `sandbox/` | Ubuntu desktop Dockerfile, supervisord configuration, and `egress.sh` nftables firewall |
| `sandbox/agentd/` | In-sandbox daemon (`main.py`, `a11y.py`, `capture.py`, `inject.py`, `recorder.py`, `repl.py`) |
| `admin/` | React 19 + TypeScript + Vite + Tailwind CSS admin console |
| `mobile/` | Flutter companion app with live desktop streaming and push notification triage |
| `scripts/` | `doctor.sh` preflight and `smoke.sh` end-to-end integration tests |
