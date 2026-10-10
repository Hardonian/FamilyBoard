#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "$PROJECT_ROOT"

VERSION_VALUE="$(tr -d '\r\n' < VERSION)"
if [[ ! "$VERSION_VALUE" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "ERROR: VERSION must contain a valid semantic version; found '$VERSION_VALUE'." >&2
  exit 1
fi

EXPECTED='VERSION="${FAMILYBOARD_VERSION:-'"$VERSION_VALUE"'}"'
for installer in familyboard-setup.sh deploy/pi/familyboard-setup.sh; do
  if ! grep -Fq "$EXPECTED" "$installer"; then
    echo "ERROR: $installer does not default to VERSION $VERSION_VALUE." >&2
    exit 1
  fi
done

if ! cmp -s familyboard-setup.sh deploy/pi/familyboard-setup.sh; then
  echo "ERROR: The root and deploy/pi setup scripts have drifted." >&2
  exit 1
fi

if ! grep -Fq "## [$VERSION_VALUE]" CHANGELOG.md; then
  echo "ERROR: CHANGELOG.md has no release section for $VERSION_VALUE." >&2
  exit 1
fi

echo "Version metadata is consistent: $VERSION_VALUE"
