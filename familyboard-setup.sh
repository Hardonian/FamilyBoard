#!/bin/bash
# ==============================================================================
# FamilyBoard Appliance Turnkey MicroSD & One-Shot Setup Script
# Converts a Raspberry Pi into a dedicated, sovereign kitchen calendar kiosk.
#
# Usage:
#   A) From MicroSD root on Pi:
#      sudo bash /boot/firmware/familyboard-setup.sh   # (Raspberry Pi OS 12 Bookworm)
#      sudo bash /boot/familyboard-setup.sh            # (Raspberry Pi OS 11 Bullseye)
#
#   B) Online One-Shot Command:
#      curl -sSL https://raw.githubusercontent.com/Hardonian/FamilyBoard/main/familyboard-setup.sh | sudo bash
#
#   C) Optional Flags:
#      --no-reboot        Do not reboot after setup completes
#      --offline          Require local tarball on microSD (no internet download)
# ==============================================================================

set -Eeuo pipefail

VERSION="${FAMILYBOARD_VERSION:-1.5.0}"
RELEASE_TARBALL_NAME="familyboard-${VERSION}-linux-arm64.tar.gz"
GITHUB_REPO="Hardonian/FamilyBoard"
RELEASE_URL="https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/${RELEASE_TARBALL_NAME}"

NO_REBOOT=false
OFFLINE_ONLY=false
CUSTOM_TARBALL=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-reboot)
      NO_REBOOT=true
      shift
      ;;
    --offline)
      OFFLINE_ONLY=true
      shift
      ;;
    --tarball)
      CUSTOM_TARBALL="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

echo "=============================================================="
echo "          FamilyBoard Sovereign Appliance Setup               "
echo "        Turnkey Household Kiosk Deployment Engine             "
echo "=============================================================="

# 1. Root check
if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: This installer must be executed as root." >&2
  echo "Please run: sudo bash $0" >&2
  exit 1
fi

# 2. Identify Boot Partition Paths (Bookworm vs Bullseye)
BOOT_DIR="/boot"
if [ -d "/boot/firmware" ]; then
  BOOT_DIR="/boot/firmware"
fi
CMDLINE_TXT="${BOOT_DIR}/cmdline.txt"
CONFIG_TXT="${BOOT_DIR}/config.txt"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || echo "")"

echo "Boot partition detected at: ${BOOT_DIR}"

# 3. Architecture & OS Verification
ARCH=$(uname -m)
echo "System architecture:        ${ARCH}"

OS_ID="unknown"
OS_VERSION_ID="12"
if [ -f /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-linux}"
  OS_VERSION_ID="${VERSION_ID:-12}"
fi
echo "Operating system:           ${PRETTY_NAME:-Linux} (Debian ${OS_VERSION_ID})"

if [ "${OS_VERSION_ID}" -lt 11 ] 2>/dev/null; then
  echo "ERROR: Raspberry Pi OS 11 (Bullseye) or 12 (Bookworm) 64-bit is required." >&2
  exit 1
fi

# 4. Check for Pi-hole coexistence
if command -v pihole >/dev/null 2>&1 || [ -d /etc/pihole ]; then
  echo "INFO: Detected existing Pi-hole installation. Preserving DNS (ports 53/80/443)."
  echo "INFO: FamilyBoard will operate safely on port 8080 without conflict."
fi

# 5. Locate or Retrieve Release Tarball
echo ""
echo "--- Step 1: Locating FamilyBoard Appliance Package ---"
TARBALL_PATH=""

# Search candidates in order of priority:
SEARCH_PATHS=(
  "${CUSTOM_TARBALL}"
  "${SCRIPT_DIR}/${RELEASE_TARBALL_NAME}"
  "${BOOT_DIR}/${RELEASE_TARBALL_NAME}"
  "/boot/firmware/${RELEASE_TARBALL_NAME}"
  "/boot/${RELEASE_TARBALL_NAME}"
  "/var/tmp/${RELEASE_TARBALL_NAME}"
  "/tmp/${RELEASE_TARBALL_NAME}"
)

for p in "${SEARCH_PATHS[@]}"; do
  if [ -n "$p" ] && [ -f "$p" ]; then
    TARBALL_PATH="$p"
    echo "Found local microSD release package: ${TARBALL_PATH}"
    break
  fi
done

if [ -z "$TARBALL_PATH" ]; then
  if [ "$OFFLINE_ONLY" = true ]; then
    echo "ERROR: --offline was specified but ${RELEASE_TARBALL_NAME} was not found on microSD." >&2
    exit 1
  fi

  AUTH_HEADER=()
  TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
  if [ -n "$TOKEN" ]; then
    AUTH_HEADER=(-H "Authorization: Bearer $TOKEN")
  fi

  echo "Attempting to retrieve release package from GitHub..."
  TMP_DL="/tmp/${RELEASE_TARBALL_NAME}"
  if command -v curl >/dev/null 2>&1; then
    curl -fSL "${AUTH_HEADER[@]}" -o "$TMP_DL" "$RELEASE_URL" || true
  elif command -v wget >/dev/null 2>&1; then
    if [ -n "$TOKEN" ]; then
      wget --header="Authorization: Bearer $TOKEN" -q -O "$TMP_DL" "$RELEASE_URL" || true
    else
      wget -q -O "$TMP_DL" "$RELEASE_URL" || true
    fi
  fi

  if [ -f "$TMP_DL" ] && [ -s "$TMP_DL" ]; then
    TARBALL_PATH="$TMP_DL"
    echo "Successfully downloaded: ${TARBALL_PATH}"
  else
    # Fallback search across local repository clone paths
    for repo_candidate in \
      "${SCRIPT_DIR}/dist/${RELEASE_TARBALL_NAME}" \
      "${SCRIPT_DIR}/../../dist/${RELEASE_TARBALL_NAME}" \
      "/home/*/${RELEASE_TARBALL_NAME}" \
      "/home/*/FamilyBoard/dist/${RELEASE_TARBALL_NAME}"; do
      # shellcheck disable=SC2086
      match=$(compgen -G "$repo_candidate" 2>/dev/null | head -n 1 || echo "")
      if [ -n "$match" ] && [ -f "$match" ]; then
        TARBALL_PATH="$match"
        echo "Found local release package: ${TARBALL_PATH}"
        break
      fi
    done

    if [ -z "$TARBALL_PATH" ]; then
      echo "" >&2
      echo "==============================================================" >&2
      echo "  PRIVATE REPOSITORY NOTICE: Zero-Token MicroSD Setup          " >&2
      echo "==============================================================" >&2
      echo "Because 'Hardonian/FamilyBoard' is a private repository,       " >&2
      echo "unauthenticated downloads from GitHub are blocked.             " >&2
      echo "" >&2
      echo "Recommended Solution (No Tokens or Passwords Needed on Pi):    " >&2
      echo "  1. On your PC, copy '${RELEASE_TARBALL_NAME}' from the       " >&2
      echo "     FamilyBoard/dist/ folder directly onto the root of your   " >&2
      echo "     MicroSD card next to familyboard-setup.sh.                " >&2
      echo "  2. Re-run this script. It will detect the local tarball and   " >&2
      echo "     install 100% offline without needing internet access.     " >&2
      echo "" >&2
      echo "Alternative (If downloading over network with GitHub Token):   " >&2
      echo "  export GITHUB_TOKEN=\"ghp_yourTokenHere\"                      " >&2
      echo "  sudo -E bash $0                                              " >&2
      echo "==============================================================" >&2
      exit 1
    fi
  fi
fi

# 6. Unpack into /opt/familyboard
echo ""
echo "--- Step 2: Extracting Appliance Files ---"
INSTALL_ROOT="/opt/familyboard"
mkdir -p "$INSTALL_ROOT"
tar -xzf "$TARBALL_PATH" -C "$INSTALL_ROOT" --strip-components=1 2>/dev/null || tar -xzf "$TARBALL_PATH" -C "$INSTALL_ROOT"
echo "Appliance files extracted to ${INSTALL_ROOT}"

# 7. Install System Dependencies
echo ""
echo "--- Step 3: Installing Required Appliance Packages ---"
export DEBIAN_FRONTEND=noninteractive

REQUIRED_PKGS=(
  "ca-certificates"
  "curl"
  "jq"
  "sqlite3"
  "unclutter"
  "x11-xserver-utils"
)

# Detect if Chromium is already installed
if ! command -v chromium-browser >/dev/null 2>&1 && ! command -v chromium >/dev/null 2>&1; then
  REQUIRED_PKGS+=("chromium-browser")
fi

MISSING_PKGS=()
for pkg in "${REQUIRED_PKGS[@]}"; do
  if ! dpkg -s "$pkg" >/dev/null 2>&1; then
    MISSING_PKGS+=("$pkg")
  fi
done

if [ ${#MISSING_PKGS[@]} -gt 0 ]; then
  echo "Updating package lists and installing: ${MISSING_PKGS[*]}..."
  apt-get update -y || true
  apt-get install -y --no-install-recommends "${MISSING_PKGS[@]}" || {
    # If chromium-browser fails (transitional on Debian 12), fallback to chromium
    apt-get install -y --no-install-recommends chromium ca-certificates curl jq sqlite3 unclutter x11-xserver-utils || true
  }
else
  echo "All prerequisite packages are already installed."
fi

# 8. Run Canonical Application Installer
echo ""
echo "--- Step 4: Installing FamilyBoard Binaries & Services ---"
if [ -f "${INSTALL_ROOT}/deploy/pi/install.sh" ]; then
  bash "${INSTALL_ROOT}/deploy/pi/install.sh"
else
  # Direct fallback installation
  install -m 0755 "${INSTALL_ROOT}/familyboard" /usr/local/bin/familyboard
  install -m 0755 "${INSTALL_ROOT}/familyboardctl" /usr/local/bin/familyboardctl
  mkdir -p /etc/familyboard /var/lib/familyboard /var/cache/familyboard
  if [ ! -f /etc/familyboard/config.toml ]; then
    cp "${INSTALL_ROOT}/config.example.toml" /etc/familyboard/config.toml
  fi
fi

# 9. Configure Full System Appliance Takeover
echo ""
echo "--- Step 5: Configuring Appliance Takeover (Kiosk & Autologin) ---"

# Detect Primary User (pi, or user configured via Raspberry Pi Imager)
PRIMARY_USER=""
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
  PRIMARY_USER="$SUDO_USER"
elif [ -d "/home/pi" ]; then
  PRIMARY_USER="pi"
else
  PRIMARY_USER=$(find /home -mindepth 1 -maxdepth 1 -type d -exec basename {} \; 2>/dev/null | grep -v 'familyboard' | head -n 1 || echo "")
fi

if [ -z "$PRIMARY_USER" ]; then
  PRIMARY_USER="pi"
  if ! id "$PRIMARY_USER" >/dev/null 2>&1; then
    useradd --create-home --shell /bin/bash --groups video,input,audio,dialout,sudo "$PRIMARY_USER" || true
  fi
fi
echo "Appliance kiosk session user: ${PRIMARY_USER}"

# A. Enable Graphical Auto-login via raspi-config
if command -v raspi-config >/dev/null 2>&1; then
  echo "Setting system boot behaviour to desktop autologin..."
  raspi-config nonint do_boot_behaviour B4 2>/dev/null || true
  raspi-config nonint do_blanking 1 2>/dev/null || true
fi

# B. Configure LightDM display manager autologin
if [ -f /etc/lightdm/lightdm.conf ]; then
  echo "Configuring LightDM autologin for ${PRIMARY_USER}..."
  mkdir -p /etc/lightdm/lightdm.conf.d
  cat <<EOF > /etc/lightdm/lightdm.conf.d/10-familyboard-autologin.conf
[Seat:*]
autologin-user=${PRIMARY_USER}
autologin-user-timeout=0
xserver-command=X -s 0 -dpms
EOF
fi

# C. Universal XDG Desktop Autostart (starts full screen kiosk on any graphical session)
echo "Installing universal XDG kiosk autostart entry..."
mkdir -p /etc/xdg/autostart
cat <<EOF > /etc/xdg/autostart/familyboard-kiosk.desktop
[Desktop Entry]
Type=Application
Name=FamilyBoard Kiosk
Comment=Household Calendar Appliance
Exec=/usr/local/bin/familyboard-kiosk.sh
Terminal=false
StartupNotify=false
X-GNOME-Autostart-enabled=true
EOF
chmod 0644 /etc/xdg/autostart/familyboard-kiosk.desktop

# D. User session autostart hooks (Wayland / Labwc / Openbox / LXDE)
USER_HOME=$(getent passwd "$PRIMARY_USER" 2>/dev/null | cut -d: -f6 || echo "/home/$PRIMARY_USER")
if [ -d "$USER_HOME" ]; then
  for autostart_path in \
    "${USER_HOME}/.config/labwc/autostart" \
    "${USER_HOME}/.config/lxsession/LXDE-pi/autostart" \
    "${USER_HOME}/.config/openbox/autostart"; do
    mkdir -p "$(dirname "$autostart_path")"
    if [ ! -f "$autostart_path" ] || ! grep -q "familyboard-kiosk.sh" "$autostart_path"; then
      echo "/usr/local/bin/familyboard-kiosk.sh &" >> "$autostart_path"
    fi
  done
  chown -R "$PRIMARY_USER:$PRIMARY_USER" "${USER_HOME}/.config" 2>/dev/null || true
fi

# E. Appliance Boot Tuning in cmdline.txt & config.txt
echo "Optimizing display and silent appliance boot parameters..."
if [ -f "$CMDLINE_TXT" ]; then
  # Remove temporary firstboot runner if present
  sed -i 's/ systemd\.run=[^ ]*//g' "$CMDLINE_TXT"
  sed -i 's/ systemd\.run_success_action=[^ ]*//g' "$CMDLINE_TXT"

  # Ensure silent appliance boot without console text spam
  for param in "quiet" "splash" "loglevel=3" "logo.nologo" "consoleblank=0" "vt.global_cursor_default=0"; do
    if ! grep -q "$param" "$CMDLINE_TXT"; then
      sed -i "1s/$/ $param/" "$CMDLINE_TXT"
    fi
  done
fi

if [ -f "$CONFIG_TXT" ]; then
  # Ensure HDMI hotplug and minimum 64MB GPU memory for smooth Chromium rendering
  if ! grep -q "gpu_mem=" "$CONFIG_TXT"; then
    echo "gpu_mem=128" >> "$CONFIG_TXT"
  fi
  if ! grep -q "hdmi_force_hotplug=1" "$CONFIG_TXT"; then
    echo "hdmi_force_hotplug=1" >> "$CONFIG_TXT"
  fi
fi

# 10. Start and Verify FamilyBoard Service
echo ""
echo "--- Step 6: Verifying Daemon Health ---"
systemctl daemon-reload
systemctl enable familyboard.service
systemctl restart familyboard.service

# Wait up to 15 seconds for local backend health
DAEMON_HEALTHY=false
for i in $(seq 1 15); do
  if curl -s -f "http://127.0.0.1:8080/health/live" >/dev/null 2>&1; then
    DAEMON_HEALTHY=true
    break
  fi
  sleep 1
done

if [ "$DAEMON_HEALTHY" = true ]; then
  echo "FamilyBoard backend is UP and HEALTHY on port 8080."
else
  echo "WARNING: FamilyBoard backend is still initializing (normal on first boot)."
fi

# 11. Final Success & Takeover Message
echo ""
echo "=============================================================="
echo "    FAMILYBOARD APPLIANCE TAKEOVER COMPLETE!                  "
echo "=============================================================="
echo ""
echo "The Raspberry Pi is now configured as a sovereign calendar appliance."
echo "On every power cycle:"
echo "  1. Raspberry Pi boots silently."
echo "  2. Graphical session logs in automatically."
echo "  3. FamilyBoard kiosk fills the entire screen."
echo "  4. Normal calendar interaction requires ZERO passwords."
echo "  5. Admin management is accessible at http://127.0.0.1:8080/admin"
echo ""

if [ "$NO_REBOOT" = true ]; then
  echo "Setup finished. (--no-reboot specified, skipping automatic reboot)"
  echo "To launch manually: sudo reboot"
else
  echo "Rebooting into FamilyBoard Appliance in 5 seconds (Press Ctrl+C to cancel)..."
  sleep 5 || true
  reboot
fi
