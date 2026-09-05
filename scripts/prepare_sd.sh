#!/bin/bash
# ==============================================================================
# FamilyBoard MicroSD Turnkey Preparation Engine (Linux / macOS)
# Prepares a flashed Raspberry Pi MicroSD card for offline FamilyBoard deployment.
# Because Hardonian/FamilyBoard is a PRIVATE repository, copying the release
# package directly from your PC to the MicroSD card allows 100% offline,
# zero-token, zero-credential installation on the Raspberry Pi.
# ==============================================================================

set -Eeuo pipefail

if [ $# -lt 1 ]; then
  echo "Usage: $0 <path-to-mounted-sd-card-bootfs> [--no-autorun]"
  echo "Example (macOS): $0 /Volumes/bootfs"
  echo "Example (Linux): $0 /media/$USER/bootfs"
  exit 1
fi

MOUNT_POINT="$1"
NO_AUTORUN=false

if [ "${2:-}" = "--no-autorun" ]; then
  NO_AUTORUN=true
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "=============================================================="
echo "      FamilyBoard MicroSD Turnkey Preparation Engine          "
echo "      (Private Repository: Zero-Token Offline Deployment)     "
echo "=============================================================="

if [ ! -d "$MOUNT_POINT" ]; then
  echo "ERROR: Mount point '$MOUNT_POINT' does not exist." >&2
  exit 1
fi

SETUP_SCRIPT="${PROJECT_ROOT}/familyboard-setup.sh"
if [ ! -f "$SETUP_SCRIPT" ]; then
  SETUP_SCRIPT="${PROJECT_ROOT}/deploy/pi/familyboard-setup.sh"
fi

if [ ! -f "$SETUP_SCRIPT" ]; then
  echo "ERROR: Could not find familyboard-setup.sh in $PROJECT_ROOT" >&2
  exit 1
fi

# Locate release package in dist/
DIST_DIR="${PROJECT_ROOT}/dist"
TARBALL_PATH=$(ls -t "${DIST_DIR}"/familyboard-*-linux-arm64.tar.gz 2>/dev/null | head -n 1 || echo "")

if [ -z "$TARBALL_PATH" ]; then
  echo "Release package not found. Building release package..."
  (cd "$PROJECT_ROOT" && make package-arm64)
  TARBALL_PATH=$(ls -t "${DIST_DIR}"/familyboard-*-linux-arm64.tar.gz 2>/dev/null | head -n 1 || echo "")
fi

if [ -z "$TARBALL_PATH" ] || [ ! -f "$TARBALL_PATH" ]; then
  echo "ERROR: Failed to find or build release tarball in $DIST_DIR" >&2
  exit 1
fi

TARBALL_NAME="$(basename "$TARBALL_PATH")"

echo "Target MicroSD Boot:  ${MOUNT_POINT}"
echo "Installer Script:     ${SETUP_SCRIPT}"
echo "Release Tarball:      ${TARBALL_PATH}"

echo ""
echo "[1/3] Copying setup script to ${MOUNT_POINT}/familyboard-setup.sh..."
cp -f "$SETUP_SCRIPT" "${MOUNT_POINT}/familyboard-setup.sh"

echo "[2/3] Copying ${TARBALL_NAME} to ${MOUNT_POINT}/${TARBALL_NAME}..."
cp -f "$TARBALL_PATH" "${MOUNT_POINT}/${TARBALL_NAME}"

CMDLINE_TXT="${MOUNT_POINT}/cmdline.txt"
if [ "$NO_AUTORUN" = false ] && [ -f "$CMDLINE_TXT" ]; then
  echo "[3/3] Configuring cmdline.txt for automatic first-boot takeover..."
  if ! grep -q "systemd.run=" "$CMDLINE_TXT"; then
    AUTORUN_HOOK="systemd.run=/boot/firmware/familyboard-setup.sh systemd.run_success_action=reboot"
    # Ensure appended on single line
    sed -i -e "s/\r$//" "$CMDLINE_TXT" 2>/dev/null || true
    sed -i "1s/$/ ${AUTORUN_HOOK}/" "$CMDLINE_TXT"
    echo "      Added auto-run hook to cmdline.txt."
  else
    echo "      cmdline.txt already contains systemd.run hook."
  fi
else
  echo "[3/3] Skipping cmdline.txt modification."
fi

echo ""
echo "=============================================================="
echo "  SUCCESS! MicroSD card is ready for FamilyBoard Appliance.  "
echo "=============================================================="
echo "Next steps:"
echo "  1. Unmount/eject the MicroSD card from your PC."
echo "  2. Insert the card into your Raspberry Pi and connect HDMI & power."
echo "  3. The Pi boots, installs FamilyBoard offline without requiring"
echo "     any GitHub tokens or credentials, and reboots directly into kiosk!"
echo ""
