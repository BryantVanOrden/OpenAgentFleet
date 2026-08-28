#!/usr/bin/env bash
# Sandbox entrypoint: apply network policy, then hand off to supervisord.
set -euo pipefail

log() { echo "[entrypoint] $*"; }

# Network policy runs first and while we still have NET_ADMIN, before anything
# the agent controls is listening.
if [[ -n "${EGRESS_ALLOW:-}${EGRESS_DENY:-}${EGRESS_BLOCK_LOCAL:-}" ]]; then
    if /usr/local/bin/egress.sh; then
        log "egress policy applied"
    else
        # Fail closed on the policy, not on the container: an instance that was
        # asked to be restricted must not come up unrestricted.
        log "FATAL: egress policy could not be applied"
        exit 1
    fi
fi

# Keyring directory for agentd's /keyring endpoint. Nothing baked into the image
# and, today, nothing put here either: the orchestrator has a client for that
# endpoint but no code path calls it, so this directory stays empty unless
# someone POSTs to /keyring by hand.
#
# It is an ordinary directory on the container's writable layer, NOT tmpfs, and
# it cannot be made one from here: mounting tmpfs needs CAP_SYS_ADMIN, which the
# sandbox deliberately does not have. Making it tmpfs means adding it to the
# container's Tmpfs map in backend/internal/fleet/manager.go, alongside /tmp.
# Do that before anything starts writing secrets here.
install -d -m 0700 -o agent -g agent /var/run/agentfleet/keyring

# Initialize archetype workspace and clone preinstalled repositories
if [[ -x /usr/local/bin/init-archetype.sh ]]; then
    /usr/local/bin/init-archetype.sh || log "warning: archetype init had non-fatal error"
fi

# Resolution can be changed per instance without rebuilding.
log "display ${DISPLAY} at ${SCREEN_RESOLUTION}"

exec "$@"
