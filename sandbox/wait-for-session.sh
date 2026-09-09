#!/usr/bin/env bash
# Block until the display accepts connections and the session bus answers.
#
# supervisord's priority only orders spawn calls; it is not readiness. A socket
# file also is not readiness: the bus creates it before it is listening, and
# xfce4-session connects exactly once, at startup, then parks on "Unable to
# load a failsafe session" if that connection failed. So ask the bus a real
# question (Ping) and ask X for a real answer (xdotool) before starting
# anything that needs either. Twenty seconds is the ceiling; a bus that is not
# up by then is a bug supervisord's own retries will surface.
set -u
export DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-unix:path=/var/run/agentfleet/session_bus}"
for i in $(seq 1 200); do
    if dbus-send --session --dest=org.freedesktop.DBus --type=method_call --print-reply         /org/freedesktop/DBus org.freedesktop.DBus.Peer.Ping >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
# xdotool is already in the image (the agent drives the desktop with it), so
# it is also the readiness probe: it answers only once X accepts connections.
for i in $(seq 1 200); do
    if DISPLAY="${DISPLAY:-:1}" xdotool getdisplaygeometry >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
