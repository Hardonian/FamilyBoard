# Raspberry Pi Appliance Bootstrap Guide

This guide details setting up a dedicated kitchen/refrigerator calendar appliance from a bare Raspberry Pi 3B to a fully operational, auto-booting kiosk display.

---

## 1. Hardware Bill of Materials (BOM)

| Component | Specification | Rationale |
| :--- | :--- | :--- |
| **SBC** | Raspberry Pi 3 Model B (v1.2) | 1.2 GHz 64-bit quad-core ARMv8, 1 GB LPDDR2 RAM. |
| **Storage** | 32 GB or 64 GB High Endurance MicroSD | Class 10 / U3 / A1 rated (e.g. SanDisk High Endurance). |
| **Power Supply** | Official 5.1V / 2.5A Micro-USB Supply | Prevents undervoltage throttling during Wi-Fi and display bursts. |
| **Display** | 15-inch HDMI Touchscreen Display (1080p) | 1920x1080 resolution, USB capacitive touch controller. |
| **Cables** | High-speed HDMI + Micro-USB touch cable | High-flex braided cables for clean kitchen wall mounting. |
| **Enclosure** | VESA-compatible mount with heatsinks | Passive cooling case; fanless design for silent kitchen operation. |

---

## 2. OS Preparation & Flashing

1. Download and launch the **Raspberry Pi Imager** on your workstation.
2. Under **Operating System**, choose:
   - **Raspberry Pi OS (other) > Raspberry Pi OS Lite (64-bit)** (Debian Bookworm).
   - *(Note: 64-bit OS is required for optimal memory management and modern ARM64 Go binaries).*
3. Under **Storage**, select your microSD card.
4. Click **Edit Settings** (the gear icon) to preconfigure:
   - **Hostname:** `familyboard`
   - **Username:** `pi` (or your preferred local user)
   - **Password:** Strong administrative password
   - **Wireless LAN:** Enter your 2.4 GHz or 5 GHz home Wi-Fi SSID and password
   - **Timezone:** `America/Toronto` (or local zone)
   - **Services:** Check **Enable SSH** (use password or public key authentication)
5. Click **Save** and **Write**.

---

## 3. First Boot & Network Connection

1. Insert the microSD card into the Raspberry Pi 3B.
2. Connect the HDMI cable to the display and the USB touch cable to any Pi USB port.
3. Plug in the power supply.
4. Wait 90 seconds for initial filesystem expansion and Wi-Fi connection.
5. From your workstation, SSH into the appliance:
   ```bash
   ssh pi@familyboard.local
   # or ssh pi@<IP_ADDRESS>
   ```

---

## 4. One-Step Automated Bootstrap

Clone the repository and run the turnkey appliance installer:

```bash
git clone https://github.com/Hardonian/FamilyBoard.git
cd FamilyBoard
sudo ./deploy/pi/bootstrap.sh
```

### What `bootstrap.sh` Does Automatically:
1. **Verifies Appliance Prerequisites:**
   - Validates 64-bit ARM architecture (`aarch64`).
   - Checks available RAM and confirms >= 10 GB free disk space.
   - Detects existing Pi-hole or web server bindings, verifying ports `53`, `80`, and `443` remain untouched.
2. **Installs Minimal Kiosk Display Stack:**
   - Installs `xorg`, `xinit`, `chromium`, `unclutter` (hides mouse cursor), and `matchbox-window-manager`.
   - Leaves out heavyweight desktop environments (GNOME/LXDE) to conserve RAM.
3. **Creates System Service Accounts:**
   - Creates unprivileged system user and group `familyboard`.
   - Creates `/var/lib/familyboard`, `/var/log/familyboard`, and `/etc/familyboard`.
4. **Installs Daemon & Tools:**
   - Installs `familyboard` and `familyboardctl` to `/usr/local/bin/`.
   - Generates initial `/etc/familyboard/config.toml` if not already present.
   - Applies initial embedded SQLite database schema and seeds.
5. **Configures Systemd & Auto-Boot:**
   - Registers `/etc/systemd/system/familyboard.service` to start on boot.
   - Configures the display session to launch the Chromium kiosk pointing at `http://127.0.0.1:8080/kiosk`.
6. **Reboots Appliance:** Prompts for an immediate reboot into full-screen kiosk mode.

---

## 5. Verification & Health Check

After reboot, verify the appliance status via SSH:

```bash
familyboardctl status
```

Output:
```text
FamilyBoard Appliance Status:
  Daemon Status:       Active (Running)
  Listening Address:   127.0.0.1:8080
  Database State:      Healthy (WAL mode)
  Active Events:       34
  Pending Candidates:  0
  Mutation Queue:      0 pending
  Memory Usage:        14.2 MB / 980 MB
```
