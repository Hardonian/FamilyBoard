#!/bin/bash
# FamilyBoard Migration Bundle Creator
# Packages database, configuration, and credentials into an archive for device migration or reimaging

set -Eeuo pipefail

DEST_DIR="${1:-.}"
TIMESTAMP=$(date +"%Y%m%d-%H%M%S")
BUNDLE_NAME="familyboard-migration-bundle-${TIMESTAMP}.tar.gz"
BUNDLE_PATH="${DEST_DIR}/${BUNDLE_NAME}"

echo "=== Creating FamilyBoard Migration Bundle ==="

TMP_STAGE=$(mktemp -d "/tmp/fb-migrate-XXXXXX")
trap 'rm -rf "$TMP_STAGE"' EXIT

# 1. Snapshot active SQLite database
if [ -f /var/lib/familyboard/familyboard.db ]; then
  echo "Snapshotting SQLite database..."
  if command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 /var/lib/familyboard/familyboard.db "VACUUM INTO '$TMP_STAGE/familyboard.db';"
  else
    cp /var/lib/familyboard/familyboard.db "$TMP_STAGE/familyboard.db"
  fi
fi

# 2. Copy configuration
if [ -f /etc/familyboard/config.toml ]; then
  echo "Copying appliance configuration..."
  cp /etc/familyboard/config.toml "$TMP_STAGE/config.toml"
fi

# 3. Copy credentials directory
if [ -d /etc/familyboard/credentials ]; then
  echo "Copying OAuth credential tokens..."
  cp -r /etc/familyboard/credentials "$TMP_STAGE/credentials"
fi

# 4. Generate manifest
cat <<EOF > "$TMP_STAGE/migration-manifest.json"
{
  "created_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "version": "0.9.0",
  "os": "$(cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d= -f2 || echo 'Linux')",
  "arch": "$(uname -m)"
}
EOF

# 5. Build bundle
echo "Compressing archive to $BUNDLE_PATH..."
tar -czf "$BUNDLE_PATH" -C "$TMP_STAGE" .
chmod 0600 "$BUNDLE_PATH"

echo "SUCCESS: Migration bundle created at $BUNDLE_PATH"
