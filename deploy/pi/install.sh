#!/bin/bash
# FamilyBoard Appliance Installation Script
# Idempotently installs application binaries, systemd service, and kiosk autostart

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== FamilyBoard Application Installer ==="

# 1. Verify root privileges
if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: This script must be run as root (use sudo)." >&2
  exit 1
fi

# 2. Create system user and kiosk user if not already present
if ! id "familyboard" >/dev/null 2>&1; then
  echo "Creating service user 'familyboard'..."
  useradd --system --shell /usr/sbin/nologin --home-dir /var/lib/familyboard familyboard
fi

if ! id "familyboard-kiosk" >/dev/null 2>&1; then
  echo "Creating kiosk user 'familyboard-kiosk'..."
  useradd --create-home --shell /bin/bash --groups video,input,audio familyboard-kiosk || true
fi

# 3. Create required directory tree with least-privilege permissions
echo "Configuring directory layout..."
mkdir -p /usr/local/bin
mkdir -p /etc/familyboard/credentials
mkdir -p /var/lib/familyboard/backups
mkdir -p /var/lib/familyboard/bootstrap
mkdir -p /var/cache/familyboard
mkdir -p /run/familyboard

chmod 0755 /usr/local/bin
chown -R root:familyboard /etc/familyboard
chmod 0750 /etc/familyboard
chmod 0700 /etc/familyboard/credentials
chown -R familyboard:familyboard /etc/familyboard/credentials

chown -R familyboard:familyboard /var/lib/familyboard
chmod 0750 /var/lib/familyboard
chmod 0700 /var/lib/familyboard/backups

chown -R familyboard:familyboard /var/cache/familyboard
chmod 0750 /var/cache/familyboard

# 4. Install Binaries
echo "Installing FamilyBoard binaries..."
ARCH=$(uname -m)
SRC_FB=""
SRC_CTL=""

if [ "$ARCH" = "aarch64" ] && [ -f "$REPO_ROOT/familyboard-linux-arm64" ]; then
  SRC_FB="$REPO_ROOT/familyboard-linux-arm64"
  SRC_CTL="$REPO_ROOT/familyboardctl-linux-arm64"
elif [ -f "$REPO_ROOT/familyboard" ]; then
  SRC_FB="$REPO_ROOT/familyboard"
  SRC_CTL="$REPO_ROOT/familyboardctl"
fi

if [ -n "$SRC_FB" ] && [ -f "$SRC_FB" ]; then
  install -m 0755 "$SRC_FB" /usr/local/bin/familyboard
  install -m 0755 "$SRC_CTL" /usr/local/bin/familyboardctl
  echo "Installed /usr/local/bin/familyboard and familyboardctl"
else
  echo "WARNING: Pre-compiled binaries not found. Building locally..."
  if command -v go >/dev/null 2>&1; then
    (cd "$REPO_ROOT" && go build -o /usr/local/bin/familyboard ./cmd/familyboard)
    (cd "$REPO_ROOT" && go build -o /usr/local/bin/familyboardctl ./cmd/familyboardctl)
    chmod 0755 /usr/local/bin/familyboard /usr/local/bin/familyboardctl
  else
    echo "ERROR: Go toolchain not found and no pre-built binaries available." >&2
    exit 1
  fi
fi

# 5. Install default configuration if none exists
if [ ! -f /etc/familyboard/config.toml ]; then
  echo "Installing default configuration to /etc/familyboard/config.toml..."
  cp "$REPO_ROOT/config.example.toml" /etc/familyboard/config.toml
  chown root:familyboard /etc/familyboard/config.toml
  chmod 0640 /etc/familyboard/config.toml
fi

# 6. Install and enable systemd service
echo "Installing systemd service..."
install -m 0644 "$SCRIPT_DIR/systemd/familyboard.service" /etc/systemd/system/familyboard.service
systemctl daemon-reload
systemctl enable familyboard.service
systemctl restart familyboard.service

# 7. Configure Chromium Kiosk Autostart (Idempotent Managed Block)
echo "Configuring kiosk autostart across desktop environments..."
install -m 0755 "$SCRIPT_DIR/kiosk/familyboard-kiosk.sh" /usr/local/bin/familyboard-kiosk.sh

MANAGED_START="# BEGIN FAMILYBOARD MANAGED KIOSK"
MANAGED_END="# END FAMILYBOARD MANAGED KIOSK"
MANAGED_BLOCK="$MANAGED_START
/usr/local/bin/familyboard-kiosk.sh &
$MANAGED_END"

update_autostart_file() {
  local target="$1"
  mkdir -p "$(dirname "$target")"
  touch "$target"

  if grep -q "$MANAGED_START" "$target"; then
    # Replace existing managed block
    sed -i "/$MANAGED_START/,/$MANAGED_END/d" "$target"
  fi
  echo "$MANAGED_BLOCK" >> "$target"
}

# Collect target users (dedicated kiosk user, invoking sudo user, and standard pi user)
TARGET_USERS=("familyboard-kiosk")
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
  TARGET_USERS+=("$SUDO_USER")
fi
if [ -d "/home/pi" ] && [[ ! " ${TARGET_USERS[*]} " =~ " pi " ]]; then
  TARGET_USERS+=("pi")
fi

for u in "${TARGET_USERS[@]}"; do
  user_home=$(getent passwd "$u" 2>/dev/null | cut -d: -f6 || echo "")
  if [ -n "$user_home" ] && [ -d "$user_home" ]; then
    # Wayland (Debian 12 Bookworm / labwc)
    update_autostart_file "$user_home/.config/labwc/autostart"
    # X11 (Debian 11 Bullseye / LXDE-pi)
    update_autostart_file "$user_home/.config/lxsession/LXDE-pi/autostart"
    # Openbox / lightweight window managers
    update_autostart_file "$user_home/.config/openbox/autostart"
    chown -R "$u:$u" "$user_home/.config" 2>/dev/null || true
  fi
done

echo "=== FamilyBoard Installation Complete ==="
