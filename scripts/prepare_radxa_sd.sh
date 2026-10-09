#!/bin/bash
# ==============================================================================
# FamilyBoard Radxa Dragon SD Card Preparation Script (macOS / Linux)
# Prepares a standalone folder on your SD Card ready to run
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

TARGET_DIR="${1:-${PROJECT_ROOT}/dist/familyboard-sd-standalone}"

echo "=============================================================="
echo "   FamilyBoard Radxa Dragon SD Card Preparation Engine        "
echo "=============================================================="

# 1. Compile or verify ARM64 binaries
ARM64_BIN="${PROJECT_ROOT}/familyboard-linux-arm64"
CTL_ARM64_BIN="${PROJECT_ROOT}/familyboardctl-linux-arm64"

if [ ! -f "$ARM64_BIN" ]; then
  echo "[1/4] Compiling native ARM64 Linux binaries..."
  (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o familyboard-linux-arm64 ./cmd/familyboard)
  (cd "$PROJECT_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o familyboardctl-linux-arm64 ./cmd/familyboardctl)
else
  echo "[1/4] Found precompiled ARM64 Linux binaries."
fi

# 2. Target directory
echo "[2/4] Preparing standalone folder at: $TARGET_DIR"
mkdir -p "$TARGET_DIR" "$TARGET_DIR/photos" "$TARGET_DIR/data" "$TARGET_DIR/credentials"

# 3. Copy files
echo "[3/4] Copying binaries, configurations, and deployment tools..."
cp -f "$ARM64_BIN" "$TARGET_DIR/familyboard"
if [ -f "$CTL_ARM64_BIN" ]; then
  cp -f "$CTL_ARM64_BIN" "$TARGET_DIR/familyboardctl"
fi

cp -f "$PROJECT_ROOT/config.example.toml" "$TARGET_DIR/config.toml"
cp -f "$PROJECT_ROOT/deploy/radxa/run-standalone.sh" "$TARGET_DIR/run-standalone.sh"
cp -f "$PROJECT_ROOT/deploy/radxa/setup-radxa.sh" "$TARGET_DIR/install-appliance.sh"
cp -f "$PROJECT_ROOT/deploy/radxa/migrate-to-nvme.sh" "$TARGET_DIR/migrate-to-nvme.sh"
cp -f "$PROJECT_ROOT/deploy/radxa/familyboard-kiosk.sh" "$TARGET_DIR/familyboard-kiosk.sh"
cp -f "$PROJECT_ROOT/deploy/radxa/familyboard.service" "$TARGET_DIR/familyboard.service"
cp -f "$PROJECT_ROOT/deploy/radxa/familyboard-kiosk.service" "$TARGET_DIR/familyboard-kiosk.service"
cp -f "$PROJECT_ROOT/deploy/radxa/10-familyboard-polkit.rules" "$TARGET_DIR/10-familyboard-polkit.rules"
chmod +x "$TARGET_DIR"/*.sh "$TARGET_DIR/familyboard" "${TARGET_DIR}/familyboardctl" 2>/dev/null || true

cat > "$TARGET_DIR/README.txt" << 'EOF'
# FamilyBoard Standalone on Radxa Dragon

This folder contains the complete, self-contained FamilyBoard appliance.

## Option A: Run Directly from SD Card (Portable, Zero-Install)
In terminal on the Radxa Dragon:
    cd /path/to/familyboard
    chmod +x *.sh familyboard familyboardctl
    ./run-standalone.sh

## Option B: Install as Full Appliance (Fullscreen Kiosk on Boot)
To make FamilyBoard start automatically on boot and replace desktop:
    sudo ./install-appliance.sh

## Option C: Port from SD Card to High-Speed M.2 NVMe SSD
Once running on the Radxa Dragon with an M.2 NVMe SSD installed:
    sudo ./migrate-to-nvme.sh
This automatically formats the NVMe SSD, clones the system, and sets up direct boot!
EOF

echo "[4/4] Complete!"
echo "=============================================================="
echo "  FamilyBoard standalone folder is ready at: $TARGET_DIR"
echo "  Just cut and paste this folder onto your SD card or USB drive."
echo "=============================================================="
