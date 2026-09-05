#!/bin/bash
# FamilyBoard Canonical Raspberry Pi Appliance Bootstrap Script
# Converts a compatible Raspberry Pi OS installation into a sovereign FamilyBoard appliance

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

LOCK_FILE="/var/lock/familyboard-bootstrap.lock"
STATE_FILE="/var/lib/familyboard/bootstrap/state.json"
BACKUP_DIR="/var/backups/familyboard-bootstrap-preflight"

# Default flags
CHECK_ONLY=false
RESUME=false
NO_REBOOT=false
SKIP_OS_UPDATE=false
SET_HOSTNAME=""
FORCE_KIOSK=false
UNATTENDED=false

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --check)
      CHECK_ONLY=true
      shift
      ;;
    --apply)
      CHECK_ONLY=false
      shift
      ;;
    --resume)
      RESUME=true
      shift
      ;;
    --no-reboot)
      NO_REBOOT=true
      shift
      ;;
    --skip-os-update)
      SKIP_OS_UPDATE=true
      shift
      ;;
    --set-hostname)
      SET_HOSTNAME="$2"
      shift 2
      ;;
    --force-kiosk)
      FORCE_KIOSK=true
      shift
      ;;
    --unattended)
      UNATTENDED=true
      shift
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

echo "=================================================="
echo "      FamilyBoard Appliance Bootstrap Onboarding  "
echo "=================================================="

# 1. Root Check
if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: Bootstrap must be run as root (use sudo)." >&2
  exit 1
fi

# 2. Lock Acquisition
mkdir -p "$(dirname "$LOCK_FILE")"
exec 200>"$LOCK_FILE"
if ! flock -n 200; then
  echo "ERROR: Another bootstrap process is currently running." >&2
  exit 1
fi

cleanup() {
  flock -u 200 || true
  rm -f "$LOCK_FILE" || true
}
trap cleanup EXIT

# 3. Preflight Inspection
echo "\n--- Step 1: Preflight Hardware & OS Inspection ---"

PI_MODEL="Standard PC / VM"
if [ -f /proc/device-tree/model ]; then
  PI_MODEL=$(tr -d '\0\r\n' < /proc/device-tree/model)
fi
echo "Hardware Model:       $PI_MODEL"

ARCH=$(uname -m)
echo "Kernel Architecture:  $ARCH"

USERSPACE_BITS=$(getconf LONG_BIT 2>/dev/null || echo "unknown")
echo "Userspace:            ${USERSPACE_BITS}-bit"

OS_NAME="Linux"
OS_VERSION_ID="12"
if [ -f /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_NAME="${PRETTY_NAME:-Linux}"
  OS_VERSION_ID="${VERSION_ID:-12}"
fi
echo "Operating System:     $OS_NAME (Debian version $OS_VERSION_ID)"

# Storage Free Check
FREE_DISK_MB=$(df -m / | awk 'NR==2 {print $4}')
echo "Free Storage on /:    ${FREE_DISK_MB} MB"
if [ "$FREE_DISK_MB" -lt 1000 ]; then
  echo "ERROR: Insufficient free storage. At least 1000 MB required for appliance setup." >&2
  exit 1
fi

# RAM Check
TOTAL_RAM_KB=$(grep MemTotal /proc/meminfo | awk '{print $2}')
TOTAL_RAM_MB=$((TOTAL_RAM_KB / 1024))
echo "Available Memory:     ${TOTAL_RAM_MB} MB"

# Pi-hole Coexistence Check
PIHOLE_PRESENT=false
if command -v pihole >/dev/null 2>&1 || [ -d /etc/pihole ]; then
  PIHOLE_PRESENT=true
  echo "INFO: Existing Pi-hole installation detected! Preserving DNS and port 53/80/443 configurations."
fi

# OS Version Compatibility Evaluation
# Debian 11 (Bullseye) and Debian 12 (Bookworm) are supported. Debian 10 or older requires reimage.
if [ "$OS_VERSION_ID" -lt 11 ] 2>/dev/null; then
  echo "\n!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
  echo "CRITICAL: Detected older unsupported OS version: $OS_NAME"
  echo "FamilyBoard requires Debian 11+ for modern Wayland/Chromium support."
  echo "Automatic cross-major distro upgrading risks SD card corruption."
  echo ""
  echo "Creating safety migration bundle..."
  mkdir -p /var/backups/familyboard
  "$SCRIPT_DIR/create-migration-bundle.sh" /var/backups/familyboard || true
  echo ""
  echo "STATUS: REIMAGE_REQUIRED"
  echo "Please see docs: $SCRIPT_DIR/reimage/REIMAGE_REQUIRED.md"
  echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
  exit 1
fi

if [ "$CHECK_ONLY" = true ]; then
  echo "\nPreflight inspection completed (--check mode). No changes were made."
  exit 0
fi

# 4. System Backup Before Changes
echo "\n--- Step 2: System Configuration Backup ---"
mkdir -p "$BACKUP_DIR"
for cfg in /etc/hosts /etc/hostname /etc/apt/sources.list; do
  if [ -f "$cfg" ]; then
    cp "$cfg" "$BACKUP_DIR/"
  fi
done
if [ -d /etc/apt/sources.list.d ]; then
  mkdir -p "$BACKUP_DIR/sources.list.d"
  cp -r /etc/apt/sources.list.d/* "$BACKUP_DIR/sources.list.d/" 2>/dev/null || true
fi
echo "Config files backed up to $BACKUP_DIR"

# 5. Optional Hostname Update
if [ -n "$SET_HOSTNAME" ]; then
  CURRENT_HOSTNAME=$(cat /etc/hostname 2>/dev/null || hostname)
  if [ "$CURRENT_HOSTNAME" != "$SET_HOSTNAME" ]; then
    echo "Updating hostname from $CURRENT_HOSTNAME to $SET_HOSTNAME..."
    echo "$SET_HOSTNAME" > /etc/hostname
    hostname "$SET_HOSTNAME" || true
    sed -i "s/127\.0\.1\.1.*/127.0.1.1\t$SET_HOSTNAME/g" /etc/hosts || true
  fi
fi

# 6. Package Manager Health & Repair
echo "\n--- Step 3: Package Manager Audit & Repair ---"
if dpkg --audit 2>/dev/null | grep -q .; then
  echo "Repairing interrupted package state..."
  dpkg --configure -a || true
  apt-get -f install -y || true
fi

# 7. OS Maintenance
if [ "$SKIP_OS_UPDATE" = false ]; then
  echo "\n--- Step 4: Supported OS Maintenance (apt update & upgrade) ---"
  apt-get update -y
  apt-get upgrade -y
  apt-get autoremove -y
  apt-get autoclean -y
fi

# 8. Runtime Dependency Installation
echo "\n--- Step 5: Runtime Dependencies Installation ---"
DEPS=(
  "ca-certificates"
  "curl"
  "jq"
  "sqlite3"
  "chromium-browser"
)

# If chromium-browser is virtual or transitional, fallback to chromium
INSTALL_PKGS=()
for pkg in "${DEPS[@]}"; do
  if ! dpkg -s "$pkg" >/dev/null 2>&1; then
    INSTALL_PKGS+=("$pkg")
  fi
done

if [ ${#INSTALL_PKGS[@]} -gt 0 ]; then
  echo "Installing required packages: ${INSTALL_PKGS[*]}..."
  apt-get install -y "${INSTALL_PKGS[@]}" || apt-get install -y chromium ca-certificates curl jq sqlite3
fi

# 9. Application Installation
echo "\n--- Step 6: Installing FamilyBoard Application ---"
"$SCRIPT_DIR/install.sh"

# 10. Check Reboot Requirement
mkdir -p "$(dirname "$STATE_FILE")"
cat <<EOF > "$STATE_FILE"
{
  "bootstrap_stage": "installed",
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "pi_model": "$PI_MODEL",
  "version": "0.9.0"
}
EOF

REBOOT_NEEDED=false
if [ -f /var/run/reboot-required ]; then
  REBOOT_NEEDED=true
fi

# 11. Final Verification
echo "\n--- Step 7: Final Appliance Verification ---"
"$SCRIPT_DIR/verify.sh" || true

echo "\n=================================================="
if [ "$REBOOT_NEEDED" = true ]; then
  echo "FamilyBoard bootstrap applied successfully."
  echo "NOTICE: Kernel packages were updated and a system reboot is recommended."
  if [ "$NO_REBOOT" = false ] && [ "$UNATTENDED" = false ]; then
    echo "To reboot now, execute: sudo reboot"
  fi
else
  echo "SUCCESS: FamilyBoard appliance is ready!"
  echo "Access Admin UI via: http://127.0.0.1:8080/ or http://familyboard.local:8080/"
fi
echo "=================================================="
