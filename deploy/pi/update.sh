#!/bin/bash
# Atomically update FamilyBoard, verify readiness, and roll back code and data.

set -Eeuo pipefail
umask 027

NEW_BINARY="${1:-}"
NEW_CTL_BINARY="${2:-}"
if [ -z "$NEW_BINARY" ] || [ ! -f "$NEW_BINARY" ]; then
  echo "Usage: sudo update.sh <new-familyboard-binary> [new-familyboardctl-binary]" >&2
  exit 2
fi
if [ -n "$NEW_CTL_BINARY" ] && [ ! -f "$NEW_CTL_BINARY" ]; then
  echo "ERROR: familyboardctl candidate does not exist: $NEW_CTL_BINARY" >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: Update must be run as root (use sudo)." >&2
  exit 1
fi

for required_tool in systemctl curl flock install; do
  command -v "$required_tool" >/dev/null 2>&1 || {
    echo "ERROR: Required tool '$required_tool' is not installed." >&2
    exit 1
  }
done

NEW_BINARY="$(cd "$(dirname "$NEW_BINARY")" && pwd -P)/$(basename "$NEW_BINARY")"
if [ -n "$NEW_CTL_BINARY" ]; then
  NEW_CTL_BINARY="$(cd "$(dirname "$NEW_CTL_BINARY")" && pwd -P)/$(basename "$NEW_CTL_BINARY")"
fi

LOCK_FILE="/run/lock/familyboard-maintenance.lock"
install -d -m 0755 "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
flock -n 9 || {
  echo "ERROR: Another FamilyBoard install, update, or restore is running." >&2
  exit 1
}

TARGET_BIN="/usr/local/bin/familyboard"
TARGET_CTL="/usr/local/bin/familyboardctl"
BACKUP_BIN="/usr/local/bin/familyboard.known-good"
BACKUP_CTL="/usr/local/bin/familyboardctl.known-good"
BACKUP_DIR="/var/lib/familyboard/backups"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKUP_TOOL="/usr/local/libexec/familyboard/create-migration-bundle"
RESTORE_TOOL="/usr/local/libexec/familyboard/restore-migration-bundle"
[ -x "$BACKUP_TOOL" ] || BACKUP_TOOL="$SCRIPT_DIR/create-migration-bundle.sh"
[ -x "$RESTORE_TOOL" ] || RESTORE_TOOL="$SCRIPT_DIR/restore-migration-bundle.sh"
[ -x "$BACKUP_TOOL" ] || { echo "ERROR: Backup tool is unavailable." >&2; exit 1; }
[ -x "$RESTORE_TOOL" ] || { echo "ERROR: Restore tool is unavailable." >&2; exit 1; }
[ -x "$TARGET_BIN" ] || { echo "ERROR: No installed FamilyBoard binary exists at $TARGET_BIN." >&2; exit 1; }

echo "Validating candidate binaries on $(uname -m)..."
[ -x "$NEW_BINARY" ] || { echo "ERROR: Candidate daemon is not executable: $NEW_BINARY" >&2; exit 1; }
NEW_VERSION=$("$NEW_BINARY" --version)
echo "Candidate daemon: $NEW_VERSION"
if [ -n "$NEW_CTL_BINARY" ]; then
  [ -x "$NEW_CTL_BINARY" ] || { echo "ERROR: Candidate CLI is not executable: $NEW_CTL_BINARY" >&2; exit 1; }
  NEW_CTL_VERSION=$("$NEW_CTL_BINARY" version)
  echo "Candidate CLI:    $NEW_CTL_VERSION"
fi

install -d -m 0700 -o familyboard -g familyboard "$BACKUP_DIR"
echo "Creating verified pre-update data and credential backup..."
PRE_UPDATE_BUNDLE=$("$BACKUP_TOOL" \
  --destination "$BACKUP_DIR" \
  --prefix pre-update \
  --keep 5)
[ -s "$PRE_UPDATE_BUNDLE" ] || { echo "ERROR: Pre-update backup was not created." >&2; exit 1; }
echo "Pre-update backup: $PRE_UPDATE_BUNDLE"

install -m 0755 "$TARGET_BIN" "$BACKUP_BIN"
if [ -x "$TARGET_CTL" ]; then
  install -m 0755 "$TARGET_CTL" "$BACKUP_CTL"
fi

SWAPPED=false
UPDATE_SUCCEEDED=false

rollback_update() {
  set +e
  echo "Rolling back FamilyBoard binary and appliance data..." >&2
  systemctl stop familyboard.service >/dev/null 2>&1
  install -m 0755 "$BACKUP_BIN" "${TARGET_BIN}.rollback"
  mv -f -- "${TARGET_BIN}.rollback" "$TARGET_BIN"
  if [ -f "$BACKUP_CTL" ]; then
    install -m 0755 "$BACKUP_CTL" "${TARGET_CTL}.rollback"
    mv -f -- "${TARGET_CTL}.rollback" "$TARGET_CTL"
  fi
  FAMILYBOARD_MAINTENANCE_LOCK_HELD=1 "$RESTORE_TOOL" "$PRE_UPDATE_BUNDLE"
  local restore_status=$?
  if [ "$restore_status" -ne 0 ]; then
    echo "CRITICAL: Automatic data rollback failed. Safety backup: $PRE_UPDATE_BUNDLE" >&2
  else
    echo "Rollback completed and previous release readiness was verified." >&2
  fi
  SWAPPED=false
  return "$restore_status"
}

cleanup() {
  local exit_code=$?
  rm -f -- "${TARGET_BIN}.new" "${TARGET_CTL}.new"
  if [ "$SWAPPED" = true ] && [ "$UPDATE_SUCCEEDED" != true ]; then
    rollback_update || true
  fi
  exit "$exit_code"
}
trap cleanup EXIT

echo "Performing atomic binary swap..."
install -m 0755 "$NEW_BINARY" "${TARGET_BIN}.new"
mv -f -- "${TARGET_BIN}.new" "$TARGET_BIN"
if [ -n "$NEW_CTL_BINARY" ]; then
  install -m 0755 "$NEW_CTL_BINARY" "${TARGET_CTL}.new"
  mv -f -- "${TARGET_CTL}.new" "$TARGET_CTL"
fi
SWAPPED=true

systemctl restart familyboard.service || true
echo "Waiting up to 45 seconds for the updated daemon to become ready..."
HEALTHY=false
for _ in $(seq 1 23); do
  if curl --fail --silent --show-error --max-time 2 http://127.0.0.1:8080/health/ready >/dev/null 2>&1; then
    HEALTHY=true
    break
  fi
  sleep 2
done

if [ "$HEALTHY" != true ]; then
  echo "ERROR: Updated daemon failed its readiness check." >&2
  journalctl -u familyboard.service -n 75 --no-pager >&2 || true
  rollback_update || true
  exit 1
fi

UPDATE_SUCCEEDED=true
echo "SUCCESS: Appliance update is healthy. Rollback binary: $BACKUP_BIN"
echo "Safety backup retained at: $PRE_UPDATE_BUNDLE"
