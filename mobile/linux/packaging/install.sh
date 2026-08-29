#!/usr/bin/env bash
# Install a built Linux bundle into the current user's desktop environment.
# Flutter does not produce a desktop entry, so the app otherwise shows up with
# no icon and no launcher entry.
#
#   flutter build linux --release
#   linux/packaging/install.sh
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
bundle=${1:-$here/../../build/linux/x64/release/bundle}
if [[ ! -x "$bundle/agentfleet_companion" ]]; then
  echo "no bundle at $bundle — run 'flutter build linux --release' first" >&2
  exit 1
fi
bundle=$(cd "$bundle" && pwd)

apps=${XDG_DATA_HOME:-$HOME/.local/share}/applications
icons=${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/512x512/apps
mkdir -p "$apps" "$icons"

install -m644 "$here/agentfleet.png" "$icons/agentfleet.png"
sed "s|^Exec=.*|Exec=$bundle/agentfleet_companion|" "$here/agentfleet.desktop" \
  > "$apps/agentfleet.desktop"
chmod 644 "$apps/agentfleet.desktop"

command -v update-desktop-database >/dev/null && update-desktop-database "$apps" || true
command -v gtk-update-icon-cache >/dev/null && \
  gtk-update-icon-cache -f -t "${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor" 2>/dev/null || true

echo "installed AgentFleet -> $apps/agentfleet.desktop"
