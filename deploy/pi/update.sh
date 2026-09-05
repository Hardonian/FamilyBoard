#!/bin/bash
# FamilyBoard Safe Appliance Update Script with Rollback
# Coordinates backup, staged binary swap, health verification, and rollback

set -Eeuo pipefail

NEW_BINARY="${1:-}"

if [ -z "$NEW_BINARY" ] || [ ! -f "$NEW_BINARY" ]; then
  echo "Usage: sudo ./deploy/pi/update.sh <path-to-new-familyboard-binary>" >&2
  exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: Must be run as root (use sudo)." >&2
  exit 1
fi

echo "=== FamilyBoard Safe Appliance Update ==="

TARGET_BIN="/usr/local/bin/familyboard"
BACKUP_BIN="/usr/local/bin/familyboard.known-good"

# 1. Create Pre-Update Backup
echo "Creating pre-update database backup..."
if command -v familyboardctl >/dev/null 2>&1; then
  familyboardctl backup --to /var/lib/familyboard/backups || true
fi

# 2. Stage and Verify New Binary
echo "Verifying new binary architecture..."
if ! "$NEW_BINARY" --version >/dev/null 2>&1; then
  echo "ERROR: Candidate binary failed self-execution verification." >&2
  exit 1
fi

# 3. Save known-good binary for rollback
if [ -f "$TARGET_BIN" ]; then
  cp "$TARGET_BIN" "$BACKUP_BIN"
fi

# 4. Atomic Binary Swap
echo "Performing atomic binary swap..."
install -m 0755 "$NEW_BINARY" "$TARGET_BIN"

# 5. Restart Service
echo "Restarting familyboard.service..."
systemctl restart familyboard.service

# 6. Verify Health Post-Update
echo "Verifying appliance health post-update..."
HEALTHY=false
for i in $(seq 1 15); do
  if curl -s -f "http://127.0.0.1:8080/health/ready" >/dev/null 2>&1; then
    HEALTHY=true
    break
  fi
  sleep 2
done

if [ "$HEALTHY" = true ]; then
  echo "SUCCESS: Appliance update verified and healthy."
  exit 0
else
  echo "ERROR: Appliance failed post-update health check! Initiating rollback..." >&2
  if [ -f "$BACKUP_BIN" ]; then
    cp "$BACKUP_BIN" "$TARGET_BIN"
    systemctl restart familyboard.service
    echo "Rollback completed. Known-good binary restored."
  fi
  exit 1
fi
