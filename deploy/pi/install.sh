#!/bin/bash
# FamilyBoard Appliance Installation Script
# Idempotently installs application binaries, systemd service, and kiosk autostart

set -Eeuo pipefail
umask 027

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOCK_FILE="/run/lock/familyboard-install.lock"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

on_error() {
  local exit_code=$?
  echo "ERROR: Installation failed at line ${BASH_LINENO[0]} (exit ${exit_code})." >&2
  exit "$exit_code"
}
trap on_error ERR

echo "=== FamilyBoard Application Installer ==="

# 1. Verify root privileges
if [ "$(id -u)" -ne 0 ]; then
  die "This script must be run as root (use sudo)."
fi

command -v systemctl >/dev/null 2>&1 || die "systemd is required for appliance installation."
command -v flock >/dev/null 2>&1 || die "flock is required (install the util-linux package)."
install -d -m 0755 "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
flock -n 9 || die "Another FamilyBoard install or update is already running."

# 2. Create system user and kiosk user if not already present
if ! id "familyboard" >/dev/null 2>&1; then
  echo "Creating service user 'familyboard'..."
  useradd --system --shell /usr/sbin/nologin --home-dir /var/lib/familyboard familyboard
fi

if ! id "familyboard-kiosk" >/dev/null 2>&1; then
  echo "Creating kiosk user 'familyboard-kiosk'..."
  KIOSK_GROUPS=()
  for group_name in video input audio render; do
    if getent group "$group_name" >/dev/null 2>&1; then
      KIOSK_GROUPS+=("$group_name")
    fi
  done
  if [ ${#KIOSK_GROUPS[@]} -gt 0 ]; then
    KIOSK_GROUP_LIST=$(IFS=,; echo "${KIOSK_GROUPS[*]}")
    useradd --create-home --shell /bin/bash --groups "$KIOSK_GROUP_LIST" familyboard-kiosk
  else
    useradd --create-home --shell /bin/bash familyboard-kiosk
  fi
fi

# 3. Create required directory tree with least-privilege permissions
echo "Configuring directory layout..."
install -d -m 0755 -o root -g root /usr/local/bin /usr/local/libexec/familyboard
# The daemon atomically rewrites config.toml when secrets and admin settings
# change, so its group needs directory write access (the files remain private).
install -d -m 0770 -o root -g familyboard /etc/familyboard
install -d -m 0750 -o familyboard -g familyboard /var/lib/familyboard /var/cache/familyboard
install -d -m 0700 -o familyboard -g familyboard \
  /var/lib/familyboard/backups \
  /var/lib/familyboard/bootstrap \
  /var/lib/familyboard/credentials
install -d -m 0750 -o familyboard -g familyboard /run/familyboard

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

if [ -n "$SRC_FB" ] && [ -f "$SRC_FB" ] && [ -n "$SRC_CTL" ] && [ -f "$SRC_CTL" ]; then
  install -m 0755 "$SRC_FB" /usr/local/bin/.familyboard.new
  install -m 0755 "$SRC_CTL" /usr/local/bin/.familyboardctl.new
  mv -f /usr/local/bin/.familyboard.new /usr/local/bin/familyboard
  mv -f /usr/local/bin/.familyboardctl.new /usr/local/bin/familyboardctl
  echo "Installed /usr/local/bin/familyboard and familyboardctl"
else
  echo "WARNING: Pre-compiled binaries not found. Building locally..."
  if command -v go >/dev/null 2>&1; then
    (cd "$REPO_ROOT" && go build -o /usr/local/bin/.familyboard.new ./cmd/familyboard)
    (cd "$REPO_ROOT" && go build -o /usr/local/bin/.familyboardctl.new ./cmd/familyboardctl)
    chmod 0755 /usr/local/bin/.familyboard.new /usr/local/bin/.familyboardctl.new
    mv -f /usr/local/bin/.familyboard.new /usr/local/bin/familyboard
    mv -f /usr/local/bin/.familyboardctl.new /usr/local/bin/familyboardctl
  else
    die "Go toolchain not found and no complete pre-built binary pair is available."
  fi
fi

/usr/local/bin/familyboard --version >/dev/null 2>&1 || die "Installed familyboard binary cannot execute on $(uname -m)."
/usr/local/bin/familyboardctl version >/dev/null 2>&1 || die "Installed familyboardctl binary cannot execute on $(uname -m)."

# 5. Install default configuration if none exists
if [ ! -f /etc/familyboard/config.toml ]; then
  echo "Installing default configuration to /etc/familyboard/config.toml..."
  [ -f "$REPO_ROOT/config.example.toml" ] || die "Missing config.example.toml in release bundle."
  install -m 0640 -o root -g familyboard "$REPO_ROOT/config.example.toml" /etc/familyboard/config.toml
  sed -i \
    -e 's|^host = "127.0.0.1"$|host = "0.0.0.0"|' \
    -e 's|^path = "familyboard.db"$|path = "/var/lib/familyboard/familyboard.db"|' \
    -e 's|^credentials_dir = "credentials"$|credentials_dir = "/var/lib/familyboard/credentials"|' \
    /etc/familyboard/config.toml
fi

# Existing installations may predate the writable-directory fix.
chown root:familyboard /etc/familyboard
chmod 0770 /etc/familyboard
chown familyboard:familyboard /var/lib/familyboard/credentials
chmod 0700 /var/lib/familyboard/credentials

# 6. Install and enable systemd service
echo "Installing systemd service..."
install -m 0644 "$SCRIPT_DIR/systemd/familyboard.service" /etc/systemd/system/familyboard.service
install -m 0755 "$SCRIPT_DIR/create-migration-bundle.sh" /usr/local/libexec/familyboard/create-migration-bundle
install -m 0755 "$SCRIPT_DIR/restore-migration-bundle.sh" /usr/local/libexec/familyboard/restore-migration-bundle
install -m 0644 "$SCRIPT_DIR/systemd/familyboard-backup.service" /etc/systemd/system/familyboard-backup.service
install -m 0644 "$SCRIPT_DIR/systemd/familyboard-backup.timer" /etc/systemd/system/familyboard-backup.timer
systemctl daemon-reload
systemctl enable familyboard.service
systemctl restart familyboard.service
systemctl enable --now familyboard-backup.timer

echo "Waiting for the daemon readiness check..."
SERVICE_READY=false
for _ in $(seq 1 30); do
  if command -v curl >/dev/null 2>&1; then
    if curl --fail --silent --show-error --max-time 2 http://127.0.0.1:8080/health/ready >/dev/null 2>&1; then
      SERVICE_READY=true
      break
    fi
  elif systemctl is-active --quiet familyboard.service; then
    SERVICE_READY=true
    break
  fi
  sleep 1
done
if [ "$SERVICE_READY" != true ]; then
  systemctl status familyboard.service --no-pager >&2 || true
  journalctl -u familyboard.service -n 50 --no-pager >&2 || true
  die "familyboard.service did not become ready within 30 seconds."
fi

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
  printf '%s\n' "$MANAGED_BLOCK" >> "$target"
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
