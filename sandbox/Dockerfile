# AgentFleet sandbox: a disposable Linux desktop an AI can see and drive.
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
        dbus-x11 at-spi2-core \
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
COPY egress.sh /usr/local/bin/egress.sh
COPY init-archetype.sh /usr/local/bin/init-archetype.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/egress.sh /usr/local/bin/init-archetype.sh

# The agent runs unprivileged with passwordless sudo so it can install packages on the fly
RUN useradd -m -s /bin/bash -u 1000 agent \
    && echo "agent ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/agent \
    && chmod 0440 /etc/sudoers.d/agent \
    && mkdir -p /home/agent/work /var/log/agentfleet /var/run/agentfleet \
    && chown -R agent:agent /home/agent /var/log/agentfleet /var/run/agentfleet

EXPOSE 6901 7900

HEALTHCHECK --interval=10s --timeout=5s --start-period=45s --retries=6 \
    CMD curl -fsS http://localhost:${AGENTD_PORT}/health || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf", "-n"]
