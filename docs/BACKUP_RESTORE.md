# Backup & Restore Operations

This guide covers disaster-proof backup procedures, automated nightly snapshots, off-appliance replication, and recovery drill verification.

---

## 1. Hot Backup Engine (`VACUUM INTO`)

Backing up a live SQLite database in WAL mode using standard Linux file copy (`cp`) while writers are active can produce corrupt, mismatched journal copies.

`FamilyBoard` uses SQLite's native online hot snapshot command:
```sql
VACUUM INTO '/tmp/familyboard_snapshot.db';
```
- **Zero Downtime:** Operates concurrently while the kiosk and sync workers read and write.
- **Self-Contained:** Flushes dirty WAL pages into a single, defragmented, consistent database file.
- **Verification:** Automatically executes `PRAGMA integrity_check;` on the resulting snapshot before packaging.

---

## 2. Manual Backup Creation

To create an immediate backup archive:

```bash
sudo familyboardctl backup /var/backups/familyboard/familyboard-backup-$(date +%Y%m%d_%H%M%S).tar.gz
```

### Archive Contents:
```text
familyboard-backup-20261015_030000.tar.gz
├── manifest.json            # Timestamps, versions, and SHA-256 checksums
├── database/
│   └── familyboard.db       # Vacuumed SQLite database
├── config/
│   └── config.toml          # System configuration
└── tokens/
    ├── google_scott.json    # OAuth token stores (0600)
    └── google_michelle.json
```

---

## 3. Automated Nightly Cron Snapshots

The bootstrap installer configures a nightly cron job at 3:00 AM:

```bash
# /etc/cron.d/familyboard-backup
0 3 * * * root /usr/local/bin/familyboardctl backup /var/backups/familyboard/daily-$(date +\%u).tar.gz --keep 7
```
- Keeps a 7-day rolling rotation on the appliance storage.

### Off-Device Replication:
To ensure protection against physical microSD or hardware loss, replicate the nightly backup to a home NAS, Raspberry Pi server, or personal computer via `rsync`:

```bash
# Example rsync job running on a home server:
rsync -avz pi@familyboard.local:/var/backups/familyboard/ /volume1/backups/familyboard/
```

---

## 4. Restore Procedure

To restore the appliance to a previous backup snapshot:

```bash
sudo familyboardctl restore /var/backups/familyboard/familyboard-backup-20261015_030000.tar.gz
```

### Automatic Safety Steps During Restore:
1. Stops the `familyboard` daemon to release database file locks.
2. Validates the SHA-256 hash of every file in the tarball against `manifest.json`.
3. Backs up the current database to `/var/lib/familyboard/familyboard.db.pre-restore` in case of accidental invocation.
4. Stages restored files to `/var/lib/familyboard/`.
5. Enforces strict file permissions (`0640` for DB/config, `0600` for tokens, owner `familyboard:familyboard`).
6. Executes `PRAGMA integrity_check;` on the restored database.
7. Restarts the service and verifies system status.

---

## 5. Verification Drill (Testing Backups)

To verify the integrity of a backup without overwriting production data:

```bash
# Extract to a scratch directory and test integrity
mkdir /tmp/drill && cd /tmp/drill
tar -xzf /var/backups/familyboard/daily-1.tar.gz
sha256sum -c manifest.json
sqlite3 database/familyboard.db "PRAGMA integrity_check;"
sqlite3 database/familyboard.db "SELECT count(*) FROM events;"
rm -rf /tmp/drill
```
If `ok` is returned and event counts match expectations, the backup is certified valid.
