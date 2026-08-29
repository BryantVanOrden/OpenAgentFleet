SHELL := /bin/bash
COMPOSE := docker compose

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: env
env: ## Create .env from the example and generate secrets
# Every line of a recipe runs in its own shell, so the `exit 0` this guard used
# to rely on ended only that line — `cp` and the secret generation still ran on
# the next one. `make up` therefore overwrote an existing .env and rotated
# MASTER_KEY on every invocation, which by the note below silently destroyed
# every credential already sealed into the vault. Keep the whole thing in one
# shell so the early return actually returns.
	@if [ -f .env ]; then \
	   echo ".env already exists — not touching it"; \
	 else \
	   cp .env.example .env; \
	   jwt=$$(openssl rand -base64 32); master=$$(openssl rand -base64 32); \
	   if docker info --format '{{.OperatingSystem}}' 2>/dev/null | grep -qi 'docker desktop'; then \
	     gid=0; \
	   else \
	     gid=$$(stat -c %g /var/run/docker.sock 2>/dev/null || echo 0); \
	   fi; \
	   sed -i.bak "s|^JWT_SECRET=.*|JWT_SECRET=$$jwt|; s|^MASTER_KEY=.*|MASTER_KEY=$$master|; s|^DOCKER_GID=.*|DOCKER_GID=$$gid|" .env && rm -f .env.bak; \
	   echo "Wrote .env with fresh secrets. Back up MASTER_KEY — losing it means losing every stored credential."; \
	 fi

.PHONY: doctor
doctor: ## Check the host, config, images, ports and models before you start
	@bash scripts/doctor.sh

.PHONY: smoke
smoke: ## End-to-end test against a running stack (provisions and destroys a sandbox)
	@bash scripts/smoke.sh

.PHONY: smoke-agent
smoke-agent: ## Same, plus a real autonomous task against a real model
	@bash scripts/smoke.sh --with-agent

.PHONY: screenshots
screenshots: ## Recapture the documentation screenshots from the running console
	@docker run --rm --network agentfleet_control -v "$(CURDIR)/scripts:/scripts:ro" -v "$(CURDIR)/docs/images:/out" -e BASE_URL=http://admin:80 -e AF_EMAIL="$${SHOT_EMAIL:-demo@agentfleet.local}" -e AF_PASSWORD="$${SHOT_PASSWORD:-agentfleet-demo-1234}" -e PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 mcr.microsoft.com/playwright:v1.49.1-noble sh -c 'mkdir -p /tmp/pw && cd /tmp/pw && npm i --silent --no-audit --no-fund playwright@1.49.1 >/dev/null 2>&1 && cp /scripts/screenshots.mjs . && node screenshots.mjs'

.PHONY: sandbox
sandbox: ## Build the sandbox desktop image
	docker build -t agentfleet/sandbox:latest ./sandbox

.PHONY: up
up: env sandbox ## Build the sandbox image and start the whole stack
	$(COMPOSE) up -d --build
	@echo
	@echo "Console:  http://localhost:$${ADMIN_PORT:-8081}"
	@echo "API:      http://localhost:$${API_PORT:-8080}/healthz"

.PHONY: down
down: ## Stop the stack (sandboxes are left running)
	$(COMPOSE) down

.PHONY: clean-sandboxes
clean-sandboxes: ## Destroy every sandbox container this platform created
	@ids=$$(docker ps -aq --filter label=managed-by=agentfleet); \
	 if [ -n "$$ids" ]; then docker rm -f $$ids; else echo "no sandboxes running"; fi

.PHONY: nuke
nuke: down clean-sandboxes ## Stop everything and delete all data
	$(COMPOSE) down -v

.PHONY: logs
logs: ## Tail orchestrator logs
	$(COMPOSE) logs -f api

.PHONY: psql
psql: ## Open a database shell
	$(COMPOSE) exec db psql -U $${POSTGRES_USER:-agentfleet} -d $${POSTGRES_DB:-agentfleet}

# --- development -------------------------------------------------------------

.PHONY: backend
backend: ## Run the orchestrator locally (needs Go and a reachable Postgres)
	cd backend && go run ./cmd/server

.PHONY: admin
admin: ## Run the admin console dev server
	cd admin && npm install && npm run dev

.PHONY: mobile
mobile: ## Run the Flutter companion app
	cd mobile && flutter pub get && flutter run

.PHONY: test
test: ## Run every test suite across Go, Python agentd, Python SDK, React Admin, and Flutter
	cd backend && go test ./...
	cd sandbox/agentd && python -m unittest discover -p "test_*.py"
	cd sdk/python && python -m unittest discover -s tests -p "test_*.py"
	cd admin && npm run build
	cd mobile && flutter analyze

.PHONY: fmt
fmt: ## Format Go and Dart sources
	cd backend && gofmt -w .
	cd mobile && dart format lib

.PHONY: vet
vet: ## Static analysis
	cd backend && go vet ./...
