# FamilyBoard Emergency Recovery Card

Keep this sheet accessible near the appliance for fast troubleshooting during unexpected outages or hardware glitches.

---

## Emergency Troubleshooting Flow

```text
[ Symptom: Blank Screen / Frozen Display ]
                     │
                     ▼
             1. CHECK HARDWARE
  - Is red power LED steady on Raspberry Pi?
    (If flashing or off: check 5V 2.5A+ power supply).
  - Is display HDMI cable firmly seated?
  - Is touchscreen USB cable plugged in?
                     │
                     ▼
             2. WAIT FOR BOOT
  - Cold boot requires ~35–45 seconds.
  - Initial Chromium launch awaits local backend readiness.
                     │
                     ▼
         3. SSH TO CHECK STATUS
  $ ssh pi@familyboard.local
  $ familyboardctl status
  $ familyboardctl health
                     │
       ┌─────────────┴─────────────┐
       ▼                           ▼
[ Backend Down ]            [ Kiosk Exited ]
$ sudo systemctl restart    $ sudo pkill -f chromium
  familyboard.service       (kiosk supervisor restarts
                            within 2 seconds)
                     │
                     ▼
        4. RUN APPLIANCE VERIFY
  $ sudo /opt/familyboard/deploy/pi/verify.sh
  - Inspect FAIL or WARN items (Storage, DB, Network).
                     │
                     ▼
       5. DATABASE RECOVERY (IF CORRUPTED)
  $ familyboardctl db-check
  - If corrupted:
    $ familyboardctl repair
    (quarantines corrupt DB, restarts with fresh schema)
  - Restore latest snapshot:
    $ familyboardctl restore /var/lib/familyboard/backups/<LATEST_BACKUP>.db
                     │
                     ▼
            6. SYSTEM REBOOT
  $ sudo reboot
```

---

## 5 Common Issues & Instant Fixes

### 1. Kiosk Shows "Offline" Badge

* **Cause:** Home Wi-Fi is temporarily disconnected or Internet DNS is down.
* **Fix:** Normal calendar viewing and event adding work 100% offline from local SQLite cache. Changes will automatically sync once Wi-Fi reconnects. To force network restart:

  ```bash
  sudo systemctl restart NetworkManager # or dhcpcd
  ```

### 2. Kiosk Shows "Account Needs Reconnecting"

* **Cause:** Google OAuth refresh token was revoked or expired after 6 months.
* **Fix:** Tap gear icon → log into Admin with password → tap **"Reconnect Scott's Google Calendar"** → complete browser OAuth flow.

### 3. Screen Is Blank After Power Outage

* **Cause:** Raspberry Pi rebooted before display turned on or resolution hand-shake failed.
* **Fix:** Power cycle the display first, then reboot the Pi:

  ```bash
  sudo reboot
  ```

### 4. Chromium Shows Error Instead of Calendar

* **Cause:** Backend took longer than 60s to start or had transient lock.
* **Fix:**

  ```bash
  sudo systemctl restart familyboard.service
  sudo pkill -f chromium
  ```

### 5. MicroSD Card Is Full

* **Cause:** Excessive system logs or unexpected core dumps outside FamilyBoard.
* **Fix:**

  ```bash
  sudo journalctl --vacuum-size=50M
  sudo apt-get clean
  sudo df -h /
  ```
