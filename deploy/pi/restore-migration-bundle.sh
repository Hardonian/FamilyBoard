#!/bin/bash
# FamilyBoard Migration Bundle Restorer
# Unpacks and applies a migration bundle to a fresh Raspberry Pi OS installation

set -Eeuo pipefail

BUNDLE_PATH="${1:-}"

if [ -z "$BUNDLE_PATH" ] || [ ! -f "$BUNDLE_PATH" ]; then
  echo "Usage: sudo ./deploy/pi/restore-migration-bundle.sh <path-to-migration-bundle.tar.gz>" >&2
  exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: Must be run as root (use sudo)." >&2
  exit 1
fi

echo "=== Restoring FamilyBoard Migration Bundle ==="

TMP_STAGE=$(mktemp -d "/tmp/fb-restore-XXXXXX")
trap 'rm -rf "$TMP_STAGE"' EXIT

tar -xzf "$BUNDLE_PATH" -C "$TMP_STAGE"

# Stop service if running
systemctl stop familyboard.service 2>/dev/null || true

# Restore database
if [ -f "$TMP_STAGE/familyboard.db" ]; then
  echo "Restoring database to /var/lib/familyboard/familyboard.db..."
  mkdir -p /var/lib/familyboard
  cp "$TMP_STAGE/familyboard.db" /var/lib/familyboard/familyboard.db
  chown familyboard:familyboard /var/lib/familyboard/familyboard.db
  chmod 0640 /var/lib/familyboard/familyboard.db
fi

# Restore config
if [ -f "$TMP_STAGE/config.toml" ]; then
  echo "Restoring configuration to /etc/familyboard/config.toml..."
  mkdir -p /etc/familyboard
  cp "$TMP_STAGE/config.toml" /etc/familyboard/config.toml
  chown root:familyboard /etc/familyboard/config.toml
  chmod 0640 /etc/familyboard/config.toml
fi

# Restore credentials
if [ -d "$TMP_STAGE/credentials" ]; then
  echo "Restoring OAuth credentials..."
  mkdir -p /etc/familyboard/credentials
  cp -r "$TMP_STAGE/credentials/"* /etc/familyboard/credentials/ 2>/dev/null || true
  chown -R familyboard:familyboard /etc/familyboard/credentials
  chmod 0700 /etc/familyboard/credentials
  chmod 0600 /etc/familyboard/credentials/* 2>/dev/null || true
fi

# Restart service
systemctl restart familyboard.service 2>/dev/null || true

echo "SUCCESS: Migration bundle restored."
