#!/bin/bash
# ==============================================================================
# FamilyBoard NVMe Porting & Migration Engine for Radxa Dragon
# Clones live running system (MicroSD / eMMC) directly onto M.2 NVMe SSD
# ==============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_ok() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_err() { echo -e "${RED}[ERROR]${NC} $1" >&2; }

if [ "$(id -u)" -ne 0 ]; then
  log_err "This migration tool must be run as root (use: sudo ./migrate-to-nvme.sh)"
  exit 1
fi

NON_INTERACTIVE=false
for arg in "$@"; do
  if [ "$arg" = "--non-interactive" ] || [ "$arg" = "-y" ] || [ "$arg" = "--yes" ]; then
    NON_INTERACTIVE=true
  fi
done

NVME_DEV="/dev/nvme0n1"
if [ ! -b "$NVME_DEV" ]; then
  log_err "No M.2 NVMe SSD found at $NVME_DEV. Please check hardware connection."
  exit 1
fi

# Check current root device
CURRENT_ROOT="$(findmnt -n -o SOURCE /)"
if [[ "$CURRENT_ROOT" == *"nvme"* ]]; then
  log_info "System is already running directly from NVMe SSD ($CURRENT_ROOT). No migration needed."
  exit 0
fi

log_info "=========================================================="
log_info "  FamilyBoard Radxa Dragon NVMe Migration Engine"
log_info "  Source Boot: $CURRENT_ROOT"
log_info "  Target NVMe: $NVME_DEV"
log_info "=========================================================="

if [ "$NON_INTERACTIVE" = false ]; then
  echo -ne "${YELLOW}WARNING: All existing data on $NVME_DEV will be formatted. Proceed? (y/N): ${NC}"
  read -r CONFIRM
  if [[ "$CONFIRM" != [yY] && "$CONFIRM" != [yY][eE][sS] ]]; then
    log_warn "Migration cancelled by user."
    exit 0
  fi
fi

# Ensure required utilities exist
for tool in parted mkfs.ext4 rsync blkid; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    apt-get update -y && apt-get install -y --no-install-recommends parted e2fsprogs rsync
    break
  fi
done

# 1. Unmount any active NVMe mounts
log_info "Unmounting any existing partitions on $NVME_DEV..."
umount "${NVME_DEV}"* 2>/dev/null || true

# 2. Partition NVMe (GPT partition table with single ext4 root partition)
log_info "Partitioning NVMe SSD ($NVME_DEV) with GPT..."
parted -s "$NVME_DEV" mklabel gpt
parted -s "$NVME_DEV" mkpart primary ext4 4MiB 100%
udevadm settle

TARGET_PART="${NVME_DEV}p1"
if [ ! -b "$TARGET_PART" ]; then
  TARGET_PART="${NVME_DEV}1"
fi

if [ ! -b "$TARGET_PART" ]; then
  log_err "Partition $TARGET_PART not found after partitioning."
  exit 1
fi

# 3. Format NVMe partition
log_info "Formatting $TARGET_PART as ext4 with journal..."
mkfs.ext4 -F -L "radxa-nvme" "$TARGET_PART"

# 4. Mount target
MOUNT_DIR="/mnt/familyboard-nvme-target"
mkdir -p "$MOUNT_DIR"
mount "$TARGET_PART" "$MOUNT_DIR"

cleanup() {
  if mountpoint -q "$MOUNT_DIR"; then
    umount -R "$MOUNT_DIR" || true
  fi
  rmdir "$MOUNT_DIR" 2>/dev/null || true
}
trap cleanup EXIT

# 5. Synchronize live system to NVMe
log_info "Cloning root filesystem and FamilyBoard database to NVMe (this takes 1-3 minutes)..."
rsync -axHAX --delete \
  --exclude=/lost+found \
  --exclude=/dev/* \
  --exclude=/proc/* \
  --exclude=/sys/* \
  --exclude=/tmp/* \
  --exclude=/run/* \
  --exclude=/mnt/* \
  --exclude=/media/* \
  / "$MOUNT_DIR/"

mkdir -p "$MOUNT_DIR"/{dev,proc,sys,tmp,run,mnt,media}
chmod 1777 "$MOUNT_DIR/tmp"

# 6. Update /etc/fstab on NVMe with new UUID
NEW_UUID="$(blkid -s UUID -o value "$TARGET_PART")"
log_info "Configuring NVMe rootfs UUID: $NEW_UUID in $MOUNT_DIR/etc/fstab..."

if [ -f "$MOUNT_DIR/etc/fstab" ]; then
  sed -i "s|UUID=[^ ]* \+/ \+|UUID=$NEW_UUID / |" "$MOUNT_DIR/etc/fstab" || true
  sed -i "s|^/dev/mmcblk0p[^ ]* \+/ \+|UUID=$NEW_UUID / |" "$MOUNT_DIR/etc/fstab" || true
fi

# 7. Update extlinux boot configuration if present
if [ -d "$MOUNT_DIR/boot/extlinux" ] && [ -f "$MOUNT_DIR/boot/extlinux/extlinux.conf" ]; then
  log_info "Updating extlinux boot entries to point to NVMe rootfs..."
  sed -i "s|root=UUID=[^ ]*|root=UUID=$NEW_UUID|g" "$MOUNT_DIR/boot/extlinux/extlinux.conf"
  sed -i "s|root=/dev/mmcblk0p[0-9]*|root=UUID=$NEW_UUID|g" "$MOUNT_DIR/boot/extlinux/extlinux.conf"
fi

# Sync disk caches
sync

log_ok "=========================================================="
log_ok "  NVMe Migration Complete!"
log_ok "  - Rootfs cloned to: $TARGET_PART (UUID: $NEW_UUID)"
log_ok "  - High-speed NVMe I/O ready"
log_ok "  You can now reboot. Remove the MicroSD card after shutdown"
log_ok "  to boot directly from the M.2 NVMe SSD at full PCIe speed."
log_ok "=========================================================="
