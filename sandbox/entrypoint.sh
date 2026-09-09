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

# The system bus binds /run/dbus/system_bus_socket and does not create the
# directory. Without it dbus-system crash-looped into FATAL on every boot and
# xfdesktop, the panel clock and libnotify each logged "Failed to get system
# bus" -- harmless individually, but a desktop that is quietly half-broken.
install -d -m 0755 /run/dbus

# The desktop's own directories must belong to the desktop's user. A build step
# that ran as root with HOME under /home/agent (the -dev image's compiler smoke
# tests did) leaves them root-owned, xfconfd cannot create its config dir, and
# XFCE parks on its failsafe dialog. Cheap to assert here on every boot.
install -d -o agent -g agent /home/agent/.config /home/agent/.cache /home/agent/.local/share
chown agent:agent /home/agent /home/agent/.config /home/agent/.cache /home/agent/.local /home/agent/.local/share 2>/dev/null || true
find /home/agent/.config /home/agent/.cache -maxdepth 2 ! -user agent -exec chown agent:agent {} + 2>/dev/null || true

# Prepare the archetype workspace in the background.
#
# Not blocking, deliberately. This runs before supervisord, so anything slow
# here delays agentd and the orchestrator times out waiting for the sandbox to
# answer — which is what happened once tool installation became real: fetching
# kubectl, helm, terraform and friends takes minutes, and the machine never
# finished provisioning. The desktop comes up immediately and the toolchain
# lands behind it; the orchestrator waits for a marker before deciding which
# tools to tell the agent it has.
if [[ -x /usr/local/bin/init-archetype.sh ]]; then
    ( /usr/local/bin/init-archetype.sh || log "warning: archetype init had non-fatal error" ) &
fi

# Resolution can be changed per instance without rebuilding.
log "display ${DISPLAY} at ${SCREEN_RESOLUTION}"

exec "$@"
