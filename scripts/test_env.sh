#!/usr/bin/env bash
set -euo pipefail

# scripts/test_env.sh
# Verifies developer environment prerequisites or executes release gate in container.

MODE="${1:-check}"

check_tool() {
    local cmd="$1"
    local desc="$2"
    if command -v "$cmd" >/dev/null 2>&1; then
        echo "  [OK] $desc ($cmd)"
    else
        echo "  [MISSING] $desc ($cmd)"
        return 1
    fi
}

case "$MODE" in
    check)
        echo "=== Checking Local Test Environment Prerequisites ==="
        MISSING=0
        check_tool go "Go toolchain (>= 1.24)" || MISSING=1
        check_tool make "Make build tool" || MISSING=1
        check_tool node "Node.js (for frontend and syntax validation)" || MISSING=1
        check_tool git "Git version control" || MISSING=1
        
        if command -v pwsh >/dev/null 2>&1 || command -v powershell.exe >/dev/null 2>&1; then
            echo "  [OK] PowerShell environment"
        else
            echo "  [WARN] PowerShell not found; check-powershell target will fail"
        fi

        if command -v systemd-analyze >/dev/null 2>&1; then
            echo "  [OK] systemd-analyze available"
        else
            echo "  [INFO] systemd-analyze not available locally (verified in CI / Docker)"
        fi

        if [ "$MISSING" -eq 1 ]; then
            echo "ERROR: One or more required local tools are missing." >&2
            echo "Tip: Run '$0 docker' to execute the canonical gate in a container." >&2
            exit 1
        fi
        echo "Local environment is fully qualified."
        ;;
    docker)
        echo "=== Running Canonical Release Gate in Container ==="
        if ! command -v docker >/dev/null 2>&1; then
            echo "ERROR: Docker is required to run in container mode." >&2
            exit 1
        fi
        IMAGE_NAME="familyboard-test-env:local"
        docker build -f Dockerfile.test -t "$IMAGE_NAME" .
        docker run --rm -v "$(pwd):/workspace" "$IMAGE_NAME" make release-check
        ;;
    *)
        echo "Usage: $0 [check|docker]" >&2
        exit 1
        ;;
esac
