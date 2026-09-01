#!/usr/bin/env bash
# Programs the network namespace it runs in with nftables.
#
# Two homes, one policy. On the docker driver this is the sandbox container's
# own netns. On the qemu driver it is the VM RUNNER's netns — every connection
# the guest makes leaves through QEMU's SLIRP sockets there, dialled to the
# same destination address the guest asked for, so filtering the runner's
# output sees exactly the guest's egress. That placement is also what makes
# the policy tamper-proof from inside: an agent with root in the guest can
# flush the guest's own tables all it likes and never touch these rules.
#
# EGRESS_ALLOW  comma-separated hosts/CIDRs. Non-empty means allow-list only.
# EGRESS_DENY   comma-separated hosts/CIDRs to drop.
# EGRESS_BLOCK_LOCAL  "true" drops RFC1918 / link-local / loopback-adjacent
#                     traffic, so an agent cannot reach the orchestrator, the
#                     database, or anything else on the private network.
#
# Hostnames are resolved once, at policy time. That is a real limitation for
# CDN-backed hosts whose addresses rotate — for those, put a CIDR in the list or
# run an explicit egress proxy. It is called out in docs/SECURITY.md rather than
# quietly pretended away.
set -euo pipefail

ALLOW="${EGRESS_ALLOW:-}"
DENY="${EGRESS_DENY:-}"
BLOCK_LOCAL="${EGRESS_BLOCK_LOCAL:-false}"

resolve() {
    local host="$1"
    # Already an address or CIDR?
    if [[ "$host" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+(/[0-9]+)?$ ]]; then
        echo "$host"
        return
    fi
    getent ahostsv4 "$host" 2>/dev/null | awk '{print $1}' | sort -u
}

# Replace OUR table only — never `flush ruleset`. Docker programs its embedded
# DNS (127.0.0.11) with NAT rules inside this same netns, and flushing the
# whole ruleset silently destroyed them: every policied sandbox came up with
# "DNS stays open" true for the packets and false for the resolver, because
# getent asks 127.0.0.11 and nothing was left to forward it. Found by testing
# resolution inside a policied sandbox rather than reading the rules.
nft delete table inet agentfleet 2>/dev/null || true

nft add table inet agentfleet
nft add chain inet agentfleet output '{ type filter hook output priority 0; policy accept; }'

# DNS and loopback always stay open: without them nothing resolves and the local
# control plane (agentd, x11vnc) cannot talk to itself.
nft add rule inet agentfleet output oifname lo accept
nft add rule inet agentfleet output udp dport 53 accept
nft add rule inet agentfleet output tcp dport 53 accept

# Replies to connections someone else opened to us — the orchestrator's health
# polls, the console's VNC stream, agentd API calls. Without this, allow-list
# mode's final drop ate the reply packets of the very health check that decides
# whether the instance ever comes up: an allow-only policy meant a sandbox that
# provisioned, restricted itself, and was then declared dead for answering
# nobody. Outbound restrictions lose nothing — a connection the agent initiates
# is NEW at its first packet, which is where the daddr rules below judge it.
nft add rule inet agentfleet output ct state established,related accept

if [[ "$BLOCK_LOCAL" == "true" ]]; then
    # The container's own /16 is left reachable so the orchestrator can still
    # poll agentd; everything else private is dropped.
    OWN_NET="$(ip -o -4 addr show scope global | awk '{print $4}' | head -1)"
    if [[ -n "$OWN_NET" ]]; then
        nft add rule inet agentfleet output ip daddr "$OWN_NET" accept
    fi
    for cidr in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 169.254.0.0/16 100.64.0.0/10; do
        nft add rule inet agentfleet output ip daddr "$cidr" drop
    done
fi

IFS=',' read -ra deny_list <<< "$DENY"
for host in "${deny_list[@]}"; do
    host="$(echo "$host" | xargs)"
    [[ -z "$host" ]] && continue
    for addr in $(resolve "$host"); do
        nft add rule inet agentfleet output ip daddr "$addr" drop
    done
done

IFS=',' read -ra allow_list <<< "$ALLOW"
if [[ ${#allow_list[@]} -gt 0 && -n "${allow_list[0]// /}" ]]; then
    for host in "${allow_list[@]}"; do
        host="$(echo "$host" | xargs)"
        [[ -z "$host" ]] && continue
        for addr in $(resolve "$host"); do
            nft add rule inet agentfleet output ip daddr "$addr" accept
        done
    done
    # Anything not explicitly allowed above falls through to this drop.
    nft add rule inet agentfleet output ip daddr 0.0.0.0/0 drop
    nft add rule inet agentfleet output meta nfproto ipv6 drop
fi

echo "[egress] policy active"
nft list table inet agentfleet
