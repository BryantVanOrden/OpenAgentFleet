#!/usr/bin/env bash
# Verifies the features the README once listed as "partly built", against a
# running stack.
#
# scripts/smoke.sh covers the core path: provision a desktop, see it, drive it,
# record a skill. This covers the nine subsystems that were stubs, and it exists
# because unit tests could not have caught what was wrong with most of them —
# the MCP bridge passed every test it had while speaking no protocol, and a
# Stripe webhook was rejected on every real delivery while the generic HMAC
# tests were green.
#
# Needs an admin account. Set VERIFY_EMAIL / VERIFY_PASSWORD.
#
#   bash scripts/verify-features.sh
#
# A real MCP server is started as a container on the control network, so this
# needs Docker and the stack's network to exist.

set -uo pipefail

BASE="${BASE_URL:-http://localhost:8080}"
EMAIL="${VERIFY_EMAIL:-${SMOKE_EMAIL:-}}"
PASSWORD="${VERIFY_PASSWORD:-${SMOKE_PASSWORD:-}}"
NETWORK="${CONTROL_NETWORK:-agentfleet_control}"

pass=0
fail=0
TOKEN=""
CLEANUP=()

ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; pass=$((pass + 1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$1"; fail=$((fail + 1)); }
head() { printf '\n\033[1m%s\033[0m\n' "$1"; }
note() { printf '    %s\n' "$1"; }

# Retries are for the transport, not the API: Docker Desktop's port proxy on a
# loaded host can reset or stall a connection, and a battery that reports a
# real feature as broken because one socket blinked is a battery nobody trusts.
api() {
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -m 120 --retry 3 --retry-all-errors --retry-delay 1 -X "$method" "$BASE$path" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body"
  else
    curl -sS -m 120 --retry 3 --retry-all-errors --retry-delay 1 -X "$method" "$BASE$path" -H "Authorization: Bearer $TOKEN"
  fi
}

# has <json> <needle> — substring check, so this needs no jq.
has() { case "$1" in *"$2"*) return 0 ;; *) return 1 ;; esac; }

cleanup() {
  for cmd in "${CLEANUP[@]:-}"; do [ -n "$cmd" ] && eval "$cmd" >/dev/null 2>&1; done
}
trap cleanup EXIT

# ---------------------------------------------------------------------- auth ---

head "0. Authentication"
if [ -z "$EMAIL" ] || [ -z "$PASSWORD" ]; then
  bad "set VERIFY_EMAIL and VERIFY_PASSWORD to an admin account"
  echo; echo "$pass passed, $fail failed"; exit 1
fi
login=$(curl -sS -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
TOKEN=$(printf '%s' "$login" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
if [ -z "$TOKEN" ]; then
  bad "could not authenticate as $EMAIL"
  echo; echo "$pass passed, $fail failed"; exit 1
fi
ok "authenticated as $EMAIL"

# ----------------------------------------------------------------------- MCP ---

head "1. MCP bridge (was: spoke no protocol)"

# A real MCP server, so this tests the protocol rather than a mock of it.
#
# The source is base64'd into the container rather than bind-mounted. A mount
# needs a host path, and this script is run from Git Bash on Windows as often as
# from Linux — where MSYS rewrites /srv into C:/Program Files/Git/srv and the
# mount silently becomes something else.
srvfile=$(mktemp)
cat > "$srvfile" <<'PY'
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

TOOLS = [{"name": "add_numbers", "description": "Adds two numbers",
          "inputSchema": {"type": "object",
                          "properties": {"a": {"type": "number"}, "b": {"type": "number"}},
                          "required": ["a", "b"]}}]

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(n) or b"{}")
        m = body.get("method")
        if m == "notifications/initialized":
            self.send_response(202); self.end_headers(); return
        if m == "initialize":
            r = {"protocolVersion": "2025-06-18",
                 "serverInfo": {"name": "verify-mcp", "version": "1"},
                 "capabilities": {"tools": {}}}
        elif m == "tools/list":
            r = {"tools": TOOLS}
        elif m == "tools/call":
            a = body.get("params", {}).get("arguments", {})
            r = {"content": [{"type": "text", "text": f"sum={a.get('a',0)+a.get('b',0)}"}]}
        else:
            r = None
        out = json.dumps({"jsonrpc": "2.0", "id": body.get("id"), "result": r}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Mcp-Session-Id", "s1")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

HTTPServer(("0.0.0.0", 9911), H).serve_forever()
PY

b64=$(base64 -w0 < "$srvfile" 2>/dev/null || base64 < "$srvfile" | tr -d '\n')
rm -f "$srvfile"

if docker run -d --rm --name af-verify-mcp --network "$NETWORK" python:3.12-slim \
      sh -c "echo '$b64' | base64 -d > /tmp/s.py && python /tmp/s.py" >/dev/null 2>&1; then
  CLEANUP+=("docker rm -f af-verify-mcp")
  # Give the server a moment to bind before the handshake.
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    docker exec af-verify-mcp sh -c 'exit 0' >/dev/null 2>&1 && break
    sleep 1
  done
  sleep 2

  reg=$(api POST /api/mcp/servers \
    '{"name":"verify","transport":"http","url":"http://af-verify-mcp:9911/mcp","env":{"X-Probe":"secret-value"}}')
  mcpid=$(printf '%s' "$reg" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')

  if has "$reg" '"tools_count":1'; then
    ok "registered a real server and discovered its tool over JSON-RPC"
  else
    bad "registration did not discover the server's tool: $reg"
  fi

  # The invented tool the old implementation produced.
  tools=$(api GET /api/mcp/tools)
  if has "$tools" 'add_numbers' && ! has "$tools" 'verify_query'; then
    ok "the tool name came from the server, not from a template"
  else
    bad "tool list looks synthesised: $tools"
  fi

  # An argument round-trip a canned response cannot fake.
  called=$(api POST /api/mcp/call '{"tool_name":"add_numbers","params":{"a":17,"b":25}}')
  if has "$called" 'sum=42'; then
    ok "called the tool for real (17+25 came back 42)"
  else
    bad "tool call did not reach the server: $called"
  fi

  if has "$reg" 'env_keys' && ! has "$reg" 'secret-value'; then
    ok "the server's credential is redacted to key names"
  else
    bad "a credential leaked into the API response"
  fi

  [ -n "$mcpid" ] && CLEANUP+=("curl -sS -X DELETE '$BASE/api/mcp/servers/$mcpid' -H 'Authorization: Bearer $TOKEN'")
else
  note "skipped — could not start a container on network $NETWORK"
fi

bad_reg=$(api POST /api/mcp/servers '{"name":"nope","transport":"http","url":"http://127.0.0.1:1/mcp"}')
if has "$bad_reg" '"error"'; then
  ok "an unreachable server is refused instead of stored as configured"
else
  bad "registering an unreachable server succeeded: $bad_reg"
fi

# ------------------------------------------------------------------ pipelines ---

head "2. Pipelines (was: sequential, conditions ignored, no builder)"

pipe=$(api POST /api/pipelines '{
  "name":"verify branches","max_parallel":2,
  "nodes":[{"id":"a","name":"deploy","archetype_id":"fullstack_dev","goal_template":"deploy"},
           {"id":"b","name":"announce","archetype_id":"fullstack_dev","goal_template":"announce"},
           {"id":"c","name":"rollback","archetype_id":"fullstack_dev","goal_template":"roll back"}],
  "edges":[{"from_node_id":"a","to_node_id":"b","condition":"success"},
           {"from_node_id":"a","to_node_id":"c","condition":"failure"}]}')
pipeid=$(printf '%s' "$pipe" | sed -n 's/.*"id":"\(pipe-[^"]*\)".*/\1/p')
if [ -n "$pipeid" ]; then
  ok "saved a pipeline with success and failure branches"
  CLEANUP+=("curl -sS -X DELETE '$BASE/api/pipelines/$pipeid' -H 'Authorization: Bearer $TOKEN'")
else
  bad "could not save a conditional pipeline: $pipe"
fi

if has "$pipe" '"max_parallel":2'; then
  ok "the parallelism bound round-trips"
else
  bad "max_parallel was dropped: $pipe"
fi

typo=$(api POST /api/pipelines '{"name":"typo",
  "nodes":[{"id":"a","goal_template":"x","archetype_id":"y"},{"id":"b","goal_template":"x","archetype_id":"y"}],
  "edges":[{"from_node_id":"a","to_node_id":"b","condition":"sucess"}]}')
if has "$typo" 'unknown edge condition'; then
  ok "a misspelled condition is rejected at save, not silently ignored"
else
  bad "a misspelled condition was accepted: $typo"
fi

cycle=$(api POST /api/pipelines '{"name":"cycle",
  "nodes":[{"id":"a","goal_template":"x","archetype_id":"y"},{"id":"b","goal_template":"x","archetype_id":"y"}],
  "edges":[{"from_node_id":"a","to_node_id":"b"},{"from_node_id":"b","to_node_id":"a"}]}')
if has "$cycle" 'cycle'; then
  ok "a cyclic graph is rejected"
else
  bad "a cycle was accepted: $cycle"
fi

# --------------------------------------------------------------------- swarms ---

head "3. Swarms (was: fabricated members, started nothing)"

noteam=$(api POST /api/swarms '{"name":"ghost","mission":"do things"}')
if has "$noteam" 'at least one member'; then
  ok "a swarm with no members is refused rather than staffed with invented bots"
else
  bad "a memberless swarm was accepted: $noteam"
fi

ghost=$(api POST /api/swarms '{"name":"t","mission":"m","members":[{"instance_id":"inst-lead","role":"Lead"}]}')
if has "$ghost" 'not a bot on this fleet'; then
  ok "a member that is not a real instance is refused"
else
  bad "a fabricated instance id was accepted: $ghost"
fi

# ------------------------------------------------------------------- webhooks ---

head "4. Webhooks (was: generic HMAC only; Stripe never worked)"

nosecret=$(api POST /api/webhooks \
  '{"name":"w","target_archetype":"fullstack_dev","goal_template":"g"}')
if has "$nosecret" 'signing secret'; then
  ok "an unsigned webhook is refused"
else
  bad "a webhook with no secret was created: $nosecret"
fi

badkind=$(api POST /api/webhooks \
  '{"name":"w","target_archetype":"fullstack_dev","goal_template":"g","secret":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","kind":"githib"}')
if has "$badkind" 'unknown webhook kind'; then
  ok "a misspelled sender kind is refused"
else
  bad "an unknown kind was accepted: $badkind"
fi

SECRET="whsec_verify_0123456789abcdef"
wh=$(api POST /api/webhooks \
  "{\"name\":\"verify stripe\",\"target_archetype\":\"fullstack_dev\",\"goal_template\":\"Handle {{event}} for {{amount}} {{currency}}\",\"kind\":\"stripe\",\"secret\":\"$SECRET\"}")
whtok=$(printf '%s' "$wh" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
if [ -n "$whtok" ]; then
  CLEANUP+=("curl -sS -X DELETE '$BASE/api/webhooks/$whtok' -H 'Authorization: Bearer $TOKEN'")
  BODY='{"type":"invoice.payment_failed","data":{"object":{"id":"in_9","amount_due":1999,"currency":"usd"}}}'
  TS=$(date +%s)
  goodsig=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | sed 's/.*= *//')
  bodyonly=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | sed 's/.*= *//')

  # Stripe signs "<t>.<body>". A correct signature must get PAST the signature
  # check — it may then fail on "no eligible instance", which is a different and
  # acceptable error on a fleet with no bots running.
  res=$(curl -sS -X POST "$BASE/api/webhooks/$whtok" -H 'Content-Type: application/json' \
    -H "Stripe-Signature: t=$TS,v1=$goodsig" -d "$BODY")
  if ! has "$res" 'signature missing or invalid'; then
    ok "a correctly signed Stripe delivery passes verification"
  else
    bad "a correct Stripe signature was rejected: $res"
  fi

  # The old generic scheme signed the body alone. It must not be accepted, or
  # the timestamp is decorative and replay protection is gone.
  res=$(curl -sS -X POST "$BASE/api/webhooks/$whtok" -H 'Content-Type: application/json' \
    -H "Stripe-Signature: t=$TS,v1=$bodyonly" -d "$BODY")
  if has "$res" 'signature missing or invalid'; then
    ok "a body-only HMAC is rejected for a Stripe webhook"
  else
    bad "the old signing scheme was accepted as Stripe: $res"
  fi

  # An hour old, correctly signed: outside the replay window.
  OLD=$((TS - 3600))
  oldsig=$(printf '%s.%s' "$OLD" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | sed 's/.*= *//')
  res=$(curl -sS -X POST "$BASE/api/webhooks/$whtok" -H 'Content-Type: application/json' \
    -H "Stripe-Signature: t=$OLD,v1=$oldsig" -d "$BODY")
  if has "$res" 'signature missing or invalid'; then
    ok "a replayed delivery outside the tolerance window is rejected"
  else
    bad "a stale delivery was accepted: $res"
  fi
else
  bad "could not create a Stripe webhook: $wh"
fi

# --------------------------------------------------------------------- memory ---

head "5. Memory (was: per-bot only, no shared pool)"

mem=$(api GET /api/memory/fleet)
if has "$mem" '"search"' && has "$mem" '"shared"'; then
  ok "the shared pool and the search mode are reported"
else
  bad "fleet memory endpoint is missing: $mem"
fi
if has "$mem" '"semantic":true'; then
  note "search: real embeddings"
elif has "$mem" 'hashed-bag-of-words'; then
  note "search: hashed keyword index (no embedding provider configured — expected)"
else
  bad "search mode is not reported honestly: $mem"
fi

# --------------------------------------------------------------- cost / voice ---

head "6. Cost accounting and voice"

fin=$(api GET /api/telemetry/financials)
if has "$fin" 'total_cached_tokens'; then
  ok "the cached-token column is served"
else
  bad "financial summary is missing cached tokens: $fin"
fi

voices=$(api GET /api/voice/voices)
if has "$voices" '"available":true'; then
  ok "the text-to-speech sidecar is reachable"
  has "$voices" '"default":"shadow"' && ok "the documented default voice is shadow" \
    || bad "the default voice is not shadow: $voices"
elif has "$voices" '"available":false'; then
  note "no TTS sidecar deployed — reported honestly, which is the contract"
  ok "voice availability is reported rather than assumed"
else
  bad "voice endpoint did not answer usefully: $voices"
fi

# ----------------------------------------------------------------- archetypes ---

head "7. Archetype packages (was: import created nothing)"

exp=$(api GET /api/archetypes/fullstack_dev/export)
if has "$exp" '"recorded_skills"' && has "$exp" '"mcp_servers"' && has "$exp" 'fullstack_dev'; then
  ok "an archetype exports with skill and MCP sections"
else
  bad "export is missing its package sections: $(printf '%s' "$exp" | head -c 200)"
fi

imp=$(api POST /api/archetypes/import \
  '{"manifest":{"version":"1.0.0","id":"verify_arch","name":"Verify Archetype","tagline":"t",
    "category":"Test","recommended_tier":"standard","vcpu":2,"memory_mb":4096,"disk_gb":20,
    "gpu":false,"preinstalled_tools":["curl"],"system_prompt":"p","default_environment":{},
    "mcp_servers":[],"recorded_skills":[]}}')
if has "$imp" '"skills_created"' && has "$imp" 'verify_arch'; then
  ok "import returns an itemised result instead of a bare success"
else
  bad "import did not report what it did: $imp"
fi

needs=$(api POST /api/archetypes/import \
  '{"manifest":{"version":"1.0.0","id":"a","name":"A","tagline":"","category":"c",
    "recommended_tier":"standard","vcpu":1,"memory_mb":1024,"disk_gb":10,"gpu":false,
    "preinstalled_tools":[],"system_prompt":"","default_environment":{},
    "mcp_servers":[{"name":"gh","transport":"stdio","command":"npx","env_keys":["TOKEN"]}],
    "recorded_skills":[]}}')
if has "$needs" 'needs_secrets'; then
  ok "a package needing credentials names them instead of failing on auth later"
else
  bad "credential requirements were not reported: $needs"
fi

# ---------------------------------------------------------------------- tiers ---

head "8. developer-heavy image"

if [ -f sandbox/Dockerfile.dev ]; then
  ok "sandbox/Dockerfile.dev exists"
else
  bad "no Dockerfile for the developer-heavy tier"
fi
if grep -q '^sandbox-dev:' Makefile 2>/dev/null; then
  ok "make sandbox-dev builds it"
else
  bad "no make target builds the -dev image"
fi
if docker image inspect agentfleet/sandbox:latest-dev >/dev/null 2>&1; then
  ok "agentfleet/sandbox:latest-dev is built locally"
else
  note "agentfleet/sandbox:latest-dev not built yet — run: make sandbox-dev"
fi

head "9. qemu driver"

if [ -f sandbox/Dockerfile.vm ]; then
  ok "sandbox/Dockerfile.vm exists"
else
  bad "no Dockerfile for the VM runner"
fi
if grep -q '^sandbox-vm:' Makefile 2>/dev/null; then
  ok "make sandbox-vm builds it"
else
  bad "no make target builds the VM image"
fi
# Egress on the qemu driver is ENFORCED, in the runner's netns: the guest's
# only way out is QEMU's SLIRP sockets there, and vm-entrypoint programs the
# same egress.sh the container tier uses before QEMU starts. The create that
# used to be refused must now be accepted, and the runner container must carry
# the nftables table — checked directly, because "accepted" without rules is
# exactly the silent-unenforcement this check exists to catch.
policied=$(api POST /api/instances '{"name":"vm-egress-probe","tier":"micro","driver":"qemu","egress":{"block_local":true}}')
vmid=$(printf '%s' "$policied" | sed -n 's/.*"id":"\([a-f0-9-]*\)".*/\1/p')
if [ -n "$vmid" ]; then
  ok "a policied qemu instance is accepted (egress enforced in the runner)"
  CLEANUP+=("curl -sS -X DELETE '$BASE/api/instances/$vmid' -H 'Authorization: Bearer $TOKEN'")
  # The create is acknowledged before the runner exists (provisioning is
  # asynchronous), and the runner container appears once the image is checked
  # and the container created -- up to a minute on a loaded host. The rules are
  # programmed before QEMU starts, so no need to wait out the guest boot.
  vmctr="af-$(printf '%s' "$vmid" | cut -c1-12)"
  rules=""
  for _ in $(seq 1 45); do
    rules=$(docker exec "$vmctr" nft list table inet agentfleet 2>/dev/null || true)
    [ -n "$rules" ] && break
    sleep 2
  done
  if has "$rules" 'ct state established,related accept' && has "$rules" 'drop'; then
    ok "the runner netns carries the nftables policy (stateful accept + drops present)"
  else
    bad "no nftables policy visible in the qemu runner $vmctr"
  fi
else
  bad "a policied qemu instance was refused or failed: $(printf '%s' "$policied" | head -c 160)"
fi
if docker image inspect agentfleet/sandbox:latest-vm >/dev/null 2>&1; then
  ok "agentfleet/sandbox:latest-vm is built locally"
else
  note "agentfleet/sandbox:latest-vm not built yet — run: make sandbox-vm"
fi

# ----------------------------------------------------------------- fleet chat ---

head "10. fleet chat commands"
cat_out=$(api GET /api/fleet/commands)
if has "$cat_out" '"name":"help"' && has "$cat_out" '"name":"mission"' && has "$cat_out" '"name":"task"'; then
  ok "the command catalogue lists help, task and mission"
else
  bad "command catalogue incomplete: $(printf '%s' "$cat_out" | head -c 160)"
fi
help_out=$(api POST /api/fleet/command '{"text":"/help"}')
if has "$help_out" '"ok":true' && has "$help_out" '/mission'; then
  ok "/help executes and documents /mission"
else
  bad "/help did not execute: $(printf '%s' "$help_out" | head -c 160)"
fi
bogus=$(api POST /api/fleet/command '{"text":"/frobnicate"}')
if has "$bogus" '"ok":false' && has "$bogus" '/help'; then
  ok "an unknown command fails honestly and points at /help"
else
  bad "unknown command handling: $(printf '%s' "$bogus" | head -c 160)"
fi
bots_out=$(api POST /api/fleet/command '{"text":"/bots"}')
if has "$bots_out" '"ok":true'; then
  ok "/bots answers"
else
  bad "/bots failed: $(printf '%s' "$bots_out" | head -c 160)"
fi

# ------------------------------------------------------ 11. first-run setup ---
# The setup card in both clients is only as honest as this endpoint: it must say
# what is missing in order, and the chat verb behind its button must exist.

setup_out=$(api GET /api/setup)
if has "$setup_out" '"next"' && has "$setup_out" '"steps"' && has "$setup_out" '"model"'; then
  ok "/api/setup reports the first-run plan"
else
  bad "/api/setup: $(printf '%s' "$setup_out" | head -c 160)"
fi
if has "$cat_out" '"setup"'; then
  ok "the command catalogue offers /setup"
else
  bad "/setup missing from the catalogue"
fi
detect_out=$(api POST /api/setup/autodetect '{"apply":false}')
if has "$detect_out" '"found"'; then
  ok "autodetect answers (dry run)"
else
  bad "autodetect: $(printf '%s' "$detect_out" | head -c 160)"
fi

# ---------------------------------------------------------- 12. Oaf sessions ---
# The chat as an agent: a session can be made, bound only to folders a device
# exposes, and deleted; the chat verbs behind the session card exist.

sess_out=$(api POST /api/oaf/sessions '{"name":"verify session"}')
sess_id=$(printf '%s' "$sess_out" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
if [ -n "$sess_id" ]; then
  ok "a session can be created"
else
  bad "session create: $(printf '%s' "$sess_out" | head -c 160)"
fi
bind_out=$(api PATCH "/api/oaf/sessions/$sess_id" '{"cwd":"/definitely/not/exposed"}')
if has "$bind_out" 'needs a device' || has "$bind_out" 'outside'; then
  ok "a folder with no device behind it is refused"
else
  bad "folder binding was not checked: $(printf '%s' "$bind_out" | head -c 160)"
fi
if has "$cat_out" '"goal"' && has "$cat_out" '"loop"' && has "$cat_out" '"devices"'; then
  ok "the catalogue offers /goal, /loop and /devices"
else
  bad "session commands missing from the catalogue"
fi
dev_out=$(api GET /api/oaf/devices)
if has "$dev_out" '[' ; then
  ok "/api/oaf/devices answers"
else
  bad "devices: $(printf '%s' "$dev_out" | head -c 160)"
fi
del_code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE "$BASE/api/oaf/sessions/$sess_id" -H "Authorization: Bearer $TOKEN")
if [ "$del_code" = "204" ]; then
  ok "a session can be deleted"
else
  bad "session delete returned $del_code"
fi

# -------------------------------------------------------------------- summary ---

echo
if [ "$fail" -eq 0 ]; then
  printf '\033[32m%d passed, %d failed\033[0m\n' "$pass" "$fail"
else
  printf '\033[31m%d passed, %d failed\033[0m\n' "$pass" "$fail"
fi
[ "$fail" -eq 0 ]
