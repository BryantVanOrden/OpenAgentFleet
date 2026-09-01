#!/usr/bin/env bash
# Boots the sandbox VM. This container's only job is to run QEMU.
set -euo pipefail

log() { echo "[vm] $*"; }

VM_VCPUS="${VM_VCPUS:-2}"
VM_MEMORY_MB="${VM_MEMORY_MB:-3072}"

# Acceleration is detected, not configured. /dev/kvm is granted by the
# orchestrator when the host has it; TCG software emulation is the fallback
# that keeps the driver usable on hosts without nested virtualisation (Docker
# Desktop, most CI). The difference is speed, not behaviour — the same guest
# boots either way, slowly.
ACCEL_ARGS=(-machine q35 -cpu max)
if [[ -e /dev/kvm && -w /dev/kvm ]]; then
    ACCEL_ARGS=(-machine q35,accel=kvm -cpu host -enable-kvm)
    log "kvm acceleration enabled"
else
    log "no /dev/kvm; falling back to TCG software emulation (slow boot, same guest)"
fi

# The base disk stays pristine; each boot runs on a qcow2 overlay. A restarted
# runner gets a fresh guest, which is the same disposability contract the
# container tier has — a sandbox's persistence is its task artifacts, not its
# disk.
OVERLAY=/run/guest.qcow2
qemu-img create -q -f qcow2 -b /vm/rootfs.img -F raw "$OVERLAY"

# The environment the guest's entrypoint expects, carried on the kernel command
# line as base64. Only the variables the sandbox actually reads are forwarded —
# the runner's own PATH/HOSTNAME noise would blow the cmdline length budget for
# nothing.
ENV_FILE=$(mktemp)
for var in ALLOW_SHELL SUDO_ACCESS AGENTFLEET_INSTANCE SCREEN_RESOLUTION VNC_PORT VNC_VIEW_PORT \
           AGENTD_PORT DISPLAY GTK_MODULES QT_ACCESSIBILITY QT_LINUX_ACCESSIBILITY_ALWAYS_ON \
           GNOME_ACCESSIBILITY NO_AT_BRIDGE ARCHETYPE_ID PREINSTALL_TOOLS AGENTFLEET_HOSTNAME; do
    if [[ -n "${!var:-}" ]]; then
        printf '%s=%s\n' "$var" "${!var}" >> "$ENV_FILE"
    fi
done
ENV_B64=$(base64 -w0 < "$ENV_FILE")
rm -f "$ENV_FILE"

# SLIRP user networking with the sandbox's three ports forwarded, bound on
# every container interface so the orchestrator reaches the guest at this
# container's address exactly as it reaches a container-tier sandbox. The
# guest's addresses are SLIRP's fixed defaults; vm-init configures them
# statically.
NET_ARGS=(-netdev "user,id=n0,hostfwd=tcp:0.0.0.0:7900-:7900,hostfwd=tcp:0.0.0.0:6901-:6901,hostfwd=tcp:0.0.0.0:6902-:6902"
          -device virtio-net-pci,netdev=n0)

log "booting guest: ${VM_VCPUS} vcpu, ${VM_MEMORY_MB} MB"
exec qemu-system-x86_64 \
    "${ACCEL_ARGS[@]}" \
    -smp "$VM_VCPUS" -m "$VM_MEMORY_MB" \
    -kernel /vm/vmlinuz -initrd /vm/initrd \
    -append "console=ttyS0 root=/dev/vda rw net.ifnames=0 init=/sbin/agentfleet-init agentfleet.env=${ENV_B64}" \
    -drive "file=${OVERLAY},if=virtio,cache=unsafe" \
    "${NET_ARGS[@]}" \
    -display none -serial mon:stdio -no-reboot
