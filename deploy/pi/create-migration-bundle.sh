#!/bin/bash
# Create a consistent, encrypted-at-rest-ready appliance migration bundle.
# The archive contains the SQLite database, configuration, and OAuth tokens.

set -Eeuo pipefail
umask 077

DEST_DIR="."
PREFIX="familyboard-migration-bundle"
KEEP=0

usage() {
  cat >&2 <<'EOF'
Usage: create-migration-bundle.sh [DESTINATION]
       create-migration-bundle.sh [--destination DIR] [--prefix NAME] [--keep COUNT]

The final line on stdout is the absolute path of the completed archive.
All other progress is written to stderr so automation can capture that path.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --destination)
      [ $# -ge 2 ] || { usage; exit 2; }
      DEST_DIR="$2"
      shift 2
      ;;
    --prefix)
      [ $# -ge 2 ] || { usage; exit 2; }
      PREFIX="$2"
      shift 2
      ;;
    --keep)
      [ $# -ge 2 ] || { usage; exit 2; }
      KEEP="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --*)
      echo "ERROR: Unknown option: $1" >&2
      usage
      exit 2
      ;;
    *)
      if [ "$DEST_DIR" != "." ]; then
        echo "ERROR: Only one destination may be supplied." >&2
        usage
        exit 2
      fi
      DEST_DIR="$1"
      shift
      ;;
  esac
done

[[ "$PREFIX" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || {
  echo "ERROR: Backup prefix contains unsupported characters." >&2
  exit 2
}
[[ "$KEEP" =~ ^[0-9]+$ ]] || {
  echo "ERROR: --keep must be a non-negative integer." >&2
  exit 2
}

for required_tool in sqlite3 tar sha256sum flock; do
  command -v "$required_tool" >/dev/null 2>&1 || {
    echo "ERROR: Required tool '$required_tool' is not installed." >&2
    exit 1
  }
done

CONFIG_PATH="/etc/familyboard/config.toml"
DATABASE_PATH="/var/lib/familyboard/familyboard.db"
CREDENTIALS_PATH="/var/lib/familyboard/credentials"

# Read the simple quoted string values used by FamilyBoard's generated TOML.
toml_string() {
  local section="$1"
  local key="$2"
  local file="$3"
  awk -v wanted_section="$section" -v wanted_key="$key" '
    /^[[:space:]]*\[/ {
      current=$0
      gsub(/^[[:space:]]*\[|\][[:space:]]*$/, "", current)
      next
    }
    current == wanted_section && $0 ~ "^[[:space:]]*" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      sub(/[[:space:]]*#.*/, "", value)
      gsub(/^[[:space:]"]+|[[:space:]"]+$/, "", value)
      print value
      exit
    }
  ' "$file"
}

if [ -r "$CONFIG_PATH" ]; then
  configured_db=$(toml_string database path "$CONFIG_PATH")
  configured_credentials=$(toml_string security credentials_dir "$CONFIG_PATH")
  if [ -n "$configured_db" ]; then
    if [[ "$configured_db" = /* ]]; then
      DATABASE_PATH="$configured_db"
    else
      DATABASE_PATH="/var/lib/familyboard/$configured_db"
    fi
  fi
  if [ -n "$configured_credentials" ]; then
    if [[ "$configured_credentials" = /* ]]; then
      CREDENTIALS_PATH="$configured_credentials"
    else
      CREDENTIALS_PATH="/var/lib/familyboard/$configured_credentials"
    fi
  fi
fi

[ -r "$DATABASE_PATH" ] || {
  echo "ERROR: FamilyBoard database is not readable at $DATABASE_PATH." >&2
  exit 1
}

if [ ! -d "$DEST_DIR" ]; then
  mkdir -p -- "$DEST_DIR"
  chmod 0700 "$DEST_DIR"
fi
DEST_DIR="$(cd "$DEST_DIR" && pwd -P)"
[ -w "$DEST_DIR" ] || {
  echo "ERROR: Backup destination is not writable: $DEST_DIR" >&2
  exit 1
}

LOCK_PATH="/run/familyboard/backup.lock"
if [ ! -d /run/familyboard ] || [ ! -w /run/familyboard ]; then
  LOCK_PATH="/tmp/familyboard-backup-$(id -u).lock"
fi
exec 8>"$LOCK_PATH"
flock -n 8 || {
  echo "ERROR: Another FamilyBoard backup is already running." >&2
  exit 1
}

TIMESTAMP=$(date -u +"%Y%m%d-%H%M%SZ")
BUNDLE_NAME="${PREFIX}-${TIMESTAMP}.tar.gz"
BUNDLE_PATH="${DEST_DIR}/${BUNDLE_NAME}"
TMP_STAGE=$(mktemp -d "${TMPDIR:-/tmp}/familyboard-backup.XXXXXX")
TMP_ARCHIVE=$(mktemp "${DEST_DIR}/.${BUNDLE_NAME}.partial.XXXXXX")

cleanup() {
  rm -rf -- "$TMP_STAGE"
  rm -f -- "$TMP_ARCHIVE"
}
trap cleanup EXIT

echo "Snapshotting SQLite database..." >&2
escaped_snapshot=${TMP_STAGE//\'/\'\'}
sqlite3 "$DATABASE_PATH" "PRAGMA busy_timeout=30000; VACUUM INTO '${escaped_snapshot}/familyboard.db';" >/dev/null
integrity=$(sqlite3 "$TMP_STAGE/familyboard.db" "PRAGMA integrity_check;")
[ "$integrity" = "ok" ] || {
  echo "ERROR: Snapshot integrity check failed: $integrity" >&2
  exit 1
}
chmod 0600 "$TMP_STAGE/familyboard.db"

if [ -r "$CONFIG_PATH" ]; then
  install -m 0600 "$CONFIG_PATH" "$TMP_STAGE/config.toml"
fi

if [ -d "$CREDENTIALS_PATH" ]; then
  echo "Copying OAuth credentials..." >&2
  if find "$CREDENTIALS_PATH" -mindepth 1 \( -type l -o \! -type f \! -type d \) -print -quit | grep -q .; then
    echo "ERROR: Credentials contain a symlink or special file; refusing to archive it." >&2
    exit 1
  fi
  mkdir -m 0700 "$TMP_STAGE/credentials"
  cp -a -- "$CREDENTIALS_PATH/." "$TMP_STAGE/credentials/"
  find "$TMP_STAGE/credentials" -type d -exec chmod 0700 {} +
  find "$TMP_STAGE/credentials" -type f -exec chmod 0600 {} +
fi

VERSION="unknown"
if command -v familyboard >/dev/null 2>&1; then
  VERSION=$(familyboard --version 2>/dev/null | head -n 1 || true)
fi
OS_NAME=$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release 2>/dev/null | head -n 1 | tr -d '"' || true)
json_escape() {
  local value="$1"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/ }
  value=${value//$'\r'/ }
  printf '%s' "$value"
}
cat > "$TMP_STAGE/migration-manifest.json" <<EOF
{
  "created_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "version": "$(json_escape "$VERSION")",
  "os": "$(json_escape "${OS_NAME:-Linux}")",
  "arch": "$(json_escape "$(uname -m)")"
}
EOF
chmod 0600 "$TMP_STAGE/migration-manifest.json"

(
  cd "$TMP_STAGE"
  LC_ALL=C find . -type f ! -name SHA256SUMS -print | LC_ALL=C sort | xargs sha256sum > SHA256SUMS
)
chmod 0600 "$TMP_STAGE/SHA256SUMS"

echo "Compressing verified archive..." >&2
tar -czf "$TMP_ARCHIVE" -C "$TMP_STAGE" .
chmod 0600 "$TMP_ARCHIVE"
mv -f -- "$TMP_ARCHIVE" "$BUNDLE_PATH"

if [ "$KEEP" -gt 0 ]; then
  shopt -s nullglob
  old_bundles=("$DEST_DIR"/"$PREFIX"-*.tar.gz)
  while [ ${#old_bundles[@]} -gt "$KEEP" ]; do
    rm -f -- "${old_bundles[0]}"
    old_bundles=("${old_bundles[@]:1}")
  done
fi

echo "SUCCESS: Verified migration bundle created at $BUNDLE_PATH" >&2
printf '%s\n' "$BUNDLE_PATH"
