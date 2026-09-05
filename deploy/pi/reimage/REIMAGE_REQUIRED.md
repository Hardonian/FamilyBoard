# Reimage Required: Raspberry Pi OS Upgrade Guidance

If the FamilyBoard preflight checks report `REIMAGE_REQUIRED`, the existing operating system on the Raspberry Pi is an older major release (such as Debian 10 Buster or older) that lacks required modern Chromium Wayland/labwc support, glibc versions, or cryptographic libraries.

Rather than running an automated distribution mutation across major versions (which poses a high risk of microSD corruption or bricking the Pi), FamilyBoard enforces a safe migration workflow:

## Safe Migration Steps

1. **Create a Migration Bundle on the Existing Pi**:
   ```bash
   sudo ./deploy/pi/create-migration-bundle.sh /tmp
   ```
   This creates a portable archive: `/tmp/familyboard-migration-bundle-YYYYMMDD-HHMMSS.tar.gz`.

2. **Copy the Migration Bundle to a Safe Machine or USB Drive**:
   ```bash
   scp pi@familyboard.local:/tmp/familyboard-migration-bundle-*.tar.gz ~/
   ```

3. **Flash Fresh Raspberry Pi OS to the MicroSD Card**:
   - Use **Raspberry Pi Imager** (https://www.raspberrypi.com/software/).
   - Choose OS: **Raspberry Pi OS (64-bit)** (Debian Bookworm or latest stable).
   - In Advanced Options (gear icon):
     - Set Hostname: `familyboard`
     - Set Username/Password
     - Configure Wi-Fi SSID / Country
     - Enable SSH (public-key authentication recommended)

4. **Boot Fresh Pi & Clone FamilyBoard Repository**:
   ```bash
   git clone https://github.com/Hardonian/FamilyBoard.git
   cd FamilyBoard
   ```

5. **Restore the Migration Bundle**:
   ```bash
   sudo ./deploy/pi/restore-migration-bundle.sh ~/familyboard-migration-bundle-*.tar.gz
   ```

6. **Run Appliance Bootstrap**:
   ```bash
   sudo ./deploy/pi/bootstrap.sh
   ```

All calendar events, SQLite databases, and Google/Microsoft OAuth connections will be preserved seamlessly!
