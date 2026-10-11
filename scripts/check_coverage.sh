#!/usr/bin/env bash
set -euo pipefail

echo "=== Checking per-package code coverage thresholds ==="

COVER_FILE="coverage.out"
go test -coverprofile="${COVER_FILE}" ./internal/... ./cmd/... >/dev/null

check_package() {
    local pkg="$1"
    local min="$2"
    local cov
    cov=$(go test -cover "${pkg}" | grep -o '[0-9.]\+%' | tr -d '%' || echo "0")
    if [ -z "${cov}" ]; then
        cov="0"
    fi
    echo "Package ${pkg}: ${cov}% (minimum: ${min}%)"
    awk -v c="${cov}" -v m="${min}" 'BEGIN { if (c + 0 < m + 0) { exit 1 } }' || {
        echo "ERROR: Coverage for ${pkg} (${cov}%) is below required threshold (${min}%)" >&2
        exit 1
    }
}

check_package "./internal/clock" 80
check_package "./internal/errdefs" 80
check_package "./internal/timezone" 70
check_package "./internal/config" 70
check_package "./internal/queue" 55
check_package "./internal/backup" 55
check_package "./internal/restore" 55

echo "All critical package coverage thresholds satisfied!"
