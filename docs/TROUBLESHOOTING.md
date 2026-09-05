# Troubleshooting & Diagnostics Guide

This document provides symptom-cause-solution decision trees, log analysis commands, and diagnostic procedures for the `FamilyBoard` appliance.

---

## 1. Quick Diagnostic Commands

Run these commands over SSH to inspect appliance health:

```bash
# General status overview
familyboardctl status

# Live daemon log stream
journalctl -u familyboard.service -f

# Detailed database check
familyboardctl db-check

# Hardware throttle & voltage check
vcgencmd get_throttled
# Output "throttled=0x0" indicates normal voltage and thermals.

# Hardware temperature
vcgencmd measure_temp

# Generate a privacy-scrubbed diagnostic bundle
sudo familyboardctl diagnostics /tmp/familyboard-diag.tar.gz
```

---

## 2. Symptom-Cause-Solution Decision Trees

### Symptom 1: Kiosk screen displays "Connection Refused" or blank page
- **Possible Cause A:** Daemon failed to start due to configuration syntax error.
  - *Fix:* Check logs with `journalctl -u familyboard -n 50`. Test config with `familyboard --config /etc/familyboard/config.toml --validate`.
- **Possible Cause B:** Port conflict (e.g. another service bound to port 8080).
  - *Fix:* Run `sudo ss -tulpn | grep 8080` to identify conflicting process. If needed, change `listen_addr = "127.0.0.1:8081"` in `/etc/familyboard/config.toml` and update kiosk launch script.
- **Possible Cause C:** X11 or Chromium session crashed.
  - *Fix:* Restart the display manager: `sudo systemctl restart lightdm` or `sudo pkill -f chromium`.

---

### Symptom 2: OAuth linking fails with `redirect_uri_mismatch`
- **Cause:** The redirect URI entered in GCP or Azure does not match the exact URL accessed in your browser.
- **Fix:** Ensure the authorized redirect URI in GCP / Azure matches the exact scheme, IP, port, and path:
  `http://192.168.1.150:8080/oauth/google/callback`
  *(Note: `http://localhost:8080/...` only works when browsing directly on the Pi itself, not from a remote LAN computer).*

---

### Symptom 3: Calendar events appear on Google/Outlook mobile app but not on Kiosk
- **Possible Cause A:** Polling interval has not elapsed yet.
  - *Fix:* Force immediate sync: `familyboardctl sync-now`.
- **Possible Cause B:** Stale sync cursor or `410 Gone`.
  - *Fix:* Check logs for `410 Gone` or `ErrCursorInvalid`. The daemon automatically flushes stale cursors, or you can clear cursors in the Admin UI under **Calendars > Force Full Resync**.
- **Possible Cause C:** Account token expired or revoked.
  - *Fix:* Check `familyboardctl status`. If status shows `expired`, navigate to `/admin/accounts` and click **Re-Authenticate**.

---

### Symptom 4: Touch events are inverted, swapped, or offset
- **Cause:** Screen rotation without coordinate transformation matrix.
- **Fix:** Refer to [docs/PI_KIOSK.md](file:///c:/Users/scott/GitHub/FamilyBoard/docs/PI_KIOSK.md). Run `xinput list` to find the touch controller device, then apply the appropriate 90/180/270 degree transformation matrix.

---

### Symptom 5: Email schedule events are not being extracted
- **Possible Cause A:** Email arrived from an unlisted sender with confidence below threshold.
  - *Fix:* Check the Kiosk candidate queue or Admin UI under **Candidates**. If the item is pending, click **Approve**. Add the sender to `trusted_senders` in `/etc/familyboard/config.toml` to enable auto-approval.
- **Possible Cause B:** Email body contained no recognizable dates or ICS attachments.
  - *Fix:* Check `/var/log/familyboard/daemon.log` for `extraction completed confidence=0.0`. If using LAN AI fallback, verify Ollama/vLLM is reachable on the local network (`curl http://192.168.1.x:11434/api/tags`).

---

### Symptom 6: Raspberry Pi displays yellow lightning bolt or reboots unexpectedly
- **Cause:** **Undervoltage.** The power supply is dropping below 4.65V under display/Wi-Fi load.
- **Verification:** Run `vcgencmd get_throttled`. If bit 0 or bit 16 is set (`0x50000` or `0x50005`), undervoltage has occurred.
- **Fix:** Replace the power supply with an official Raspberry Pi 5.1V / 2.5A power adapter and use a thick, high-gauge micro-USB cable.

---

### Symptom 7: "Database is locked" errors in logs
- **Cause:** External process holding a lock or slow microSD card write stall.
- **Fix:**
  1. Confirm SQLite is in WAL mode: `sqlite3 /var/lib/familyboard/familyboard.db "PRAGMA journal_mode;"` (should return `wal`).
  2. Confirm `busy_timeout` is set to `5000` in config.
  3. Ensure no external backup script is running `cp` directly on the database while running. Always use `familyboardctl backup` which uses `VACUUM INTO`.
