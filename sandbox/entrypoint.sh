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

# Secrets are injected at run time through agentd, never baked into the image.
install -d -m 0700 -o agent -g agent /var/run/agentfleet/keyring

# Initialize archetype workspace and clone preinstalled repositories
if [[ -x /usr/local/bin/init-archetype.sh ]]; then
    /usr/local/bin/init-archetype.sh || log "warning: archetype init had non-fatal error"
fi

# Resolution can be changed per instance without rebuilding.
log "display ${DISPLAY} at ${SCREEN_RESOLUTION}"

exec "$@"
