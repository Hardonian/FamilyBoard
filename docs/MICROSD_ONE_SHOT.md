# Turnkey MicroSD & One-Shot Appliance Setup Guide

This guide describes how to turn any compatible Raspberry Pi (Raspberry Pi 3B, 3B+, 4B, 5, or CM4 with 1+ GB RAM) into a dedicated, permanent **FamilyBoard Kitchen Appliance**.

> [!IMPORTANT]
> **Private Repository Architecture & Security**
>
> The **`Hardonian/FamilyBoard`** GitHub repository is **STRICTLY PRIVATE** to protect your household schedules, family calendar data, and API configurations. It does **not** need to be made public, and you should **never** make it public.
>
> Because the repository is private:
>
> 1. Unauthenticated `curl` or `wget` commands directly to `raw.githubusercontent.com` will return `404 Not Found`.
> 2. The **Zero-Token MicroSD Copy Method (Method 1)** is the official, supported, and most secure solution. By copying the installer script and pre-compiled release package from your PC to the MicroSD card, the Raspberry Pi installs **100% offline** with **zero GitHub tokens, zero passwords, and zero internet dependencies during bootstrap**.

---

## Method 1: Zero-Token MicroSD Installation (Recommended)

No keyboard, mouse, SSH, or GitHub tokens required. The appliance configures itself on first power-up and reboots directly into the full-screen FamilyBoard kiosk.

### Prerequisites

* MicroSD Card (16 GB+ Class 10 / A1)
* USB MicroSD Card Reader on your PC (Windows, Mac, or Linux)
* Raspberry Pi Imager (Free from [raspberrypi.com/software](https://www.raspberrypi.com/software/))
* Local clone of this repository on your PC

---

### Step 1: Flash Raspberry Pi OS

1. Insert your MicroSD card into your computer's card reader.
2. Open **Raspberry Pi Imager**:
   * **Device:** Select your Pi model (e.g. *Raspberry Pi 3*).
   * **Operating System:** Choose **Raspberry Pi OS (64-bit)** with desktop environment (Debian 12 Bookworm).
   * **Storage:** Select your MicroSD card.
3. Click **Next**. When asked to apply OS customisation settings (Wi-Fi, username, etc.):
   * Set your hostname (e.g. `familyboard`).
   * Set your administrative username and password (e.g. `pi` or `familyboard`).
   * Configure your home Wi-Fi SSID and password so the Pi connects to your local home network automatically.
4. Click **Write** and wait for verification to finish.

---

### Step 2: Copy FamilyBoard to the MicroSD Card (Two Files)

Do **not** unplug the card yet! In Windows Explorer, Mac Finder, or Linux file manager, you will see a drive named `bootfs` (or `boot`):

You can do this with **one command**, or **drag-and-drop manually**:

#### Option A: One-Command Automated Prep (Fastest)

**On Windows (PowerShell):**

```powershell
# In your local FamilyBoard folder (replace E: with your MicroSD drive letter):
.\scripts\prepare_sd.ps1 -Drive E:
```

**On Linux / macOS (Terminal):**

```bash
# In your local FamilyBoard folder (replace with your mount path):
./scripts/prepare_sd.sh /media/$USER/bootfs
# Or using Make:
make prepare-sd SD=/Volumes/bootfs
```

This automated helper:

1. Copies **[`familyboard-setup.sh`](../familyboard-setup.sh)** to the MicroSD card root.
2. Copies **[`dist/familyboard-1.0.0-rc.1-linux-arm64.tar.gz`](../dist/familyboard-1.0.0-rc.1-linux-arm64.tar.gz)** to the MicroSD card root.
3. Configures `cmdline.txt` for automatic execution on first boot.

---

#### Option B: Manual Drag-and-Drop (Windows / Mac / Linux GUI)

If you prefer to copy files manually in your file manager:

1. Open the MicroSD card drive (e.g. `E:\` or `bootfs`).
2. Copy these **2 files** from your local `FamilyBoard` repository directly to the root of the card:
   * `familyboard-setup.sh`
   * `dist/familyboard-1.0.0-rc.1-linux-arm64.tar.gz`
3. *(Optional for Zero-Touch Auto-Run)*:
   * Open `cmdline.txt` on the MicroSD card in a text editor (Notepad, VS Code, TextEdit).
   * At the very end of the line (keep everything on **a single line**, separated by a space), add:

     ```text
     systemd.run=/boot/firmware/familyboard-setup.sh systemd.run_success_action=reboot
     ```

     *(Note: On Debian 11 Bullseye use `/boot/familyboard-setup.sh` instead).*
   * Save the file.
4. Safely eject the MicroSD card from your PC.

---

### Step 3: Power On & OS Takeover

1. Insert the MicroSD card into your Raspberry Pi.
2. Connect your HDMI display and USB touchscreen / power supply.
3. Power on the Pi:
   * The Pi boots and executes `familyboard-setup.sh`.
   * The script detects the local `familyboard-1.0.0-rc.1-linux-arm64.tar.gz` package on the card (100% offline, zero GitHub credentials needed).
   * Installs the daemon to `/opt/familyboard`, sets up systemd service, enables desktop auto-login, redirects Chromium browser cache to RAM tmpfs (`/dev/shm`), and removes the first-boot flag from `cmdline.txt`.
   * After ~90 seconds, the Pi automatically reboots.
   * **FamilyBoard appears full-screen in kiosk mode!**

---

## Method 2: One-Shot Terminal Command (Via Keyboard or SSH)

If your Raspberry Pi is already running Raspberry Pi OS:

### Option A: From the MicroSD Card (Recommended for Private Repo)

If you copied the 2 files to your MicroSD card:

```bash
sudo bash /boot/firmware/familyboard-setup.sh
```

*(On Debian 11 Bullseye: `sudo bash /boot/familyboard-setup.sh`)*

### Option B: Via SSH Clone (Using Git SSH Keys)

If you have your GitHub SSH keys configured on the Pi:

```bash
git clone git@github.com:Hardonian/FamilyBoard.git
cd FamilyBoard
sudo ./deploy/pi/familyboard-setup.sh
```

### Option C: Online One-Shot with GitHub Personal Access Token

If you want to pull directly from the private GitHub repository without cloning:

```bash
# Generate a temporary fine-grained or classic GitHub PAT with 'Contents: Read-only'
export GITHUB_TOKEN="ghp_yourPersonalAccessTokenHere"

curl -sSL -H "Authorization: Bearer $GITHUB_TOKEN" \
  https://raw.githubusercontent.com/Hardonian/FamilyBoard/main/familyboard-setup.sh | sudo GITHUB_TOKEN="$GITHUB_TOKEN" bash
```

---

## What the Appliance Takeover Does

When `familyboard-setup.sh` runs, it completely re-configures the operating system into a sovereign appliance:

| Setting | Action Taken | Rationale |
| :--- | :--- | :--- |
| **Boot Behaviour** | Desktop Auto-login enabled (`B4`) | Boots directly into graphical desktop with zero password prompt. |
| **Display Sleep** | DPMS & Screen Blanking disabled | Prevents HDMI display from turning off or going into standby. |
| **Kiosk Engine** | Chromium launched via `/usr/local/bin/familyboard-kiosk.sh` | Runs full-screen (`--kiosk`), without address bars, tabs, or popups. |
| **MicroSD Longevity** | Chromium cache moved to tmpfs `/dev/shm` (max 16 MB) | Prevents browser cache writes from burning through microSD flash cycles. |
| **Crash Recovery** | 24/7 supervisor loop | If Chromium crashes or network drops, automatically recovers. |
| **Service Daemon** | Systemd unit `familyboard.service` | Starts the Go backend on port 8080 with automatic restart on failure. |
| **Pi-hole Safety** | Ports 53, 80, 443 untouched | Coexists harmoniously with existing Pi-hole ad-blocking setups. |

---

## First-Run Household Usage

1. **Viewing Schedule:** The 30-day Month view is displayed with family members' color-coded events.
2. **Adding an Event:** Tap the large `+ Add Event` button in the header. Tap who it's for, tap an activity chip (e.g. *Soccer*, *Dentist*), tap a date and time, then tap *Add Event*.
3. **Admin Settings:** Tap the gear icon `⚙` at top right to enter the password-protected admin portal for Google Calendar/Gmail account linking.
