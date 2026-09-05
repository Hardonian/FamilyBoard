#!/bin/bash
set -euo pipefail

VERSION="${1:-1.0.0-rc.1}"
DIST_DIR="dist/familyboard-${VERSION}-linux-arm64"

rm -rf "$DIST_DIR" "dist/familyboard-${VERSION}-linux-arm64.tar.gz"*
mkdir -p "$DIST_DIR"

echo "Packaging FamilyBoard ${VERSION} ARM64 release..."
cp familyboard-linux-arm64 "$DIST_DIR/familyboard"
cp familyboardctl-linux-arm64 "$DIST_DIR/familyboardctl"
cp -r deploy "$DIST_DIR/deploy"
cp config.example.toml "$DIST_DIR/"
cp README.md "$DIST_DIR/"
cp docs/OPERATIONS.md "$DIST_DIR/"
cp docs/RECOVERY_CARD.md "$DIST_DIR/"

tar -czf "dist/familyboard-${VERSION}-linux-arm64.tar.gz" -C dist "familyboard-${VERSION}-linux-arm64"
(cd dist && sha256sum "familyboard-${VERSION}-linux-arm64.tar.gz" > "familyboard-${VERSION}-linux-arm64.tar.gz.sha256")

echo "Package complete:"
ls -lh dist/
cat "dist/familyboard-${VERSION}-linux-arm64.tar.gz.sha256"
