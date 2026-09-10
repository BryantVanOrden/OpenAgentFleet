#!/usr/bin/env bash
# Desktop watchdog: notice a wedged X server and restart the desktop stack.
#
# On 2026-09-09, an hour into a run, Xvfb stopped answering its clients -- a
# server grab nobody released, as far as anyone could tell. Every X request
# blocked: xdotool hung, agentd's screenshot hung, and with it every observe,
# so the orchestrator saw "could not observe the desktop" until the run was
# failed. Restarting agentd did nothing; restarting the window manager did
# nothing; restarting Xvfb and the programs on top of it fixed it in seconds.
# That is what this does, automatically, after three misses in a row.
#
# The probe is a request that needs the server to actually answer
# (getmouselocation), not a socket connect, which succeeds while the server
# is wedged because the listener is the kernel's.
set -u
export DISPLAY="${DISPLAY:-:1}"
INTERVAL="${WATCHDOG_INTERVAL:-30}"
PROBE_TIMEOUT="${WATCHDOG_PROBE_TIMEOUT:-8}"
MISSES_TO_ACT="${WATCHDOG_MISSES:-3}"
misses=0

log() { echo "[desktop-watchdog] $(date -u +%H:%M:%S) $*"; }

# Give the desktop time to come up before judging it.
sleep 45

while true; do
  if timeout "$PROBE_TIMEOUT" xdotool getmouselocation >/dev/null 2>&1; then
    if [ "$misses" -gt 0 ]; then log "display answering again"; fi
    misses=0
  else
    misses=$((misses + 1))
    log "display did not answer in ${PROBE_TIMEOUT}s (miss $misses/$MISSES_TO_ACT)"
    if [ "$misses" -ge "$MISSES_TO_ACT" ]; then
      log "restarting the desktop stack"
      supervisorctl restart xvfb >/dev/null 2>&1
      sleep 5
      supervisorctl restart xfce x11vnc x11vnc-view agentd >/dev/null 2>&1
      # The accessibility bus autostarts with the session; a second launcher
      # exits at once, so only start ours if none is running.
      pgrep -x at-spi-bus-launcher >/dev/null || supervisorctl start atspi >/dev/null 2>&1
      misses=0
      sleep 30
    fi
  fi
  sleep "$INTERVAL"
done
