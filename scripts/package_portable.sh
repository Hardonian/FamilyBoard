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

COMMIT="${COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo dev)}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct HEAD 2>/dev/null || date +%s)}"
BUILD_DATE="${BUILD_DATE:-$(git show -s --format=%cI HEAD 2>/dev/null || date -u +'%Y-%m-%dT%H:%M:%SZ')}"
LDFLAGS="-s -w -X github.com/Hardonian/FamilyBoard/internal/buildinfo.Version=$VERSION -X github.com/Hardonian/FamilyBoard/internal/buildinfo.Commit=$COMMIT -X github.com/Hardonian/FamilyBoard/internal/buildinfo.Date=$BUILD_DATE"
OUTPUT_DIR="dist/releases/${VERSION}"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
mkdir -p "$OUTPUT_DIR"

package_target() {
  local goos="$1"
  local goarch="$2"
  local extension=""
  if [ "$goos" = "windows" ]; then
    extension=".exe"
  fi

  local package_name="familyboard-${VERSION}-${goos}-${goarch}"
  local package_dir="${WORK_DIR}/${package_name}"
  local archive_path="${OUTPUT_DIR}/${package_name}.tar.gz"
  mkdir -p "$package_dir/docs"

  echo "Building ${goos}/${goarch}..."
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "${package_dir}/familyboard${extension}" ./cmd/familyboard
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "${package_dir}/familyboardctl${extension}" ./cmd/familyboardctl

  cp VERSION LICENSE CHANGELOG.md config.example.toml README.md "$package_dir/"
  cp docs/OPERATIONS.md docs/RECOVERY_CARD.md "$package_dir/docs/"
  if [ "$goos" = "linux" ]; then
    cp -r deploy "$package_dir/deploy"
  fi

  if tar --version 2>/dev/null | grep -q 'GNU tar'; then
    tar --sort=name --mtime="@${SOURCE_DATE_EPOCH}" --owner=0 --group=0 --numeric-owner -cf - -C "$WORK_DIR" "$package_name" | gzip -n > "$archive_path"
  else
    tar -czf "$archive_path" -C "$WORK_DIR" "$package_name"
  fi
  rm -r "$package_dir"
}

package_target linux amd64
package_target linux arm64
package_target windows amd64
package_target darwin arm64

(
  cd "$OUTPUT_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./*.tar.gz > SHA256SUMS
    sha256sum -c SHA256SUMS
  else
    shasum -a 256 ./*.tar.gz > SHA256SUMS
    shasum -a 256 -c SHA256SUMS
  fi
)

echo "Portable release artifacts:"
ls -lh "$OUTPUT_DIR"
