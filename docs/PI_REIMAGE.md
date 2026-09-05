# Re-Imaging & Migration Guide

This guide explains how to re-flash your Raspberry Pi appliance or migrate to a new microSD card without losing calendar data, configured OAuth tokens, or customized settings.

---

## 1. When Re-Imaging Is Required

- Upgrading the underlying operating system (e.g., Debian Bookworm to a newer major release).
- Recovering from bad flash blocks or filesystem corruption.
- Replacing the microSD card with a higher-endurance or larger capacity card.
- Migrating to a newer Raspberry Pi board (e.g., Raspberry Pi 4 or 5).

---

## 2. Step 1: Create a Migration Bundle

Before wiping the card, export a complete, cryptographically verified appliance migration archive:

```bash
# SSH into the running appliance
ssh pi@familyboard.local

# Run the migration bundle creator
sudo ./deploy/pi/create-migration-bundle.sh /tmp/familyboard-migration.tar.gz
```

### What is Captured:
- Consistent SQLite database snapshot using `VACUUM INTO` (no dirty WAL locks).
- All OAuth token files from `/var/lib/familyboard/tokens/`.
- Configuration file `/etc/familyboard/config.toml`.
- System metadata and SHA-256 checksums in `manifest.json`.

Download the archive to your workstation:
```bash
# On your personal computer:
scp pi@familyboard.local:/tmp/familyboard-migration.tar.gz ~/Desktop/
```

---

## 3. Step 2: Re-Flash Clean OS

1. Open **Raspberry Pi Imager** on your computer.
2. Flash **Raspberry Pi OS Lite (64-bit)** to the target microSD card.
3. Configure hostname (`familyboard`), username (`pi`), Wi-Fi, and SSH in the Imager settings.
4. Insert the card into the Pi and power on.

---

## 4. Step 3: Run Bootstrap & Restore

1. SSH into the newly imaged Pi:
   ```bash
   ssh pi@familyboard.local
   ```
2. Clone the repository and run bootstrap:
   ```bash
   git clone https://github.com/Hardonian/FamilyBoard.git
   cd FamilyBoard
   sudo ./deploy/pi/bootstrap.sh
   ```
3. Copy the migration archive from your workstation to the Pi:
   ```bash
   # On your computer:
   scp ~/Desktop/familyboard-migration.tar.gz pi@familyboard.local:/tmp/
   ```
4. Restore the migration bundle:
   ```bash
   # On the Pi:
   sudo ./deploy/pi/restore-migration-bundle.sh /tmp/familyboard-migration.tar.gz
   ```

### What `restore-migration-bundle.sh` Does:
1. Stops the `familyboard` systemd service.
2. Validates the archive structure and verifies all SHA-256 checksums against `manifest.json`.
3. Unpacks the database to `/var/lib/familyboard/familyboard.db`.
4. Restores token files with strict `0600` permissions and `familyboard:familyboard` ownership.
5. Restores `/etc/familyboard/config.toml`.
6. Executes `familyboardctl db-check` to confirm database health.
7. Restarts the daemon.

---

## 5. Step 4: Verification

Check appliance health:
```bash
familyboardctl status
```

Verify that both Scott and Michelle's accounts remain connected, all calendar events are immediately visible on the kiosk display, and sync resumes seamlessly without requiring re-authentication.
