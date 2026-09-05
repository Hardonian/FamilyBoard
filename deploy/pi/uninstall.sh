#!/bin/bash
# FamilyBoard Appliance Uninstallation Script
# Preserves user data and configuration by default unless --purge is specified

set -Eeuo pipefail

PURGE=false
if [ "${1:-}" = "--purge" ]; then
  PURGE=true
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: Must be run as root (use sudo)." >&2
  exit 1
fi

echo "=== Uninstalling FamilyBoard Appliance ==="

# 1. Stop and disable systemd service
if systemctl is-active --quiet familyboard.service 2>/dev/null; then
  echo "Stopping familyboard.service..."
  systemctl stop familyboard.service || true
fi
if systemctl is-enabled --quiet familyboard.service 2>/dev/null; then
  echo "Disabling familyboard.service..."
  systemctl disable familyboard.service || true
fi
rm -f /etc/systemd/system/familyboard.service
systemctl daemon-reload

# 2. Remove managed block from kiosk autostart
MANAGED_START="# BEGIN FAMILYBOARD MANAGED KIOSK"
MANAGED_END="# END FAMILYBOARD MANAGED KIOSK"

clean_autostart() {
  local target="$1"
  if [ -f "$target" ] && grep -q "$MANAGED_START" "$target"; then
    echo "Removing FamilyBoard autostart from $target..."
    sed -i "/$MANAGED_START/,/$MANAGED_END/d" "$target"
  fi
}

clean_autostart "/home/familyboard-kiosk/.config/labwc/autostart"
clean_autostart "/home/familyboard-kiosk/.config/lxsession/LXDE-pi/autostart"

# 3. Remove application binaries
rm -f /usr/local/bin/familyboard
rm -f /usr/local/bin/familyboardctl
rm -f /usr/local/bin/familyboard-kiosk.sh

# 4. Handle data directories
if [ "$PURGE" = true ]; then
  echo "Purging all FamilyBoard databases, credentials, and configurations..."
  rm -rf /etc/familyboard
  rm -rf /var/lib/familyboard
  rm -rf /var/cache/familyboard
  rm -rf /run/familyboard
  userdel familyboard || true
  userdel familyboard-kiosk || true
  echo "All FamilyBoard data and users have been purged."
else
  echo ""
  echo "User data and database preserved at:"
  echo "  - /etc/familyboard/ (configuration & credentials)"
  echo "  - /var/lib/familyboard/ (SQLite database & backups)"
  echo ""
  echo "To completely erase data, run: sudo ./deploy/pi/uninstall.sh --purge"
fi

echo "=== FamilyBoard Uninstallation Complete ==="
