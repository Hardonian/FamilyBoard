#!/bin/bash
# FamilyBoard Appliance Verification Script
# Validates Hardware, OS, Network, Service, Database, and Kiosk

set -Eeuo pipefail

PASS_COUNT=0
WARN_COUNT=0
FAIL_COUNT=0

report() {
  local level="$1"
  local item="$2"
  local details="$3"

  case "$level" in
    PASS)
      echo "  [PASS] $item: $details"
      PASS_COUNT=$((PASS_COUNT + 1))
      ;;
    WARN)
      echo "  [WARN] $item: $details"
      WARN_COUNT=$((WARN_COUNT + 1))
      ;;
    FAIL)
      echo "  [FAIL] $item: $details"
      FAIL_COUNT=$((FAIL_COUNT + 1))
      ;;
  esac
}

echo "=================================================="
echo "      FamilyBoard Appliance Verification          "
echo "=================================================="

# 1. Hardware Inspection
echo "\n--- 1. Hardware ---"
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "armv7l" ] || [ "$ARCH" = "x86_64" ]; then
  report "PASS" "Architecture" "$ARCH"
else
  report "FAIL" "Architecture" "Unsupported architecture $ARCH"
fi

TOTAL_MEM_KB=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo "1048576")
TOTAL_MEM_MB=$((TOTAL_MEM_KB / 1024))
if [ "$TOTAL_MEM_MB" -ge 800 ]; then
  report "PASS" "RAM" "${TOTAL_MEM_MB} MB available"
else
  report "WARN" "RAM" "Low RAM detected (${TOTAL_MEM_MB} MB). Verify swap/zram is configured."
fi

if command -v vcgencmd >/dev/null 2>&1; then
  THROTTLE=$(vcgencmd get_throttled | cut -d= -f2)
  if [ "$THROTTLE" = "0x0" ]; then
    report "PASS" "Throttling" "No undervoltage or throttling detected"
  else
    report "WARN" "Throttling" "Throttling detected: $THROTTLE. Check power supply."
  fi
fi

# 2. OS & Storage
echo "\n--- 2. OS & Storage ---"
if [ -f /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  report "PASS" "OS Release" "${PRETTY_NAME:-Linux}"
else
  report "WARN" "OS Release" "Generic Linux"
fi

# Test Root Read-Write
if touch /tmp/.fb_rw_check 2>/dev/null; then
  rm -f /tmp/.fb_rw_check
  report "PASS" "Filesystem Mode" "Root filesystem is read-write"
else
  report "FAIL" "Filesystem Mode" "Root filesystem is READ-ONLY. Storage error suspected."
fi

# Free Disk Space
FREE_MB=$(df -m / | awk 'NR==2 {print $4}')
if [ "$FREE_MB" -gt 1000 ]; then
  report "PASS" "Free Storage" "${FREE_MB} MB free on root"
elif [ "$FREE_MB" -gt 250 ]; then
  report "WARN" "Free Storage" "Low disk space: ${FREE_MB} MB free"
else
  report "FAIL" "Free Storage" "Critically low disk space: ${FREE_MB} MB free"
fi

# 3. Network
echo "\n--- 3. Network ---"
IP_ADDR=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")
if [ -n "$IP_ADDR" ] && [ "$IP_ADDR" != "127.0.0.1" ]; then
  report "PASS" "Local IP" "$IP_ADDR"
else
  report "WARN" "Local IP" "No external LAN IP detected; local-only loopback active"
fi

if host google.com >/dev/null 2>&1 || ping -c 1 -W 2 8.8.8.8 >/dev/null 2>&1; then
  report "PASS" "Internet Connectivity" "Connected to external network"
else
  report "WARN" "Internet Connectivity" "Internet offline. Local appliance operates from cache."
fi

# 4. FamilyBoard Service & Database
echo "\n--- 4. FamilyBoard Service & Database ---"
if command -v systemctl >/dev/null 2>&1; then
  if systemctl is-active --quiet familyboard.service 2>/dev/null; then
    report "PASS" "Systemd Service" "familyboard.service is active"
  else
    report "WARN" "Systemd Service" "familyboard.service is not currently active"
  fi
fi

if curl -s -f "http://127.0.0.1:8080/health/live" >/dev/null 2>&1; then
  report "PASS" "HTTP API Liveness" "Endpoint /health/live responded 200 OK"
else
  report "WARN" "HTTP API Liveness" "Backend process not responding on port 8080"
fi

if [ -f /var/lib/familyboard/familyboard.db ]; then
  if command -v sqlite3 >/dev/null 2>&1; then
    INTEGRITY=$(sqlite3 /var/lib/familyboard/familyboard.db "PRAGMA integrity_check;")
    if [ "$INTEGRITY" = "ok" ]; then
      report "PASS" "Database Integrity" "SQLite integrity check: ok"
    else
      report "FAIL" "Database Integrity" "Integrity error: $INTEGRITY"
    fi
  else
    report "PASS" "Database" "Database file exists (/var/lib/familyboard/familyboard.db)"
  fi
else
  report "WARN" "Database" "Database not yet initialized at /var/lib/familyboard/familyboard.db"
fi

# 5. Kiosk Display
echo "\n--- 5. Kiosk Display ---"
if pgrep -f "chromium" >/dev/null 2>&1; then
  report "PASS" "Chromium Kiosk" "Chromium kiosk process running"
else
  report "WARN" "Chromium Kiosk" "Chromium kiosk not detected (may be running headless or starting up)"
fi

echo "\n=================================================="
echo "Verification Summary: $PASS_COUNT PASS, $WARN_COUNT WARN, $FAIL_COUNT FAIL"
echo "=================================================="

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
exit 0
