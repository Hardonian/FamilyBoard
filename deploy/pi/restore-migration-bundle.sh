#!/bin/bash
# Validate and restore a FamilyBoard migration bundle with automatic rollback.

set -Eeuo pipefail
umask 077

BUNDLE_PATH="${1:-}"
if [ -z "$BUNDLE_PATH" ] || [ ! -f "$BUNDLE_PATH" ]; then
  echo "Usage: sudo restore-migration-bundle.sh <migration-bundle.tar.gz>" >&2
  exit 2
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: Restore must be run as root (use sudo)." >&2
  exit 1
fi

for required_tool in tar sqlite3 sha256sum flock curl; do
  command -v "$required_tool" >/dev/null 2>&1 || {
    echo "ERROR: Required tool '$required_tool' is not installed." >&2
    exit 1
  }
done

BUNDLE_PATH="$(cd "$(dirname "$BUNDLE_PATH")" && pwd -P)/$(basename "$BUNDLE_PATH")"
LOCK_FILE="/run/lock/familyboard-maintenance.lock"
if [ "${FAMILYBOARD_MAINTENANCE_LOCK_HELD:-0}" != "1" ]; then
  install -d -m 0755 "$(dirname "$LOCK_FILE")"
  exec 9>"$LOCK_FILE"
  flock -n 9 || {
    echo "ERROR: Another FamilyBoard install, update, or restore is running." >&2
    exit 1
  }
fi

TMP_STAGE=$(mktemp -d "${TMPDIR:-/tmp}/familyboard-restore.XXXXXX")
ROLLBACK_STAGE=$(mktemp -d "${TMPDIR:-/tmp}/familyboard-rollback.XXXXXX")
SERVICE_STOPPED=false
RESTORE_COMMITTED=false
CHANGES_STARTED=false

cleanup() {
  local exit_code=$?
  if [ "$CHANGES_STARTED" = true ] && [ "$RESTORE_COMMITTED" != true ]; then
    rollback_restore || true
    SERVICE_STOPPED=false
  elif [ "$SERVICE_STOPPED" = true ] && [ "$RESTORE_COMMITTED" != true ]; then
    systemctl start familyboard.service >/dev/null 2>&1 || true
  fi
  rm -rf -- "$TMP_STAGE" "$ROLLBACK_STAGE"
  exit "$exit_code"
}
trap cleanup EXIT

echo "Validating archive structure..."
mapfile -t ARCHIVE_ENTRIES < <(tar -tzf "$BUNDLE_PATH")
[ ${#ARCHIVE_ENTRIES[@]} -gt 0 ] || {
  echo "ERROR: Migration archive is empty." >&2
  exit 1
}

for entry in "${ARCHIVE_ENTRIES[@]}"; do
  clean_entry="${entry#./}"
  clean_entry="${clean_entry%/}"
  if [[ "$clean_entry" = /* || "$clean_entry" = ".." || "$clean_entry" = ../* || "$clean_entry" = */../* || "$clean_entry" = */.. || "$clean_entry" = *\\* ]]; then
    echo "ERROR: Unsafe archive path rejected: $entry" >&2
    exit 1
  fi
  case "$clean_entry" in
    ""|familyboard.db|config.toml|migration-manifest.json|SHA256SUMS|credentials|credentials/*)
      ;;
    *)
      echo "ERROR: Unexpected archive member rejected: $entry" >&2
      exit 1
      ;;
  esac
done

# Only regular files and directories are permitted. This prevents link-based
# extraction attacks even when a bundle came from an untrusted removable disk.
if ! LC_ALL=C tar -tvzf "$BUNDLE_PATH" | awk '{ type=substr($1,1,1); if (type != "-" && type != "d") exit 1 }'; then
  echo "ERROR: Archive contains a symlink, hard link, or special file." >&2
  exit 1
fi

tar -xzf "$BUNDLE_PATH" -C "$TMP_STAGE" --no-same-owner --no-same-permissions
if find "$TMP_STAGE" -mindepth 1 \( -type l -o \! -type f \! -type d \) -print -quit | grep -q .; then
  echo "ERROR: Extracted archive contains an unsupported file type." >&2
  exit 1
fi

if [ -f "$TMP_STAGE/SHA256SUMS" ]; then
  echo "Verifying bundle checksums..."
  (cd "$TMP_STAGE" && sha256sum --check --strict SHA256SUMS)
else
  echo "WARNING: Legacy bundle has no SHA256SUMS; only database integrity can be verified." >&2
fi

[ -f "$TMP_STAGE/familyboard.db" ] || {
  echo "ERROR: Archive is missing familyboard.db." >&2
  exit 1
}
integrity=$(sqlite3 "$TMP_STAGE/familyboard.db" "PRAGMA integrity_check;")
[ "$integrity" = "ok" ] || {
  echo "ERROR: Restored database failed integrity validation: $integrity" >&2
  exit 1
}

id familyboard >/dev/null 2>&1 || {
  echo "ERROR: The FamilyBoard service account does not exist; install the appliance first." >&2
  exit 1
}

install -d -m 0700 -o familyboard -g familyboard /var/lib/familyboard/backups
BACKUP_TOOL="/usr/local/libexec/familyboard/create-migration-bundle"
if [ ! -x "$BACKUP_TOOL" ]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  BACKUP_TOOL="$SCRIPT_DIR/create-migration-bundle.sh"
fi
if [ -x "$BACKUP_TOOL" ] && [ -r /var/lib/familyboard/familyboard.db ]; then
  echo "Creating a verified pre-restore safety backup..."
  PRE_RESTORE_BACKUP=$("$BACKUP_TOOL" \
    --destination /var/lib/familyboard/backups \
    --prefix pre-restore \
    --keep 5)
  echo "Safety backup: $PRE_RESTORE_BACKUP"
fi

DB_PATH="/var/lib/familyboard/familyboard.db"
CONFIG_PATH="/etc/familyboard/config.toml"
CREDENTIALS_PATH="/var/lib/familyboard/credentials"

systemctl stop familyboard.service
SERVICE_STOPPED=true

DB_EXISTED=false
CONFIG_EXISTED=false
CREDENTIALS_EXISTED=false
if [ -f "$DB_PATH" ]; then
  DB_EXISTED=true
  rollback_db_escaped=${ROLLBACK_STAGE//\'/\'\'}
  sqlite3 "$DB_PATH" ".backup '${rollback_db_escaped}/familyboard.db'"
fi
if [ -f "$CONFIG_PATH" ]; then
  CONFIG_EXISTED=true
  cp -a -- "$CONFIG_PATH" "$ROLLBACK_STAGE/config.toml"
fi
if [ -d "$CREDENTIALS_PATH" ]; then
  CREDENTIALS_EXISTED=true
  cp -a -- "$CREDENTIALS_PATH" "$ROLLBACK_STAGE/credentials"
fi

rollback_restore() {
  echo "Restore health check failed; rolling back the previous appliance state..." >&2
  systemctl stop familyboard.service >/dev/null 2>&1 || true
  if [ "$DB_EXISTED" = true ]; then
    install -m 0640 -o familyboard -g familyboard "$ROLLBACK_STAGE/familyboard.db" "$DB_PATH"
  else
    rm -f -- "$DB_PATH"
  fi
  rm -f -- "${DB_PATH}-wal" "${DB_PATH}-shm"
  if [ "$CONFIG_EXISTED" = true ]; then
    install -d -m 0770 -o root -g familyboard /etc/familyboard
    install -m 0640 -o root -g familyboard "$ROLLBACK_STAGE/config.toml" "$CONFIG_PATH"
  else
    rm -f -- "$CONFIG_PATH"
  fi
  install -d -m 0700 -o familyboard -g familyboard "$CREDENTIALS_PATH"
  find "$CREDENTIALS_PATH" -mindepth 1 -delete
  if [ "$CREDENTIALS_EXISTED" = true ]; then
    cp -a -- "$ROLLBACK_STAGE/credentials/." "$CREDENTIALS_PATH/"
    chown -R familyboard:familyboard "$CREDENTIALS_PATH"
  fi
  systemctl start familyboard.service >/dev/null 2>&1 || true
}

echo "Installing restored database..."
CHANGES_STARTED=true
install -d -m 0750 -o familyboard -g familyboard /var/lib/familyboard
install -m 0640 -o familyboard -g familyboard "$TMP_STAGE/familyboard.db" "${DB_PATH}.restore"
mv -f -- "${DB_PATH}.restore" "$DB_PATH"
rm -f -- "${DB_PATH}-wal" "${DB_PATH}-shm"

if [ -f "$TMP_STAGE/config.toml" ]; then
  echo "Installing restored configuration..."
  install -d -m 0770 -o root -g familyboard /etc/familyboard
  install -m 0640 -o root -g familyboard "$TMP_STAGE/config.toml" "${CONFIG_PATH}.restore"
  mv -f -- "${CONFIG_PATH}.restore" "$CONFIG_PATH"
fi

if [ -d "$TMP_STAGE/credentials" ]; then
  echo "Installing restored OAuth credentials..."
  install -d -m 0700 -o familyboard -g familyboard "$CREDENTIALS_PATH"
  find "$CREDENTIALS_PATH" -mindepth 1 -delete
  cp -a -- "$TMP_STAGE/credentials/." "$CREDENTIALS_PATH/"
  chown -R familyboard:familyboard "$CREDENTIALS_PATH"
  find "$CREDENTIALS_PATH" -type d -exec chmod 0700 {} +
  find "$CREDENTIALS_PATH" -type f -exec chmod 0600 {} +
fi

systemctl start familyboard.service
SERVICE_STOPPED=false

HEALTHY=false
for _ in $(seq 1 30); do
  if curl --fail --silent --show-error --max-time 2 http://127.0.0.1:8080/health/ready >/dev/null 2>&1; then
    HEALTHY=true
    break
  fi
  sleep 1
done
if [ "$HEALTHY" != true ]; then
  SERVICE_STOPPED=true
  rollback_restore
  SERVICE_STOPPED=false
  CHANGES_STARTED=false
  journalctl -u familyboard.service -n 50 --no-pager >&2 || true
  exit 1
fi

RESTORE_COMMITTED=true
echo "SUCCESS: Migration bundle restored and readiness verified."
