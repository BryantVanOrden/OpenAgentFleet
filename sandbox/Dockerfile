# OpenAgentFleet sandbox: a disposable Linux desktop an AI can see and drive.
#
# Layers, deliberately ordered cheapest-to-change last:
#   1. X stack + XFCE + VNC + noVNC          (rarely changes)
#   2. automation tools + accessibility bus  (rarely changes)
#   3. python deps for agentd               (changes with requirements.txt)
#   4. agentd source                        (changes constantly)
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    DISPLAY=:1 \
    SCREEN_RESOLUTION=1920x1080x24 \
    VNC_PORT=6901 \
    VNC_VIEW_PORT=6902 \
    AGENTD_PORT=7900 \
    HOME=/home/agent \
    # Defaults so supervisord can expand these even when the image is run
    # standalone; the orchestrator overrides both per instance.
    ALLOW_SHELL=false \
    AGENTFLEET_INSTANCE=standalone \
    # Accessibility has to be on for the semantic recorder to see anything.
    GTK_MODULES=gail:atk-bridge \
    QT_ACCESSIBILITY=1 \
    QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1 \
    GNOME_ACCESSIBILITY=1 \
    NO_AT_BRIDGE=0

# --- 1. desktop + remote view -------------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb x11vnc xauth \
        xfce4 xfce4-terminal xfdesktop4 \
        dbus dbus-x11 at-spi2-core \
        fonts-dejavu-core fonts-liberation \
        novnc websockify \
        supervisor ca-certificates curl wget gnupg \
    && apt-get purge -y xfce4-power-manager xfce4-screensaver light-locker 2>/dev/null || true \
    && rm -rf /var/lib/apt/lists/*

# --- 2. automation + policy tooling ------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        xdotool wmctrl xclip scrot imagemagick \
        git ripgrep jq build-essential pkg-config \
        unzip tar nano htop tmux ffmpeg nmap \
        net-tools dnsutils iputils-ping tree psmisc \
        sudo strace lsof openssh-client \
        python3 python3-pip python3-venv \
        python3-gi python3-pyatspi gir1.2-atspi-2.0 \
        nftables iproute2 \
    && rm -rf /var/lib/apt/lists/*

# Firefox from Mozilla's own APT repository. Ubuntu 24.04 ships `firefox` as a
# snap transition package, which cannot install in a container — pulling the
# real .deb from Mozilla is the only way to get a browser in here. The pinning
# is required, otherwise apt still prefers the snap stub.
RUN install -d -m 0755 /etc/apt/keyrings \
    && wget -qO- https://packages.mozilla.org/apt/repo-signing-key.gpg \
        > /etc/apt/keyrings/packages.mozilla.org.asc \
    && echo "deb [signed-by=/etc/apt/keyrings/packages.mozilla.org.asc] https://packages.mozilla.org/apt mozilla main" \
        > /etc/apt/sources.list.d/mozilla.list \
    && printf 'Package: *\nPin: origin packages.mozilla.org\nPin-Priority: 1000\n' \
        > /etc/apt/preferences.d/mozilla \
    && apt-get update \
    && apt-get install -y --no-install-recommends firefox \
    && rm -rf /var/lib/apt/lists/*
# Turn off Firefox's first-run onboarding.
#
# A fresh profile opens on the "Welcome to Firefox" modal, and an agent asked to
# test a web page finds a dialog over it. It clicks Continue, the overlay
# advances to another onboarding panel that looks almost identical, and the
# screen-change check reads that as clicking into the void: two ToolCheck runs
# died on `click [1] "Continue"` without ever reaching the page they were sent
# to test. Enterprise policy is the profile-independent way to say no -- a
# profile-level pref would come back with every new profile.
COPY firefox-policies.json /etc/firefox/policies/policies.json

# --- 3. agentd dependencies ---------------------------------------------------
# A venv with --system-site-packages, not `pip --break-system-packages`.
#
# pip cannot uninstall dpkg-managed distributions (no RECORD file), so installing
# straight into the system interpreter fails the moment a wheel wants a newer
# typing_extensions than Ubuntu ships. The venv sidesteps dpkg entirely, while
# --system-site-packages keeps pyatspi and python3-gi importable — those only
# exist as apt packages with compiled bindings and cannot come from PyPI.
COPY agentd/requirements.txt /opt/agentd/requirements.txt
RUN python3 -m venv --system-site-packages /opt/agentd/venv \
    && /opt/agentd/venv/bin/pip install --no-cache-dir --upgrade pip \
    && /opt/agentd/venv/bin/pip install --no-cache-dir -r /opt/agentd/requirements.txt \
    && /opt/agentd/venv/bin/python -c "import fastapi, uvicorn, mss, PIL, Xlib; print('agentd deps OK')"

# --- 4. agentd + service wiring ----------------------------------------------
COPY agentd/ /opt/agentd/
COPY supervisord.conf /etc/supervisor/conf.d/agentfleet.conf
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY wait-for-session.sh /usr/local/bin/wait-for-session.sh
COPY egress.sh /usr/local/bin/egress.sh
COPY init-archetype.sh /usr/local/bin/init-archetype.sh
# Install recipes, editable without touching the script that reads them.
RUN mkdir -p /etc/agentfleet
COPY tools.conf /etc/agentfleet/tools.conf
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/egress.sh /usr/local/bin/init-archetype.sh

# The agent runs unprivileged, with a restricted passwordless sudo so it can
# still install packages on the fly.
# Ubuntu 24.04 ships a default `ubuntu` user already holding UID 1000, so a
# plain useradd -u 1000 fails with "UID already in use" (exit 4). Reclaim it:
# pinning the agent to 1000 keeps bind-mounted files owned sanely on Linux hosts.
#
# The sudo grant is deliberately not `NOPASSWD:ALL` any more. The dev archetypes
# genuinely need on-the-fly `apt-get install`, so sudo stays usable — but the
# commands that dismantle the platform's own controls are denied. The one that
# matters is `nft`: an instance carrying an egress policy also carries
# CAP_NET_ADMIN, so a single `sudo nft flush ruleset` used to delete the whole
# allow-list, the RFC1918 block and all, from inside the container the policy
# exists to contain.
#
# Be clear about what this is worth. A sudoers deny-list is a guardrail, not a
# boundary: sudo matches on the resolved binary path, so `sudo cp /usr/sbin/nft
# /tmp/n && sudo /tmp/n flush ruleset` — or simply `sudo bash` — still gets
# there. It stops the direct, obvious command an agent (or a page instructing
# one) would actually reach for. The real control is the setuid bit on
# /usr/bin/sudo, which the orchestrator clears at provision unless the operator
# granted sudo and can clear again at any time (Manager.SetSudo). With the bit
# cleared sudo cannot escalate, and an unprivileged agent cannot restore it.
# This deny-list is what is left when someone runs the image directly with
# `docker run`, which is how a developer will first test it.
RUN (userdel -r ubuntu 2>/dev/null || true) \
    && useradd -m -s /bin/bash -u 1000 agent \
    && printf '%s\n' \
        '# Platform state, not the agent to edit: egress policy, the mount' \
        '# table, kernel modules, sysctls, namespaces.' \
        'Cmnd_Alias AGENTFLEET_DENIED = /usr/sbin/nft, /usr/sbin/iptables, /usr/sbin/ip6tables, /usr/sbin/iptables-legacy, /usr/sbin/iptables-nft, /usr/sbin/ip6tables-nft, /usr/sbin/xtables-nft-multi, /usr/sbin/ip, /usr/bin/ip, /usr/sbin/tc, /usr/bin/mount, /usr/bin/umount, /usr/sbin/mount, /usr/sbin/umount, /usr/sbin/insmod, /usr/sbin/rmmod, /usr/sbin/modprobe, /usr/sbin/depmod, /usr/sbin/sysctl, /usr/bin/sysctl, /usr/bin/unshare, /usr/bin/nsenter' \
        'agent ALL=(ALL) NOPASSWD: ALL, !AGENTFLEET_DENIED' \
        > /etc/sudoers.d/agent \
    && chmod 0440 /etc/sudoers.d/agent \
    && visudo -cf /etc/sudoers.d/agent \
    && mkdir -p /home/agent/work /var/log/agentfleet /var/run/agentfleet \
    && chown -R agent:agent /home/agent /var/log/agentfleet /var/run/agentfleet

EXPOSE 6901 6902 7900

HEALTHCHECK --interval=10s --timeout=5s --start-period=45s --retries=6 \
    CMD curl -fsS http://localhost:${AGENTD_PORT}/health || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf", "-n"]
