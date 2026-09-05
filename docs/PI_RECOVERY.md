# Appliance Disaster Recovery & Emergency Procedures

This guide provides step-by-step instructions for recovering a `FamilyBoard` appliance from corrupt storage, forgotten passwords, network partitions, or boot failures.

---

## 1. Database Corruption Recovery

If power was interrupted during an unbuffered flash write, SQLite integrity may be compromised.

### Symptoms:
- Daemon fails to start with log error: `SQLite integrity check failed: file is not a database` or `database disk image is malformed`.
- Kiosk display shows a connection error banner.

### Automated Quarantine:
The `FamilyBoard` daemon automatically executes `PRAGMA integrity_check;` on startup. If unrecoverable corruption is detected:
1. It immediately renames the damaged database file:
   `/var/lib/familyboard/familyboard.db.corrupt.<TIMESTAMP>`
2. It initializes a clean, empty database and applies all migrations up to current version.
3. It preserves all token files in `/var/lib/familyboard/tokens/` so upstream calendar synchronization can repopulate all events automatically.

### Manual Database Check & Repair:
```bash
# 1. Run offline integrity verification
familyboardctl db-check

# 2. If corrupted, attempt salvage:
sqlite3 /var/lib/familyboard/familyboard.db ".recover" | sqlite3 /var/lib/familyboard/familyboard_salvaged.db

# 3. Or restore from a known-good backup:
familyboardctl restore /var/backups/familyboard/backup-latest.tar.gz
```

---

## 2. Forgotten Admin Password Reset

If you lose access to the administrative web interface (`/admin`):

1. SSH into the Raspberry Pi:
   ```bash
   ssh pi@familyboard.local
   ```
2. Run the command-line password reset tool:
   ```bash
   sudo familyboardctl repair --reset-admin-password
   ```
3. Enter a new password when prompted. The tool hashes the password with `bcrypt` (cost 12) and updates the local administrator record immediately.
4. Log in at `http://<PI_LAN_IP>:8080/admin/login`.

---

## 3. Network Outage & Stuck Mutation Queue

If home Wi-Fi drops for an extended period (e.g. several days or weeks):

1. **Verify Offline Functionality:**
   The kiosk display remains fully interactive. New events added on screen are saved locally and placed in the mutation queue.
2. **Inspect Stuck Queue Items:**
   ```bash
   familyboardctl status
   ```
   If items appear in `dead_letter` state:
   ```bash
   # Re-queue dead-letter items for upstream replay
   sudo familyboardctl repair --retry-queue
   ```
3. **Trigger Manual Immediate Synchronization:**
   Once Wi-Fi is restored:
   ```bash
   familyboardctl sync-now
   ```

---

## 4. Boot Failure & Workstation Rescue

If the Raspberry Pi fails to boot or SSH is unresponsive:

1. Power down the Pi and remove the microSD card.
2. Insert the card into your workstation using a USB card reader.
3. The card exposes two partitions: `bootfs` (FAT32) and `rootfs` (ext4).

### Inspecting Logs on Workstation:
Mount the `rootfs` partition and check daemon logs:
```bash
tail -n 100 /media/user/rootfs/var/log/familyboard/daemon.log
```

### Emergency Network Configuration:
To force Wi-Fi reconfiguration, edit `/media/user/rootfs/etc/NetworkManager/system-connections/preconfigured.nmconnection` or place `wpa_supplicant.conf` on `bootfs`.

### Recovering Local Data:
Copy the database and token files directly to your workstation:
```bash
cp /media/user/rootfs/var/lib/familyboard/familyboard.db ~/Desktop/familyboard_salvage.db
cp -r /media/user/rootfs/var/lib/familyboard/tokens ~/Desktop/tokens_salvage
```
Re-flash the card, run `bootstrap.sh`, and restore the salvaged files.
