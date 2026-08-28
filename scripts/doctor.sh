#!/usr/bin/env bash
# Preflight. Answers "why isn't this working?" before you have to ask it.
#
# Every check prints what it found, not just pass/fail — a wrong answer is more
# useful than a red cross when you are trying to get unstuck.

set -uo pipefail

PASS=0
WARN=0
FAIL=0

ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS+1)); }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; WARN=$((WARN+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$1"; FAIL=$((FAIL+1)); }
head_() { printf '\n\033[1m%s\033[0m\n' "$1"; }

API_PORT="${API_PORT:-8080}"
ADMIN_PORT="${ADMIN_PORT:-8081}"

# ---------------------------------------------------------------- host tools ---

head_ "Host"

if command -v docker >/dev/null 2>&1; then
    if server=$(docker version --format '{{.Server.Version}}' 2>/dev/null); then
        ok "docker engine $server"
    else
        bad "docker is installed but the daemon is not reachable — is Docker Desktop running?"
    fi
else
    bad "docker not found — required"
fi

if docker compose version >/dev/null 2>&1; then
    ok "docker compose $(docker compose version --short 2>/dev/null)"
else
    bad "docker compose v2 not found — required"
fi

# Sandboxes are memory-hungry. Four power-user instances is 32 GB before the
# host is counted, and the failure mode is an OOM kill mid-build.
if total_kb=$(awk '/MemTotal/ {print $2}' /proc/meminfo 2>/dev/null) && [[ -n "$total_kb" ]]; then
    total_gb=$((total_kb / 1024 / 1024))
    if (( total_gb >= 16 )); then
        ok "${total_gb} GB RAM"
    elif (( total_gb >= 8 )); then
        warn "${total_gb} GB RAM — fine for micro/standard tiers, tight for power-user"
    else
        warn "${total_gb} GB RAM — expect OOM kills above the micro tier"
    fi
fi

if avail=$(df -Pk . 2>/dev/null | awk 'NR==2 {print int($4/1024/1024)}'); then
    if (( avail >= 30 )); then
        ok "${avail} GB free disk"
    else
        warn "${avail} GB free disk — the sandbox image alone is ~3 GB"
    fi
fi

# ------------------------------------------------------------ configuration ---

head_ "Configuration"

if [[ -f .env ]]; then
    ok ".env present"
    # shellcheck disable=SC1091
    set -a; source .env 2>/dev/null; set +a

    if [[ -n "${JWT_SECRET:-}" ]]; then ok "JWT_SECRET set"; else bad "JWT_SECRET empty — run: make env"; fi
    if [[ -n "${MASTER_KEY:-}" ]]; then
        ok "MASTER_KEY set"
        printf '    \033[2mlosing this makes every stored credential unreadable — back it up\033[0m\n'
    else
        bad "MASTER_KEY empty — run: make env"
    fi

    if [[ -S /var/run/docker.sock ]]; then
        actual=$(stat -c %g /var/run/docker.sock 2>/dev/null || echo "")
        if [[ -n "$actual" && "${DOCKER_GID:-}" != "$actual" ]]; then
            bad "DOCKER_GID=${DOCKER_GID:-unset} but the socket is owned by group $actual — the orchestrator will not be able to provision"
        elif [[ -n "$actual" ]]; then
            ok "DOCKER_GID matches the socket ($actual)"
        fi
    fi
else
    bad ".env missing — run: make env"
fi

# ------------------------------------------------------------------- images ---

head_ "Images"

image="${SANDBOX_IMAGE:-agentfleet/sandbox:latest}"
if docker image inspect "$image" >/dev/null 2>&1; then
    size=$(docker image inspect "$image" --format '{{.Size}}' 2>/dev/null)
    ok "$image present ($((size / 1024 / 1024)) MB)"
else
    bad "$image not built — run: make sandbox"
fi

# -------------------------------------------------------------------- ports ---

head_ "Ports"

port_busy() {
    if command -v ss >/dev/null 2>&1; then ss -lnt 2>/dev/null | grep -q ":$1 "
    elif command -v netstat >/dev/null 2>&1; then netstat -an 2>/dev/null | grep -q "[:.]$1 .*LISTEN"
    else return 1
    fi
}

for entry in "$API_PORT:orchestrator" "$ADMIN_PORT:console"; do
    port="${entry%%:*}"; what="${entry##*:}"
    if port_busy "$port"; then
        if curl -fsS "http://localhost:$port/healthz" >/dev/null 2>&1; then
            ok "port $port — AgentFleet $what already running"
        else
            warn "port $port is in use by something else — set ${what^^}_PORT in .env"
        fi
    else
        ok "port $port free"
    fi
done

# ------------------------------------------------------------------- models ---

head_ "Models"

ollama="${OLLAMA_BASE_URL:-http://localhost:11434}"
probe="${ollama/host.docker.internal/localhost}"
if tags=$(curl -fsS --max-time 4 "$probe/api/tags" 2>/dev/null); then
    count=$(printf '%s' "$tags" | grep -o '"name"' | wc -l | tr -d ' ')
    ok "ollama reachable at $probe ($count models)"

    want="${OLLAMA_VISION_MODEL:-qwen2.5vl:7b}"
    if printf '%s' "$tags" | grep -q "\"${want%%:*}"; then
        ok "vision model $want is pulled"
    else
        # This is the single most common reason a first run does nothing useful:
        # a text-only model cannot see the desktop, so every step is a guess.
        warn "vision model $want not pulled — run: ollama pull $want"
    fi
else
    warn "no ollama at $probe — fine if you are using a cloud provider instead"
fi

# ------------------------------------------------------------------ running ---

head_ "Running stack"

if health=$(curl -fsS --max-time 4 "http://localhost:$API_PORT/healthz" 2>/dev/null); then
    ok "orchestrator healthy"
    printf '    \033[2m%s\033[0m\n' "$health"
else
    warn "orchestrator not responding — run: make up"
fi

live=$(docker ps -q --filter label=managed-by=agentfleet 2>/dev/null | wc -l | tr -d ' ')
if [[ "$live" != "0" ]]; then
    ok "$live sandbox container(s) running"
fi

# ------------------------------------------------------------------ summary ---

printf '\n\033[1m%d passed, %d warnings, %d failures\033[0m\n' "$PASS" "$WARN" "$FAIL"
if (( FAIL > 0 )); then
    printf 'Fix the failures above before running the stack.\n'
    exit 1
fi
printf 'Ready.\n'
