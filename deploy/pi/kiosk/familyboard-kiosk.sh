#!/bin/bash
# FamilyBoard Chromium Kiosk Session Launcher
# Optimized for Raspberry Pi 3B (1 GB RAM) and 15-inch display

set -euo pipefail

KIOSK_URL="http://127.0.0.1:8080/"

echo "Starting FamilyBoard Kiosk launcher..."

# 1. Wait for FamilyBoard backend to be healthy before launching browser
for i in $(seq 1 60); do
  if curl -s -f "http://127.0.0.1:8080/health/live" >/dev/null 2>&1; then
    echo "FamilyBoard backend is ready."
    break
  fi
  echo "Waiting for FamilyBoard backend to start ($i/60)..."
  sleep 1
done

# 2. Disable screen saver, DPMS, and hide mouse pointer on touch display
if command -v xset >/dev/null 2>&1 && [ -n "${DISPLAY:-}" ]; then
  xset s off || true
  xset -dpms || true
  xset s noblank || true
fi
if command -v unclutter >/dev/null 2>&1 && [ -n "${DISPLAY:-}" ]; then
  pkill unclutter 2>/dev/null || true
  unclutter -idle 1 -root &
fi

# 3. Detect Chromium executable
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

# Clean up stale singleton lock if previous crash occurred
rm -rf ~/.config/chromium/Singleton* ~/.config/chromium/Default/Preferences.bad || true

# 4. Chromium flags for 24/7 kitchen appliance kiosk & microSD protection
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
  # MicroSD write longevity guards: browser disk cache kept entirely in RAM (tmpfs)
  "--disk-cache-dir=/dev/shm/familyboard-chromium-cache"
  "--disk-cache-size=16777216"
  "--media-cache-size=1"
  # Memory bounding for 1 GB RAM
  "--renderer-process-limit=2"
  "--js-flags=--max_old_space_size=256"
  "$KIOSK_URL"
)

# 5. Continuous recovery loop
while true; do
  echo "Launching $CHROMIUM_BIN in kiosk mode..."
  "$CHROMIUM_BIN" "${FLAGS[@]}" || true
  echo "Chromium exited. Restarting kiosk in 2 seconds..."
  sleep 2
done
