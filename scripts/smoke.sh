#!/usr/bin/env bash
# End-to-end smoke test against a running stack.
#
# Exercises the real path a first-time user takes: bootstrap an admin, provision
# a sandbox, prove the desktop is alive and drivable, drive it by hand, record a
# demonstration, run an autonomous task, and tear it all down.
#
#   ./scripts/smoke.sh              # everything except the autonomous task
#   ./scripts/smoke.sh --with-agent # also run a real task through a real model
#   ./scripts/smoke.sh --keep       # leave the instance running afterwards
#
# The agent step is opt-in because it costs tokens and needs a working vision
# model; everything else is deterministic and free.

set -uo pipefail

API="${API:-http://localhost:8080}"
EMAIL="${SMOKE_EMAIL:-smoke@agentfleet.local}"
PASSWORD="${SMOKE_PASSWORD:-smoke-test-password-1234}"
WITH_AGENT=0
KEEP=0

for arg in "$@"; do
    case "$arg" in
        --with-agent) WITH_AGENT=1 ;;
        --keep) KEEP=1 ;;
        *) echo "unknown flag: $arg"; exit 2 ;;
    esac
done

PASS=0
FAIL=0
INSTANCE_ID=""
TOKEN=""

ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$1"; FAIL=$((FAIL+1)); }
info() { printf '    \033[2m%s\033[0m\n' "$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$1"; }

cleanup() {
    if [[ -n "$INSTANCE_ID" && "$KEEP" == "0" ]]; then
        step "Cleanup"
        if api DELETE "/api/instances/$INSTANCE_ID" >/dev/null; then
            ok "instance destroyed"
        else
            bad "could not destroy instance $INSTANCE_ID — remove it by hand"
        fi
    elif [[ -n "$INSTANCE_ID" ]]; then
        printf '\nLeaving instance %s running (--keep).\n' "$INSTANCE_ID"
    fi

    printf '\n\033[1m%d passed, %d failed\033[0m\n' "$PASS" "$FAIL"
    (( FAIL > 0 )) && exit 1
    exit 0
}
trap cleanup EXIT

# api METHOD PATH [BODY] -> response body on stdout, non-zero on HTTP >= 400
api() {
    local method="$1" path="$2" body="${3:-}"
    local args=(-sS -X "$method" -w '\n%{http_code}')
    [[ -n "$TOKEN" ]] && args+=(-H "Authorization: Bearer $TOKEN")
    [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' -d "$body")

    local raw code
    raw=$(curl "${args[@]}" "$API$path" 2>/dev/null)
    code="${raw##*$'\n'}"
    printf '%s' "${raw%$'\n'*}"
    [[ "$code" =~ ^2 ]]
}

# Minimal JSON string extraction. jq would be better; not assuming it is present.
jget() { grep -o "\"$1\":\"[^\"]*\"" | head -1 | sed "s/.*\"$1\":\"//;s/\"$//"; }

# ---------------------------------------------------------------- 1. reachable ---

step "1. Orchestrator"

if health=$(api GET /healthz); then
    ok "healthy"
    info "$health"
else
    bad "not reachable at $API — is the stack up?"
    exit 1
fi

# ------------------------------------------------------------------- 2. auth ---

step "2. Authentication"

# Bootstrap only works on a virgin deployment; on a used one we just log in.
if out=$(api POST /api/auth/bootstrap "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"); then
    TOKEN=$(printf '%s' "$out" | jget token)
    ok "bootstrapped the first administrator"
else
    if out=$(api POST /api/auth/login "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"); then
        TOKEN=$(printf '%s' "$out" | jget token)
        ok "logged in as an existing user"
    else
        bad "cannot authenticate — set SMOKE_EMAIL/SMOKE_PASSWORD to a real account"
        exit 1
    fi
fi
[[ -n "$TOKEN" ]] && ok "session token issued" || { bad "no token in the response"; exit 1; }

if api GET /api/instances >/dev/null; then ok "token accepted"; else bad "token rejected"; fi

# An unauthenticated call must be refused — a smoke test that only proves the
# happy path would not notice auth being wired up backwards.
if TOKEN="" api GET /api/instances >/dev/null 2>&1; then
    bad "unauthenticated request was ACCEPTED — authentication is not enforced"
else
    ok "unauthenticated request rejected"
fi

# ------------------------------------------------------------------ 3. tiers ---

step "3. Fleet catalogue"

if tiers=$(api GET /api/tiers); then
    for t in micro standard power-user developer-heavy; do
        printf '%s' "$tiers" | grep -q "\"$t\"" && ok "tier $t offered" || bad "tier $t missing"
    done
else
    bad "could not list tiers"
fi

# ------------------------------------------------------------ 4. provisioning ---

step "4. Provision a sandbox"
info "building the desktop takes ~30-60s on first boot"

body='{"name":"smoke-test","tier":"micro","shell_access":true,
       "egress":{"block_local":true},"override":{}}'
if out=$(api POST /api/instances "$body"); then
    INSTANCE_ID=$(printf '%s' "$out" | jget id)
    ok "provisioned $INSTANCE_ID"
else
    bad "provisioning failed"
    info "$out"
    exit 1
fi

state=$(printf '%s' "$out" | jget state)
[[ "$state" == "running" ]] && ok "reached state=running" || bad "state=$state (expected running)"

# DNS must survive the egress policy. It didn't, once: egress.sh flushed the
# whole ruleset, which took Docker's embedded-DNS NAT rules with it, and every
# policied sandbox came up unable to resolve anything. The rules LOOKED right —
# only asking the resolver from inside catches this class of break.
if docker exec "af-${INSTANCE_ID:0:12}" getent ahostsv4 example.com >/dev/null 2>&1; then
    ok "DNS resolves inside the policied sandbox"
else
    bad "DNS is dead inside the policied sandbox — the egress policy broke the resolver"
fi

# ------------------------------------------------------------- 5. perception ---

step "5. The agent can see"

if obs=$(api GET "/api/instances/$INSTANCE_ID/observe?a11y=true"); then
    if printf '%s' "$obs" | grep -q '"screenshot_b64":"[A-Za-z0-9+/]'; then
        bytes=$(printf '%s' "$obs" | grep -o '"screenshot_b64":"[^"]*"' | wc -c)
        ok "screenshot captured (~$((bytes * 3 / 4 / 1024)) KB of WebP)"
    else
        bad "screenshot is empty — Xvfb or the capture path is broken"
    fi

    hash=$(printf '%s' "$obs" | jget hash)
    [[ -n "$hash" && "$hash" != "0000000000000000" ]] \
        && ok "perceptual hash computed ($hash)" \
        || bad "hash is empty or all-zero — stall detection will not work"

    # A tree is not guaranteed on a bare desktop with nothing focused, so this
    # is reported rather than failed.
    if printf '%s' "$obs" | grep -q '"a11y_tree":"[^"]'; then
        ok "accessibility tree populated"
    else
        printf '  \033[33m!\033[0m accessibility tree empty — AT-SPI may not have started\n'
    fi
else
    bad "observe failed — agentd is not answering"
fi

# ----------------------------------------------------------------- 6. action ---

step "6. The agent can act"

# xfce4-terminal is the cheapest thing to open that proves input injection works
# end to end: keystroke -> X server -> window manager -> new window.
if api POST "/api/instances/$INSTANCE_ID/act" \
     '{"action":"shell","text":"xfce4-terminal & sleep 3; echo spawned"}' >/dev/null; then
    ok "shell action executed"
else
    bad "shell action failed"
fi

if res=$(api POST "/api/instances/$INSTANCE_ID/act" \
     '{"action":"type","text":"echo agentfleet-smoke-ok"}'); then
    printf '%s' "$res" | grep -q '"ok":true' && ok "keystrokes injected" || bad "type action rejected: $res"
else
    bad "type action failed"
fi

if res=$(api POST "/api/instances/$INSTANCE_ID/act" '{"action":"key","key":"Return"}'); then
    printf '%s' "$res" | grep -q '"ok":true' && ok "key action injected" || bad "key action rejected"
fi

# The screen must actually have changed after all that. If the hash is identical
# the agent would be flying blind and stall detection would misfire.
sleep 2
if obs2=$(api GET "/api/instances/$INSTANCE_ID/observe?a11y=false"); then
    hash2=$(printf '%s' "$obs2" | jget hash)
    [[ -n "${hash:-}" && "$hash2" != "$hash" ]] \
        && ok "screen changed after input (hash $hash -> $hash2)" \
        || bad "screen did NOT change after opening a terminal — input injection is not reaching the desktop"
fi

# An action the instance is not allowed to take must be refused.
if res=$(api POST "/api/instances/$INSTANCE_ID/act" '{"action":"nonsense"}'); then
    printf '%s' "$res" | grep -q '"ok":false' \
        && ok "unknown action refused" \
        || bad "unknown action was accepted"
fi

# ------------------------------------------------------------- 7. desktop proxy ---

step "7. Desktop stream"

code=$(curl -sS -o /dev/null -w '%{http_code}' "$API/vnc/$INSTANCE_ID/vnc.html?token=$TOKEN" 2>/dev/null)
[[ "$code" == "200" ]] && ok "noVNC served through the authenticated proxy" \
                       || bad "desktop proxy returned $code"

code=$(curl -sS -o /dev/null -w '%{http_code}' "$API/vnc/$INSTANCE_ID/vnc.html" 2>/dev/null)
[[ "$code" == "401" ]] && ok "desktop proxy rejects an unauthenticated request" \
                       || bad "desktop proxy returned $code without a token — it should be 401"

# ---------------------------------------------------------------- 8. recorder ---

step "8. Recording"

if api POST "/api/instances/$INSTANCE_ID/record/start" '{"name":"smoke recording"}' >/dev/null; then
    ok "recording started"
    api POST "/api/instances/$INSTANCE_ID/act" '{"action":"type","text":"hello"}' >/dev/null
    api POST "/api/instances/$INSTANCE_ID/act" '{"action":"key","key":"Return"}' >/dev/null
    sleep 1

    if skill=$(api POST "/api/instances/$INSTANCE_ID/record/stop"); then
        steps=$(printf '%s' "$skill" | grep -o '"index":' | wc -l | tr -d ' ')
        ok "recording compiled into $steps step(s)"
        printf '%s' "$skill" | grep -q '"markdown":"#' \
            && ok "SKILL.md rendered" \
            || bad "SKILL.md is empty"
    else
        bad "stopping the recording failed"
    fi
else
    bad "could not start recording"
fi

# ------------------------------------------------------------------ 9. alerts ---

step "9. Alerts and events"

api GET "/api/alerts?open=true" >/dev/null && ok "alert feed readable" || bad "alert feed failed"
api GET "/api/skills" >/dev/null && ok "skill catalogue readable" || bad "skill catalogue failed"
api GET "/api/providers" >/dev/null && ok "provider list readable" || bad "provider list failed"

# ------------------------------------------------------------- 10. the agent ---

if (( WITH_AGENT )); then
    step "10. Autonomous task"
    info "this calls a real model and costs real tokens"

    if out=$(api POST /api/tasks "{\"instance_id\":\"$INSTANCE_ID\",
            \"goal\":\"Open a terminal and run 'echo hello from the agent'. Then you are done.\",
            \"max_steps\":8}"); then
        task_id=$(printf '%s' "$out" | jget id)
        ok "task $task_id queued"

        for _ in $(seq 1 60); do
            sleep 5
            snapshot=$(api GET "/api/tasks/$task_id")
            state=$(printf '%s' "$snapshot" | jget state)
            printf '\r    \033[2mstate: %-16s\033[0m' "$state"
            case "$state" in
                succeeded) printf '\n'; ok "task succeeded"; break ;;
                failed)    printf '\n'; bad "task failed: $(printf '%s' "$snapshot" | jget error)"; break ;;
                cancelled) printf '\n'; bad "task was cancelled"; break ;;
                awaiting_human) printf '\n'; ok "agent escalated to a human (a valid outcome)"; api POST "/api/tasks/$task_id/cancel" >/dev/null; break ;;
            esac
        done

        if steps=$(api GET "/api/tasks/$task_id/steps"); then
            n=$(printf '%s' "$steps" | grep -o '"step":' | wc -l | tr -d ' ')
            (( n > 0 )) && ok "$n step(s) recorded with screenshots" || bad "no steps were persisted"
        fi
    else
        bad "could not create the task"
        info "$out"
    fi
else
    step "10. Autonomous task"
    info "skipped — pass --with-agent to run a real model against the desktop"
fi
