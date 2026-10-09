#!/bin/bash
# FamilyBoard Wayland/Cage Kiosk Launcher for Radxa Dragon
# Optimized for Qualcomm Snapdragon QCS6490 (Adreno 643 GPU) and Touchscreen

set -euo pipefail

KIOSK_URL="http://127.0.0.1:8080/"

echo "Starting FamilyBoard Radxa Dragon Kiosk launcher..."

# 1. Wait for FamilyBoard daemon to become healthy
for i in $(seq 1 60); do
  if curl -s -f "http://127.0.0.1:8080/health/live" >/dev/null 2>&1; then
    echo "FamilyBoard backend daemon is ready."
    break
  fi
  echo "Waiting for FamilyBoard backend to start ($i/60)..."
  sleep 1
done

# 2. Detect Chromium binary
CHROMIUM_BIN=""
for bin in chromium-browser chromium google-chrome; do
  if command -v "$bin" >/dev/null 2>&1; then
    CHROMIUM_BIN="$bin"
    break
  fi
done

if [ -z "$CHROMIUM_BIN" ]; then
  echo "ERROR: Chromium browser not found in PATH." >&2
  exit 1
fi

# Clean up stale locks from unexpected reboots
rm -rf ~/.config/chromium/Singleton* ~/.config/chromium/Default/Preferences.bad || true

# 3. Chromium flags tuned for Qualcomm Adreno 643 GPU and Wayland Kiosk
FLAGS=(
  "--kiosk"
  "--noerrdialogs"
  "--disable-infobars"
  "--check-for-update-interval=31536000"
  "--disable-pinch"
  "--overscroll-history-navigation=0"
  "--disable-features=TranslateUI,Translate"
  "--disable-session-crashed-bubble"
  "--disable-component-update"
  "--disable-breakpad"
  "--disable-sync"
  "--disable-speech-api"
  "--disable-default-apps"
  "--no-first-run"
  "--fast"
  "--fast-start"
  "--autoplay-policy=no-user-gesture-required"
  "--touch-events=enabled"
  "--enable-features=OverlayScrollbar"
  # Qualcomm Adreno GPU acceleration flags
  "--enable-gpu-rasterization"
  "--enable-zero-copy"
  "--ignore-gpu-blocklist"
  # RAM disk cache to eliminate flash memory wear
  "--disk-cache-dir=/dev/shm/familyboard-chromium-cache"
  "--disk-cache-size=33554432"
  "--media-cache-size=1"
)

# Enable Wayland Ozone platform if Cage/Wayland is active
if [ -n "${WAYLAND_DISPLAY:-}" ] || [ -e "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/wayland-0" ]; then
  FLAGS+=(
    "--ozone-platform=wayland"
    "--enable-features=UseOzonePlatform,OverlayScrollbar"
  )
fi

FLAGS+=("$KIOSK_URL")

# 4. Continuous recovery loop
while true; do
  echo "Launching $CHROMIUM_BIN with Qualcomm GPU acceleration..."
  "$CHROMIUM_BIN" "${FLAGS[@]}" || true
  echo "Chromium exited. Restarting in 2 seconds..."
  sleep 2
done
