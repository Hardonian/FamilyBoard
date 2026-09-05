# In-Place Upgrade & Rollback Guide

This document explains the safe, zero-downtime, and rollback-protected software upgrade procedure for the `FamilyBoard` appliance.

---

## 1. Upgrade Philosophy & Safeguards

Upgrades on an embedded home appliance must be completely reliable. A failed upgrade must never leave the household with a broken screen or corrupted database.

### Safeguards Built into `update.sh`:
1. **Automated Pre-Update Snapshot:**
   Before any files are changed, `VACUUM INTO` creates a verified SQLite database snapshot and archives `/etc/familyboard/config.toml` in `/var/backups/familyboard/pre-upgrade/`.
2. **Atomic Binary Swapping:**
   New binaries are validated before deployment. Old binaries are moved to `/usr/local/bin/familyboard.old` rather than overwritten, enabling instant reversal.
3. **Atomic Schema Migrations:**
   Embedded database migrations run in forward-only, transactional blocks. If a migration statement errors, the database rolls back to its exact pre-migration state.
4. **Health Check & Auto-Rollback:**
   After starting the new version, `update.sh` polls `http://127.0.0.1:8080/api/health` for 30 seconds. If the endpoint fails to return `HTTP 200 OK`, the script immediately:
   - Stops the failed service.
   - Restores the `.old` binaries.
   - Restores the pre-upgrade database backup.
   - Restarts the previous stable version.
   - Prints an error report with diagnostic logs.

---

## 2. Standard Upgrade Procedure

To upgrade to the latest stable release:

```bash
# SSH into the Raspberry Pi
ssh pi@familyboard.local

# Navigate to the repository
cd FamilyBoard

# Execute the update script
sudo ./deploy/pi/update.sh
```

### Specifying a Release Tag:
To upgrade to a specific release (e.g. `v1.1.0`):
```bash
sudo ./deploy/pi/update.sh --tag v1.1.0
```

---

## 3. Manual Rollback Procedure

In the unlikely event that an issue is discovered after a successful update:

```bash
# Revert to previous binary version
sudo cp /usr/local/bin/familyboard.old /usr/local/bin/familyboard
sudo cp /usr/local/bin/familyboardctl.old /usr/local/bin/familyboardctl

# Restore pre-upgrade database snapshot
sudo familyboardctl restore /var/backups/familyboard/pre-upgrade/pre-update.tar.gz

# Restart service
sudo systemctl restart familyboard

# Verify health
familyboardctl status
```

---

## 4. Upgrading the Operating System Packages

To safely apply security patches to Raspberry Pi OS (Debian) without interfering with the appliance daemon:

```bash
sudo apt update && sudo apt --only-upgrade install -y chromium-browser xorg systemd
sudo systemctl restart familyboard
```
Or use the CLI helper:
```bash
sudo familyboardctl os-update
```
