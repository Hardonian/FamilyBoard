# Backup and Restore

FamilyBoard creates online SQLite snapshots with `VACUUM INTO`, so the kiosk and
sync workers can keep running while a backup is made. Each snapshot is checked
with `PRAGMA integrity_check` before it is packaged.

## Create a backup

Use a directory and let FamilyBoard name the archive:

```bash
sudo familyboardctl backup --to /var/backups/familyboard
```

Or choose an exact new archive path:

```bash
sudo familyboardctl backup --to /var/backups/familyboard/familyboard-2026-10-10.tar.gz
```

The command refuses to overwrite an existing archive. It writes to a private
temporary file and atomically publishes the completed archive.

## Verify a backup drill

Verification never changes the live database or configuration. It validates the
database SHA-256 recorded in the manifest and runs SQLite `integrity_check` on
an isolated staged copy:

```bash
sudo familyboardctl verify-backup /var/backups/familyboard/familyboard-2026-10-10.tar.gz
```

Run this after creating a backup and after copying it to a NAS or another
computer. A successful drill prints that both the checksum and SQLite integrity
check passed.

## Archive contents

```text
familyboard-backup-YYYYMMDD-HHMMSS.tar.gz
├── manifest.json  # creation time, application/schema versions, SHA-256
├── database.db    # self-contained SQLite snapshot
└── config.toml    # included when the active configuration file exists
```

OAuth tokens are intentionally not included in the archive. Reconnect provider
accounts after restoring on replacement hardware.

## Restore safely

Stop the service first so no process has the live database open:

```bash
sudo systemctl stop familyboard.service
sudo familyboardctl verify-backup /var/backups/familyboard/familyboard-2026-10-10.tar.gz
sudo familyboardctl restore /var/backups/familyboard/familyboard-2026-10-10.tar.gz
sudo systemctl start familyboard.service
sudo familyboardctl health
```

Before replacing the database, restore validates the archived checksum and
SQLite integrity. The previous database is retained beside it with the suffix
`.bak-prerestore`, allowing a manual rollback if required.

## Optional nightly schedule

FamilyBoard does not create a scheduler automatically. If you want one, add a
systemd timer or cron job that runs `familyboardctl backup --to
/var/backups/familyboard`, then replicate that directory off the appliance.
