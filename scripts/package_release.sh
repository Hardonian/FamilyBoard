#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$PROJECT_ROOT"

RELEASE_VERSION="$(tr -d '\r\n' < VERSION)"
VERSION="${1:-$RELEASE_VERSION}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "ERROR: Invalid release version: $VERSION" >&2
  exit 1
fi
if [ "$VERSION" != "$RELEASE_VERSION" ]; then
  echo "ERROR: Requested version $VERSION does not match VERSION ($RELEASE_VERSION)." >&2
  exit 1
fi

PACKAGE_NAME="familyboard-${VERSION}-linux-arm64"
ARCHIVE_PATH="dist/${PACKAGE_NAME}.tar.gz"
CHECKSUM_PATH="${ARCHIVE_PATH}.sha256"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
PACKAGE_DIR="${WORK_DIR}/${PACKAGE_NAME}"
mkdir -p "$PACKAGE_DIR" dist
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct HEAD 2>/dev/null || date +%s)}"

echo "Packaging FamilyBoard ${VERSION} ARM64 release..."
cp familyboard-linux-arm64 "$PACKAGE_DIR/familyboard"
cp familyboardctl-linux-arm64 "$PACKAGE_DIR/familyboardctl"
cp -r deploy "$PACKAGE_DIR/deploy"
cp VERSION LICENSE CHANGELOG.md config.example.toml README.md "$PACKAGE_DIR/"
mkdir -p "$PACKAGE_DIR/docs"
cp docs/OPERATIONS.md docs/RECOVERY_CARD.md docs/MICROSD_ONE_SHOT.md "$PACKAGE_DIR/docs/"

if tar --version 2>/dev/null | grep -q 'GNU tar'; then
  tar --sort=name --mtime="@${SOURCE_DATE_EPOCH}" --owner=0 --group=0 --numeric-owner -cf - -C "$WORK_DIR" "$PACKAGE_NAME" | gzip -n > "$ARCHIVE_PATH"
else
  tar -czf "$ARCHIVE_PATH" -C "$WORK_DIR" "$PACKAGE_NAME"
fi
(
  cd dist
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${PACKAGE_NAME}.tar.gz" > "${PACKAGE_NAME}.tar.gz.sha256"
  else
    shasum -a 256 "${PACKAGE_NAME}.tar.gz" > "${PACKAGE_NAME}.tar.gz.sha256"
  fi
)

echo "Package complete:"
ls -lh "$ARCHIVE_PATH" "$CHECKSUM_PATH"
cat "$CHECKSUM_PATH"
