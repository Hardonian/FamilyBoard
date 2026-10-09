# Radxa Dragon Native Appliance & Plug-and-Play Setup Guide

This guide details running FamilyBoard as a native, plug-and-play sovereign calendar appliance on the **Radxa Dragon Q64 Single Board Computer** powered by the **Qualcomm Snapdragon QCS6490** SoC.

---

## 1. Hardware Architecture Overview

The Radxa Dragon offers performance, power efficiency, and connectivity for a 24/7 kitchen calendar appliance:

* **SoC:** Qualcomm Snapdragon QCS6490 (8-core Kryo 670 CPU up to 2.7 GHz, Hexagon NPU).
* **Graphics:** Qualcomm Adreno 643 GPU with Vulkan 1.2, OpenGL ES 3.2, and OpenCL 2.0.
* **Display Output:** HDMI 2.0 (up to 4K@60Hz), dual MIPI-DSI, eDP, capacitive touchscreen header.
* **Wireless:** Onboard Qualcomm Wi-Fi 6E (2.4 GHz, 5 GHz, 6 GHz 802.11ax) + Bluetooth 5.2 / BLE.
* **Storage Options:** UFS 3.1, eMMC 5.1 module, M.2 NVMe SSD (PCIe 3.0), or MicroSD card.
* **Memory:** 8 GB or 12 GB LPDDR4x / LPDDR5 RAM.
* **Compositor:** Wayland via `cage` (minimal, single-application kiosk compositor) with direct DRM/KMS hardware acceleration.

---

## 2. Operating System & Flashing

### Recommended Distribution

* **Radxa OS (Debian 12 Bookworm)** or **Ubuntu 24.04 (Noble)** ARM64 for Radxa Dragon Q64.
* Available from the official Radxa developer portal.

### Flashing Steps

1. Download the Radxa OS image (`.img.xz`).
2. Insert your MicroSD card, NVMe drive, or connect the Radxa Dragon in eMMC flash mode.
3. Flash using **balenaEtcher**, **Raspberry Pi Imager**, or `dd`:

   ```bash
   xz -d radxa-dragon-debian-bookworm-*.img.xz
   sudo dd if=radxa-dragon-debian-bookworm-*.img of=/dev/sdX bs=4M status=progress conv=fsync
   ```

4. Insert storage into the Radxa Dragon, connect your display/touchscreen, and power on via USB-C PD (12V/3A or 15V/3A recommended).

---

## 3. Turnkey Plug-and-Play Installation

FamilyBoard includes an automated installer that sets up services, user permissions, Polkit hardware elevation, Wayland kiosk compositor, and GPU acceleration.

### One-Command Setup

On the Radxa Dragon terminal or via SSH:

```bash
# Clone or copy FamilyBoard release bundle
git clone https://github.com/Hardonian/FamilyBoard.git
cd FamilyBoard

# Run the turnkey installer
sudo ./deploy/radxa/setup-radxa.sh
```

### What `setup-radxa.sh` Does Automatically

1. Installs runtime dependencies: `cage`, `chromium`, `network-manager`, `bluez`, `libinput-bin`, `mesa-va-drivers`, `mesa-vulkan-drivers`.
2. Creates the dedicated `familyboard` non-root system user and assigns permissions to `video`, `render`, `input`, `bluetooth`, `netdev`, and `tty`.
3. Installs targeted **Polkit** rules (`/etc/polkit-1/rules.d/10-familyboard.rules`), allowing the web application to scan Wi-Fi, join networks, toggle hotspots, and scan Bluetooth without root credentials.
4. Deploys native ARM64 binaries (`/usr/local/bin/familyboard` and `familyboardctl`).
5. Configures the Wayland `/usr/local/bin/familyboard-kiosk.sh` launcher with Qualcomm Adreno 643 GPU acceleration flags.
6. Enables and starts systemd units:
   * `familyboard.service` (daemon on port 8080)
   * `familyboard-kiosk.service` (fullscreen kiosk on TTY7)

---

## 4. Plug-and-Play Wi-Fi Onboarding

FamilyBoard on Radxa Dragon is designed to be plug-and-play without requiring an Ethernet cable or pre-configured credentials:

### Method A: Touchscreen Onboarding (On-Device)

1. On boot, the FamilyBoard fullscreen kiosk appears on the display.
2. Tap the **Wi-Fi** status badge in the header or tap **Admin** &rarr; **Hardware & Network**.
3. Tap **Scan Networks**.
4. Select your home Wi-Fi network and enter the passphrase using the built-in on-screen virtual keyboard.
5. Tap **Join Network**.

### Method B: Mobile Phone Hotspot Fallback (Zero-Touch)

If the device has never connected to Wi-Fi or cannot reach known networks:

1. FamilyBoard activates the fallback access point: **`FamilyBoard-Setup`**.
2. Connect your smartphone to the `FamilyBoard-Setup` Wi-Fi network.
3. Open your mobile browser and navigate to:

   ```text
   http://192.168.4.1:8080/admin?tab=hardware
   ```

4. Scan and select your home Wi-Fi network, enter your password, and click **Join Network**.
5. The Radxa Dragon connects to your home network, shuts down the setup hotspot, and switches to normal kiosk operation.

---

## 5. Bluetooth 5.2 Household Presence Detection

FamilyBoard leverages the Radxa Dragon's Qualcomm Bluetooth 5.2 radio for **100% private, local household presence detection**:

### How It Works

* Bluetooth Low Energy (BLE) advertisements from family members' devices (iPhones, Android phones, Apple Watches, Wear OS smartwatches) are passively detected.
* Zero cloud connection and zero pairing are required.
* When a registered MAC address is detected with a signal RSSI &gt; -85 dBm (indicating radio presence in the household), FamilyBoard marks that family member as **Home**.

### Registering Family Devices

1. In the Admin portal, navigate to **Hardware &amp; Network** &rarr; **Bluetooth 5.2 &amp; Household Presence Detection**.
2. Click **Scan Devices** to discover nearby Bluetooth signals.
3. Click **Use for Presence** next to the family member's phone or watch, or manually enter the device MAC address.
4. Enter the family member's name and save.
5. The Kiosk header automatically displays real-time presence chips (e.g. `👤 Alex · Jordan`) whenever they are at home!

---

## 6. Qualcomm Adreno 643 GPU Acceleration

Chromium runs under `cage` (Wayland) using Qualcomm Adreno 643 GPU acceleration:

* **Wayland Ozone Platform:** `--ozone-platform=wayland --enable-features=UseOzonePlatform,OverlayScrollbar`
* **GPU Rasterization:** `--enable-gpu-rasterization --enable-zero-copy --ignore-gpu-blocklist`
* **Flash Storage Protection:** Browser cache is routed to RAM tmpfs (`--disk-cache-dir=/dev/shm/familyboard-chromium-cache`), preventing wear on eMMC or MicroSD cards.

To verify GPU acceleration in Chromium:

```bash
# Check running flags
ps aux | grep chromium | grep -o "\-\-enable-gpu-rasterization"
```

---

## 7. Diagnostics & Appliance Control

### Hardware Health Checks via Admin Portal

Navigate to `/admin?tab=hardware`:

* **CPU Temperature:** Live thermals from `/sys/class/thermal/thermal_zone0/temp`. Normal operating temperature is 45&deg;C&ndash;65&deg;C.
* **Resource Usage:** Real-time RAM and internal storage allocation.
* **Wi-Fi Telemetry:** Signal percentage (dBm equivalent), active SSID, and IP address.
* **Appliance Controls:** One-click graceful reboot and power-off buttons.

### Command-Line Diagnostics

```bash
# Check backend daemon status
sudo systemctl status familyboard.service

# Check kiosk Wayland compositor status
sudo systemctl status familyboard-kiosk.service

# View real-time journal logs
sudo journalctl -u familyboard.service -f

# Inspect NetworkManager status
nmcli device status

# Inspect Bluetooth controller
bluetoothctl show
```
