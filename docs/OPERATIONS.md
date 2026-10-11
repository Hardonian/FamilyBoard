# FamilyBoard Appliance Operations Guide

This document is an operator cheat sheet for administering, monitoring, and maintaining the **FamilyBoard** appliance on Raspberry Pi hardware.

> **Household Notice:** Normal household members never need to run these commands or use a terminal. This guide is strictly for the appliance administrator.

---

## 1. Quick Command Reference

All routine maintenance is handled through `familyboardctl`:

| Task | Command | Expected Output |
| :--- | :--- | :--- |
| **Check Health** | `familyboardctl health` | `READY` |
| **System Status** | `familyboardctl status` | Active calendars, pending queue count, sync state |
| **Trigger Live Sync** | `familyboardctl sync-now` | `Triggered sync across all accounts` |
| **Integrity Check** | `familyboardctl db-check` | `Database integrity check: ok` |
| **Create Backup** | `familyboardctl backup --to /var/backups/familyboard` | Verified snapshot archive |
| **Verify Backup** | `familyboardctl verify-backup /path/to/backup.tar.gz` | Checksum and SQLite integrity pass |
| **Collect Diagnostics** | `familyboardctl diagnostics --out /tmp/familyboard-diagnostics.json` | Private JSON report |
| **Verify Hardware & OS** | `sudo /opt/familyboard/deploy/pi/verify.sh` | Report across Hardware, OS, DB, Network, Display |

---

## 2. Service Management (Systemd)

FamilyBoard runs as a sandboxed systemd daemon under user `familyboard`:

```bash
# Check daemon status
sudo systemctl status familyboard.service

# View live daemon logs
sudo journalctl -u familyboard.service -f --no-tail

# Restart daemon
sudo systemctl restart familyboard.service

# Stop daemon
sudo systemctl stop familyboard.service
```

---

## 3. Chromium Kiosk Management

The physical touchscreen runs Chromium in full-screen appliance mode under user `familyboard-kiosk`:

```bash
# Restart kiosk browser without rebooting Pi
sudo pkill -f chromium

# Check kiosk launch script logs
tail -f /var/log/familyboard/kiosk.log 2>/dev/null || journalctl --user -u familyboard-kiosk -f
```

---

## 4. Network & Coexistence Verification

FamilyBoard runs exclusively on port `8080`, preserving standard web and DNS ports for Pi-hole:

```bash
# Verify listening ports (should only see 127.0.0.1:8080 and 0.0.0.0:8080)
ss -tulpn | grep familyboard

# Verify Pi-hole DNS and web server remain untouched
ss -tulpn | grep -E ':53|:80|:443'
```

---

## 5. Database Maintenance & Backups

The SQLite database uses Write-Ahead Logging (WAL) at `/var/lib/familyboard/familyboard.db`:

```bash
# 1. Check database size and WAL status
ls -lh /var/lib/familyboard/familyboard.db*

# 2. Perform an online snapshot backup and verify it before storing it elsewhere
familyboardctl backup --to /var/backups/familyboard
familyboardctl verify-backup /var/backups/familyboard/<backup-file>.tar.gz

# 3. List existing backups
ls -lh /var/backups/familyboard/

# 4. Check integrity
familyboardctl db-check
```

---

## 6. Updating the Appliance

In-place atomic upgrades with automatic health validation and rollback:

```bash
# Run atomic upgrade script
sudo /opt/familyboard/deploy/pi/update.sh /path/to/new-familyboard-linux-arm64
```

The updater:

1. Takes a pre-update database snapshot.
2. Stages the new binary.
3. Restarts the service.
4. Polls `/health/ready` for 30 seconds.
5. If health check fails, **automatically rolls back** to the prior binary.

---

## 7. Storage Retention Policy

FamilyBoard enforces bounded disk usage:

- **Chromium Cache:** Redirected to RAM tmpfs (`/dev/shm/familyboard-chromium-cache`, max 16 MB) with zero persistent writes to microSD.
- **Processed Messages:** Parsed email metadata pruned after 90 days.
- **Mutation Queue:** Successfully processed sync mutations pruned after 30 days.
- **Admin Sessions:** Expired authentication tokens purged automatically.

The daemon runs this maintenance once at startup and every 24 hours. The Admin
System tab can run the same transactional retention pass on demand, followed by
a WAL checkpoint and explicit database compaction. Active or retrying outbound
mutations prevent their related soft-deleted events from being purged.
