# In-Place Upgrade & Rollback Guide

This document explains the safe, zero-downtime, and rollback-protected software upgrade procedure for the `FamilyBoard` appliance.

---

## 1. Upgrade Philosophy & Safeguards

Upgrades on an embedded home appliance must be completely reliable. A failed upgrade must never leave the household with a broken screen or corrupted database.

### Safeguards Built into `update.sh`:
1. **Automated Pre-Update Snapshot:**
   Before any files are changed, `VACUUM INTO` creates a verified SQLite database snapshot and archives `/etc/familyboard/config.toml` in `/var/backups/familyboard/pre-upgrade/`.
2. **Atomic Binary Swapping:**
   New binaries are validated before deployment. Known-good binaries are retained as `/usr/local/bin/familyboard.known-good` and `/usr/local/bin/familyboardctl.known-good`.
3. **Atomic Schema Migrations:**
   Embedded database migrations run in forward-only, transactional blocks. If a migration statement errors, the database rolls back to its exact pre-migration state.
4. **Health Check & Auto-Rollback:**
   After starting the new version, `update.sh` calls `familyboardctl health` for up to 45 seconds. The CLI handles direct TLS certificates while keeping the request on loopback. If readiness fails, the script immediately:
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

# Verify release checksums first, then pass both matching binaries
sha256sum -c SHA256SUMS
sudo /opt/familyboard/deploy/pi/update.sh \
  ./familyboard-linux-arm64 ./familyboardctl-linux-arm64
```

To make the updater independently verify unpacked binaries, provide their
digests through the protected environment:

```bash
sudo FAMILYBOARD_DAEMON_SHA256=<daemon-sha256> \
  FAMILYBOARD_CTL_SHA256=<ctl-sha256> \
  /opt/familyboard/deploy/pi/update.sh \
  ./familyboard-linux-arm64 ./familyboardctl-linux-arm64
```

---

## 3. Manual Rollback Procedure

In the unlikely event that an issue is discovered after a successful update:

```bash
# Revert to previous binary version
sudo install -m 0755 /usr/local/bin/familyboard.known-good /usr/local/bin/familyboard
sudo install -m 0755 /usr/local/bin/familyboardctl.known-good /usr/local/bin/familyboardctl

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
