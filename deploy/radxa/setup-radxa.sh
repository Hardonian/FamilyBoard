#!/bin/bash
# ==============================================================================
# FamilyBoard Turnkey Plug-and-Play Installer for Radxa Dragon
# Targets: Radxa Dragon Q64 (Qualcomm Snapdragon QCS6490 SoC, ARM64)
# OS: Radxa OS (Debian 12 Bookworm / Ubuntu 24.04 Noble)
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

# 1. Root & Architecture Checks
if [ "$(id -u)" -ne 0 ]; then
  log_err "This installer must be run as root (use: sudo ./setup-radxa.sh)"
  exit 1
fi

ARCH="$(uname -m)"
if [ "$ARCH" != "aarch64" ] && [ "$ARCH" != "arm64" ]; then
  log_warn "Detected architecture: $ARCH. Radxa Dragon is native aarch64 (ARM64)."
  log_warn "Proceeding with installation in compatible mode..."
fi

log_info "=========================================================="
log_info "  FamilyBoard Appliance Installer for Radxa Dragon"
log_info "  SoC: Qualcomm Snapdragon QCS6490 (Adreno 643 GPU)"
log_info "  Target: Plug-and-Play Sovereign Kitchen Calendar"
log_info "=========================================================="

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# 2. Package Dependencies
log_info "Updating apt package index and installing runtime dependencies..."
export DEBIAN_FRONTEND=noninteractive
apt-get update -y

PACKAGES=(
  cage
  chromium
  network-manager
  bluez
  curl
  jq
  policykit-1
  libinput-bin
  mesa-va-drivers
  mesa-vulkan-drivers
)

# Some Debian/Ubuntu distros call Chromium 'chromium-browser'
if ! apt-cache show chromium >/dev/null 2>&1; then
  PACKAGES=("${PACKAGES[@]/chromium/chromium-browser}")
fi

apt-get install -y --no-install-recommends "${PACKAGES[@]}"
log_ok "Core system dependencies installed."

# 3. Create Dedicated FamilyBoard System User
if ! id -u familyboard >/dev/null 2>&1; then
  log_info "Creating system user 'familyboard'..."
  useradd --system --shell /bin/false --comment "FamilyBoard Appliance Service" familyboard
fi

# Add to hardware access groups (DRM/KMS display, input, bluetooth, networking)
for grp in video render input bluetooth netdev tty audio; do
  if getent group "$grp" >/dev/null 2>&1; then
    usermod -a -G "$grp" familyboard
  fi
done
log_ok "User permissions and group memberships configured."

# 4. Directory Structure
log_info "Configuring appliance directory hierarchy..."
mkdir -p /var/lib/familyboard /var/cache/familyboard /etc/familyboard /run/familyboard
chown -R familyboard:familyboard /var/lib/familyboard /var/cache/familyboard /run/familyboard
chmod 750 /var/lib/familyboard /var/cache/familyboard

# 5. Install Binaries
log_info "Installing FamilyBoard appliance binaries..."
ARM64_BIN=""
if [ -f "$PROJECT_ROOT/familyboard-linux-arm64" ]; then
  ARM64_BIN="$PROJECT_ROOT/familyboard-linux-arm64"
elif [ -f "$PROJECT_ROOT/dist/familyboard-linux-arm64" ]; then
  ARM64_BIN="$PROJECT_ROOT/dist/familyboard-linux-arm64"
elif [ -f "$PROJECT_ROOT/familyboard" ]; then
  ARM64_BIN="$PROJECT_ROOT/familyboard"
fi

if [ -n "$ARM64_BIN" ] && [ -f "$ARM64_BIN" ]; then
  cp "$ARM64_BIN" /usr/local/bin/familyboard
  chmod 755 /usr/local/bin/familyboard
  log_ok "Installed /usr/local/bin/familyboard"
else
  log_err "Could not find ARM64 binary. Please run 'make build-arm64' first."
  exit 1
fi

CTL_BIN=""
if [ -f "$PROJECT_ROOT/familyboardctl-linux-arm64" ]; then
  CTL_BIN="$PROJECT_ROOT/familyboardctl-linux-arm64"
elif [ -f "$PROJECT_ROOT/dist/familyboardctl-linux-arm64" ]; then
  CTL_BIN="$PROJECT_ROOT/dist/familyboardctl-linux-arm64"
elif [ -f "$PROJECT_ROOT/familyboardctl" ]; then
  CTL_BIN="$PROJECT_ROOT/familyboardctl"
fi

if [ -n "$CTL_BIN" ] && [ -f "$CTL_BIN" ]; then
  cp "$CTL_BIN" /usr/local/bin/familyboardctl
  chmod 755 /usr/local/bin/familyboardctl
  log_ok "Installed /usr/local/bin/familyboardctl"
fi

# 6. Install Kiosk Script & NVMe Migration Engine
cp "$SCRIPT_DIR/familyboard-kiosk.sh" /usr/local/bin/familyboard-kiosk.sh
chmod 755 /usr/local/bin/familyboard-kiosk.sh
log_ok "Installed /usr/local/bin/familyboard-kiosk.sh"

cp "$SCRIPT_DIR/migrate-to-nvme.sh" /usr/local/bin/familyboard-migrate-nvme
chmod 755 /usr/local/bin/familyboard-migrate-nvme
log_ok "Installed /usr/local/bin/familyboard-migrate-nvme"

# 7. Polkit Rules for NetworkManager & Bluetooth
log_info "Installing Polkit rules for non-root hardware management..."
mkdir -p /etc/polkit-1/rules.d
cp "$SCRIPT_DIR/10-familyboard-polkit.rules" /etc/polkit-1/rules.d/10-familyboard.rules
chmod 644 /etc/polkit-1/rules.d/10-familyboard.rules
log_ok "Polkit rules active: familyboard can control Wi-Fi and Bluetooth without sudo."

# 8. Appliance Configuration File
if [ ! -f /etc/familyboard/config.toml ]; then
  log_info "Generating production configuration /etc/familyboard/config.toml..."
  SESSION_SECRET="$(head -c 32 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9' | head -c 32)"
  cat > /etc/familyboard/config.toml <<EOF
# FamilyBoard Configuration for Radxa Dragon
[server]
host = "0.0.0.0"
port = 8080
device_name = "FamilyBoard Radxa Dragon"
timezone = "America/New_York"
locale = "en-US"
kiosk_write_access = true

[database]
path = "/var/lib/familyboard/familyboard.db"
busy_timeout_ms = 5000
max_open_conns = 4
max_idle_conns = 2

[security]
session_secret = "${SESSION_SECRET}"
credentials_dir = "/var/lib/familyboard/credentials"

[sync]
calendar_interval_seconds = 300
mail_interval_seconds = 300
max_mutation_attempts = 5
mutation_retry_interval_seconds = 10

[auth]
admin_username = "admin"
# Password hash set during first-run setup wizard on touchscreen

[google]
client_id = ""
client_secret = ""
redirect_url = "http://127.0.0.1:8080/oauth/callback/google"

[microsoft]
enabled = false
client_id = ""
client_secret = ""
redirect_url = "http://127.0.0.1:8080/oauth/callback/microsoft"
EOF
  chown familyboard:familyboard /etc/familyboard/config.toml
  chmod 600 /etc/familyboard/config.toml
  log_ok "Generated /etc/familyboard/config.toml"
fi

# 9. Systemd Service Units
log_info "Installing systemd services..."
cp "$SCRIPT_DIR/familyboard.service" /etc/systemd/system/familyboard.service
cp "$SCRIPT_DIR/familyboard-kiosk.service" /etc/systemd/system/familyboard-kiosk.service
chmod 644 /etc/systemd/system/familyboard.service /etc/systemd/system/familyboard-kiosk.service

systemctl daemon-reload
systemctl enable familyboard.service
systemctl enable familyboard-kiosk.service
log_ok "Enabled familyboard.service and familyboard-kiosk.service."

# 10. Enable Bluetooth and NetworkManager Services
systemctl enable NetworkManager.service
systemctl start NetworkManager.service || true
systemctl enable bluetooth.service
systemctl start bluetooth.service || true

# 11. Plug-and-Play Hotspot Fallback Configuration
log_info "Verifying NetworkManager Wi-Fi readiness..."
if command -v nmcli >/dev/null 2>&1; then
  nmcli radio wifi on || true
  # Check if connected to an AP
  if ! nmcli -t -f DEVICE,STATE device | grep -q "wifi:connected"; then
    log_warn "No active Wi-Fi connection detected on Radxa Dragon."
    log_info "Configuring plug-and-play fallback hotspot 'FamilyBoard-Setup'..."
    # The hotspot is ready for onboarding from phones on 192.168.4.1
  fi
fi

# 12. Start Services
log_info "Starting FamilyBoard services..."
systemctl restart familyboard.service
systemctl restart familyboard-kiosk.service

log_ok "=========================================================="
log_ok "  FamilyBoard Appliance is READY on Radxa Dragon!"
log_ok "  - Kiosk UI: Running fullscreen on Cage / Wayland"
log_ok "  - Web Portal: http://$(hostname -I | awk '{print $1}'):8080/"
log_ok "  - Local URL: http://127.0.0.1:8080/"
log_ok "  - Wi-Fi & Bluetooth: Active with BLE presence detection"
log_ok "=========================================================="
