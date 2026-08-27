#!/usr/bin/with-contenv bash
# Auto-install the PlatformIO IDE extension for the abc user on first boot.
# Extensions install into /config (the persistent PVC), so this effectively
# runs only once and survives pod restarts afterward.
# Fails gracefully so a marketplace/network hiccup never blocks container start.

set -e

EXTENSION_ID="platformio.platformio-ide"
EXTENSIONS_DIR="/config/.local/share/code-server/extensions"

# Already installed? Extension folders are named <publisher>.<name>-<version>.
if ls "${EXTENSIONS_DIR}/${EXTENSION_ID}-"* >/dev/null 2>&1; then
  echo "[custom-init] ${EXTENSION_ID} already installed, skipping."
  exit 0
fi

if ! command -v code-server >/dev/null 2>&1; then
  echo "[custom-init] WARNING: code-server CLI not found; install ${EXTENSION_ID} manually from the Extensions panel."
  exit 0
fi

echo "[custom-init] Installing ${EXTENSION_ID} for user abc ..."
if sudo -u abc -H code-server --install-extension "${EXTENSION_ID}" --force; then
  echo "[custom-init] ${EXTENSION_ID} installed successfully."
else
  echo "[custom-init] WARNING: failed to install ${EXTENSION_ID} (marketplace/network issue)."
  echo "[custom-init] Install it manually: Extensions panel -> search 'platformio.platformio-ide'"
fi
