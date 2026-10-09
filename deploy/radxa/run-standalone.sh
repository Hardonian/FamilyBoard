#!/bin/bash
# ==============================================================================
# FamilyBoard Standalone Portable Launcher (Run directly from SD card / USB)
# Zero installation required: runs entirely from current folder
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=========================================================="
echo "  FamilyBoard Standalone Portable Appliance"
echo "  Running directly from: $SCRIPT_DIR"
echo "=========================================================="

# Create local data and photo directories
mkdir -p data credentials photos

# Ensure binary is executable
if [ -f "./familyboard" ]; then
  chmod +x ./familyboard
elif [ -f "./familyboard-linux-arm64" ]; then
  chmod +x ./familyboard-linux-arm64
  ln -sf ./familyboard-linux-arm64 ./familyboard
else
  echo "ERROR: familyboard ARM64 binary not found in $SCRIPT_DIR" >&2
  exit 1
fi

if [ -f "./familyboardctl-linux-arm64" ]; then
  chmod +x ./familyboardctl-linux-arm64
  ln -sf ./familyboardctl-linux-arm64 ./familyboardctl
fi

CONFIG_FILE="./config.toml"
if [ ! -f "$CONFIG_FILE" ]; then
  echo "Generating portable config.toml..."
  SECRET="$(head -c 32 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9' | head -c 32 2>/dev/null || echo "standalone-secret-key-1234567890")"
  cat > "$CONFIG_FILE" <<EOF
[server]
host = "0.0.0.0"
port = 8080
device_name = "FamilyBoard Portable (SD Card)"
timezone = "America/New_York"
locale = "en-US"
kiosk_write_access = true

[database]
path = "./data/familyboard.db"
busy_timeout_ms = 5000
max_open_conns = 4
max_idle_conns = 2

[security]
session_secret = "${SECRET}"
credentials_dir = "./credentials"

[sync]
calendar_interval_seconds = 300
mail_interval_seconds = 300
max_mutation_attempts = 5
mutation_retry_interval_seconds = 10

[auth]
admin_username = "admin"

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
fi

IP_ADDR="$(hostname -I 2>/dev/null | awk '{print $1}' || echo "127.0.0.1")"
echo ""
echo "Starting FamilyBoard server..."
echo "  Kiosk URL:   http://127.0.0.1:8080/"
echo "  Network URL: http://${IP_ADDR}:8080/"
echo "  Admin URL:   http://${IP_ADDR}:8080/admin"
echo ""

exec ./familyboard --config "$CONFIG_FILE"
