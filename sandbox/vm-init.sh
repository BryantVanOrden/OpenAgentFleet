#!/bin/bash
# PID 1 inside the QEMU guest.
#
# The guest's rootfs IS the sandbox container image, converted to a disk at
# build time — same agentd, same desktop, same supervisord config — so the VM
# cannot drift from the container the way a separately-maintained guest image
# would. What a container gets from its runtime, this script provides by hand:
# kernel filesystems, a network identity, and the environment the entrypoint
# expects, carried in on the kernel command line because a VM has no docker
# env to inherit.
set -u

log() { echo "[vm-init] $*"; }

# Kernel filesystems first; nothing below works without /proc.
mount -t proc     proc     /proc                          || true
mount -t sysfs    sysfs    /sys                           || true
mountpoint -q /dev || mount -t devtmpfs devtmpfs /dev     || true
mkdir -p /dev/pts /dev/shm
mount -t devpts   devpts   /dev/pts                       || true
mount -t tmpfs    tmpfs    /dev/shm                       || true
# Matches the container: /tmp is tmpfs with exec, sized to stay inside the
# guest's own memory limit.
mount -t tmpfs -o rw,exec,size=1g tmpfs /tmp              || true
# /run is tmpfs whether we like it or not — Debian's initrd mounts it before
# this script runs — so the image's agent-owned /run/agentfleet never survives
# into the guest. The session bus (running as agent) binds its socket there,
# and the entrypoint's `install -d .../keyring` only chowns the leaf, leaving
# the parent root-owned: dbus-session crash-looped on "Permission denied" and
# XFCE painted a black screen with "Unable to contact settings server".
# Recreate it with the right owner before anything needs it.
mkdir -p /run/lock
install -d -m 0755 -o agent -g agent /run/agentfleet 2>/dev/null \
    || mkdir -p /run/agentfleet

# QEMU user-mode (SLIRP) networking has fixed, documented addresses: the guest
# is 10.0.2.15, the gateway 10.0.2.2, the DNS proxy 10.0.2.3. Static
# configuration beats running a DHCP client for values that cannot change.
# net.ifnames=0 on the cmdline pins the NIC's name to eth0.
ip link set lo up
ip link set eth0 up                                       || true
ip addr add 10.0.2.15/24 dev eth0                         || true
ip route add default via 10.0.2.2                         || true
echo "nameserver 10.0.2.3" > /etc/resolv.conf

# The environment the sandbox entrypoint reads (ALLOW_SHELL, the instance id,
# the screen resolution...) arrives base64-encoded on the kernel command line,
# because that is the only channel the host controls that the guest can read
# before userspace exists.
for word in $(cat /proc/cmdline); do
    case "$word" in
        agentfleet.env=*)
            echo "${word#agentfleet.env=}" | base64 -d > /run/agentfleet.env || true
            ;;
    esac
done
# Image ENV defaults first (captured at build; see Dockerfile.vm), then the
# per-instance env from the cmdline overrides — the same precedence docker run
# gives -e over the image's own ENV.
if [[ -f /etc/agentfleet-image.env ]]; then
    set -a
    # shellcheck disable=SC1091
    source /etc/agentfleet-image.env
    set +a
fi
if [[ -f /run/agentfleet.env ]]; then
    set -a
    # shellcheck disable=SC1091
    source /run/agentfleet.env
    set +a
fi
export DISPLAY="${DISPLAY:-:1}"

hostname "${AGENTFLEET_HOSTNAME:-agentfleet-vm}" 2>/dev/null || true

# The sudo decision, applied where sudo actually lives. The container tier
# does this with a docker exec after boot; a VM has no exec channel, so the
# guest applies its own grant here — and clearing the setuid bit before any
# agent-reachable process starts is a strictly better ordering than the
# container tier's post-boot exec.
if [[ "${SUDO_ACCESS:-false}" == "true" ]]; then
    chmod u+s /usr/bin/sudo 2>/dev/null || true
else
    chmod u-s /usr/bin/sudo 2>/dev/null || true
fi

log "guest up: $(hostname), display ${DISPLAY}, shell=${ALLOW_SHELL:-unset}"

# Hand off to the same entrypoint the container runs: egress policy (skipped
# when no policy env is set, which is the VM default — see Dockerfile.vm for
# why), keyring dir, archetype init, then supervisord in the foreground.
#
# exec, so supervisord inherits PID 1's signal handling; it supervises its own
# children, which is everything this guest runs.
exec /usr/local/bin/entrypoint.sh /usr/bin/supervisord -c /etc/supervisor/supervisord.conf -n
