# FamilyBoard

[![CI](https://github.com/Hardonian/FamilyBoard/actions/workflows/ci.yml/badge.svg)](https://github.com/Hardonian/FamilyBoard/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform: Linux ARM64](https://img.shields.io/badge/Platform-Linux%20ARM64-orange.svg)](#hardware-targets--supported-boards)
[![Go Version](https://img.shields.io/badge/Go-1.26.9-00ADD8.svg)](https://go.dev)

**FamilyBoard** is an appliance-grade, sovereign, local-first digital family calendar and email event ingestion system for dedicated kitchen and refrigerator touchscreens. It targets Linux ARM64 appliances, with first-class Radxa Dragon Q64 support and a low-resource Raspberry Pi 3B profile.

---

## Key Features

- **Sovereign & Local-First:** Runs an embedded SQLite database in WAL mode. If home Wi-Fi drops, the kitchen touchscreen never freezes, blanks, or drops edits. All changes are stored locally and replayed automatically upon reconnection.
- **Multi-Identity Household Isolation:** Native support for independent accounts for family members (e.g., Parent 1: `alex@example.com` and Parent 2: `jordan@example.com`). Strict identity isolation blocks cross-assignment, cross-pollination, or silent token remapping.
- **Universal Calendar Ecosystem Interoperability (Google Workspace, Exchange & Apple):**
  - **Google Workspace (G Suite):** Paged multi-calendar discovery (`nextPageToken`), multi-tenant account chooser (`select_account`), automatic hosted domain (`hd`) routing for custom domain family accounts, and enterprise policy guidance.
  - **Microsoft Exchange Online & Office 365:** Native two-way calendar sync via Microsoft Graph (`Calendars.ReadWrite`), delta link updates, optimistic concurrency changeKey verification, and unified single sign-on with Outlook mail.
  - **Apple iCloud (CalDAV & Shared Webcal):** Direct two-way CalDAV synchronization via Apple ID + App-Specific Passwords, plus zero-friction public Apple Calendar link (`webcal://`) subscription feeds with embedded RFC 5545 iCalendar parsing.
- **Cross-Platform Smartphone PWA & Zero-Friction QR Pairing:** Seamless interoperability with Apple iPhone (iOS Safari) and Poco F7 / modern Android (Chrome / HyperOS). Standalone PWA mode with borderless Dynamic Island / notch edge-to-edge layout, bottom navigation dock, floating action button (FAB), and instant point-and-scan camera QR onboarding with cryptographically signed HMAC household write capability (`fb_kiosk_cap`).
- **Flawless Real-Time Push Synchronization:** High-frequency 15s keepalive heartbeats (`: keepalive\n\n`) on Server-Sent Events (`/api/events/stream`) preventing mobile router NAT drops. Instant synchronization triggers on phone screen wake (`visibilitychange`) and app focus. Production Service Worker (`/sw.js`) provides offline caching for uninterrupted use across the yard or garage.
- **Autonomous Email Ingestion Pipeline:** Ingests calendar invitations, school newsletters, soccer practice schedules, and dentist appointments from Gmail and Microsoft Graph. Uses RFC 5545 `.ics` parsing, natural language date extraction (`America/Toronto`), and optional LAN AI fallback (Ollama/vLLM) with strict SSRF protection.
- **Low Resource Footprint:** Pure Go daemon using `modernc.org/sqlite` (no CGO cross-compilation headaches), designed to leave most appliance memory available to the OS and Chromium kiosk.
- **Zero Runtime Dependencies:** Server-rendered HTML templates via embedded Go `embed.FS`, vanilla CSS with light/dark high-contrast themes, and vanilla JavaScript. Zero CDN calls, zero npm packages, and zero external runtime requirements.
- **Pi-hole Coexistence:** Binds strictly to `127.0.0.1:8080` (or `0.0.0.0:8080`), preserving standard ports `53` (DNS), `80` (HTTP), and `443` (HTTPS) for Pi-hole and Lighttpd.
- **Durable Mutation Queue:** Offline creations, edits, and deletions are persisted to SQLite and synced upstream with exponential backoff, jitter, and dead-letter handling.
- **Hardened Local Control Plane:** Strict-site sessions, real CSRF validation, localhost kiosk capabilities, bounded JSON and photo uploads, and authenticated access to household-sensitive hardware, presence, QR, and photo endpoints.
- **Turnkey Appliance Tooling:** Includes complete automation scripts (`bootstrap.sh`, `install.sh`, `update.sh`, `verify.sh`, `uninstall.sh`), systemd integration, and `familyboardctl` CLI management utility.

---

## Hardware Targets & Supported Boards

FamilyBoard is built for appliance single-board computers (SBCs):

### Tier 1: Radxa Dragon Q64 (Flagship Kitchen Appliance)

- **SoC:** Qualcomm Snapdragon QCS6490 (8-core Kryo 670 CPU up to 2.7 GHz, Hexagon NPU).
- **Graphics:** Qualcomm Adreno 643 GPU (Wayland / `cage` compositor with GPU rasterization).
- **Storage:** High-speed M.2 PCIe Gen3 NVMe SSD with 1-click live SD-to-NVMe cloner.
- **Wireless:** Qualcomm Wi-Fi 6E (2.4/5/6 GHz) + Bluetooth 5.2 (BLE household presence tracking).
- **Ambient Smart Frame:** FotoCube digital photo frame screensaver with drag-and-drop gallery manager.
- **Zero-Touch Onboarding:** Mobile phone camera QR pairing (`WIFI:S:FamilyBoard-Setup;...`).
- **Full Guide:** **[Radxa Dragon Native Appliance & Plug-and-Play Setup Guide](docs/RADXA_DRAGON_SETUP.md)**.

### Tier 2: Raspberry Pi 3 Model B (Low-Power / Cost-Effective)

- **SoC:** Broadcom BCM2837 (1.2 GHz 64-bit quad-core ARMv8, 1 GB LPDDR2 RAM).
- **Operating System:** Raspberry Pi OS Lite (64-bit / Debian Bookworm).
- **Display:** 15-inch HDMI 1080p (1920x1080) capacitive USB touchscreen.
- **Storage:** 32 GB or 64 GB High Endurance Class 10 MicroSD card.
- **Full Guide:** **[Turnkey MicroSD & One-Shot Setup Guide](docs/MICROSD_ONE_SHOT.md)**.

---

## Turnkey Appliance Deployment

### Radxa Dragon: Standalone SD or Appliance Takeover

1. Copy the standalone folder to any MicroSD card: `.\scripts\prepare_radxa_sd.ps1 -DriveLetter E:`
2. Insert into the Radxa Dragon and run `./run-standalone.sh` (zero install) or `sudo ./install-appliance.sh` (fullscreen kiosk on boot).
3. Clone to M.2 NVMe SSD anytime with 1 click in the Admin portal.

### Raspberry Pi: Zero-Token MicroSD Installation

See the full guide: **[Turnkey MicroSD & One-Shot Appliance Setup Guide](docs/MICROSD_ONE_SHOT.md)**.

1. Flash Raspberry Pi OS (64-bit) with Raspberry Pi Imager.
2. Run `.\scripts\prepare_sd.ps1 -Drive E:` (or `./scripts/prepare_sd.sh /media/$USER/bootfs`).
3. Insert into the Raspberry Pi and power on. It installs 100% offline and reboots directly into the full-screen kiosk.

### Method 2: From a Local Clone

On a running Raspberry Pi OS 64-bit system:

```bash
git clone git@github.com:Hardonian/FamilyBoard.git
cd FamilyBoard
sudo ./deploy/pi/bootstrap.sh
```

The bootstrap installer handles everything:

1. Validates hardware prerequisites and checks disk space.
2. Installs minimal X11, Chromium, and unclutter kiosk display dependencies.
3. Creates system service user `familyboard` and directories (`/var/lib/familyboard`, `/etc/familyboard`).
4. Cross-compiles or installs native ARM64 binaries (`familyboard`, `familyboardctl`).
5. Configures systemd auto-start and full-screen kiosk display session.
6. Preserves existing Pi-hole and DNS configurations.

---

## System Architecture

```text
+-------------------------------------------------------------------------------+
|                             Raspberry Pi 3B Appliance                         |
|                                                                               |
|  +-----------------------------+         +----------------------------------+ |
|  |       Chromium Kiosk        |         |       Remote LAN Browser         | |
|  |  (Touchscreen 1080p Display)|         |       (Admin / Settings)         | |
|  +--------------+--------------+         +----------------+-----------------+ |
|                 | Loopback                                | Private LAN       |
|                 | http://127.0.0.1:8080                   | http://pi.local   |
|                 v                                         v                   |
|  +--------------------------------------------------------------------------+ |
|  |                          FamilyBoard Daemon (Go)                         | |
|  |                                                                          | |
|  |  +------------------------+  +-------------------+  +-----------------+  | |
|  |  |    Web & Kiosk HTTP    |  |  SSE Live Event   |  | CSRF & Session  |  | |
|  |  |      Controllers       |  |      Broker       |  |    Middleware   |  | |
|  |  +-----------+------------+  +---------+---------+  +--------+--------+  | |
|  |              |                         |                     |           | |
|  |              +-------------------------+---------------------+           | |
|  |                                        |                                 | |
|  |  +-------------------+  +--------------v----+  +----------------------+  | |
|  |  |   Calendar Sync   |  |   Mutation Queue  |  |  Mail Intake Engine  |  | |
|  |  |     (Google /     |  | (Durable Replay & |  |  (MIME, ICS, NL,     |  | |
|  |  |    Microsoft)     |  |   Dead-Letter)    |  |   LAN AI Fallback)   |  | |
|  |  +---------+---------+  +---------+---------+  +----------+-----------+  | |
|  |            |                      |                       |              | |
|  |  +---------v----------------------v-----------------------v-----------+  | |
|  |  |                     Data Access Layer (Store)                      |  | |
|  |  +---------------------------------+----------------------------------+  | |
|  |                                    |                                     | |
|  |  +---------------------------------v----------------------------------+  | |
|  |  |                SQLite WAL Database Engine                          |  | |
|  |  |       (/var/lib/familyboard/familyboard.db, busy_timeout=5000)     |  | |
|  |  +--------------------------------------------------------------------+  | |
|  +--------------------------------------------------------------------------+ |
|                                       | Token Files (0600)                    |
|                                       v                                       |
|                    /var/lib/familyboard/tokens/*.json                         |
+---------------------------------------+---------------------------------------+
```

---

## Management CLI (`familyboardctl`)

The appliance includes a command-line tool for maintenance:

```bash
# Check appliance status and memory footprint
familyboardctl status

# Run database integrity and schema verification
familyboardctl db-check

# Trigger immediate calendar and mail synchronization
familyboardctl sync-now

# Create a hot, consistent snapshot backup
sudo familyboardctl backup /var/backups/familyboard/backup.tar.gz

# Restore from a backup archive
sudo familyboardctl restore /var/backups/familyboard/backup.tar.gz

# Reset administrator password
sudo familyboardctl repair --reset-admin-password

# Generate privacy-scrubbed diagnostic report
sudo familyboardctl diagnostics /tmp/diag.tar.gz
```

---

## Documentation Suite

Detailed guides and operational runbooks are available in the [`docs/`](docs/) directory:

- **Architecture & Security:**
  - [Architecture & System Design](docs/ARCHITECTURE.md)
  - [Security Policy & Mitigations](docs/SECURITY.md)
  - [STRIDE Threat Model](docs/THREAT_MODEL.md)
- **Account & Cloud Provider Setup:**
  - [Google Cloud & OAuth Setup Guide](docs/GOOGLE_SETUP.md)
  - [Gmail API Integration Guide](docs/GMAIL_SETUP.md)
  - [Microsoft Entra ID & Graph Setup Guide](docs/MICROSOFT_SETUP.md)
- **Engines & Data Model:**
  - [Email Ingestion & Extraction Pipeline](docs/EMAIL_INGESTION.md)
  - [Offline-First & Synchronization Model](docs/OFFLINE_MODEL.md)
  - [Database Schema & Migration Architecture](docs/DATABASE.md)
- **Mobile Smartphones & PWA Interoperability:**
  - [Mobile Smartphone Setup & Real-Time Sync Guide](docs/MOBILE_SETUP.md)
- **Radxa Dragon & FotoCube Appliance:**
  - [Radxa Dragon Native Appliance & Plug-and-Play Setup Guide](docs/RADXA_DRAGON_SETUP.md)
- **Raspberry Pi Operations & Runbooks:**
  - [Turnkey MicroSD & One-Shot Setup Guide](docs/MICROSD_ONE_SHOT.md)
  - [Appliance Bootstrap Guide](docs/PI_BOOTSTRAP.md)
  - [Kiosk Display & Touchscreen Tuning](docs/PI_KIOSK.md)
  - [Disaster Recovery & Emergency Procedures](docs/PI_RECOVERY.md)
  - [Re-Imaging & Migration Guide](docs/PI_REIMAGE.md)
  - [In-Place Upgrades & Rollbacks](docs/PI_UPGRADE.md)
  - [Backup & Restore Operations](docs/BACKUP_RESTORE.md)
  - [Troubleshooting & Diagnostics](docs/TROUBLESHOOTING.md)
  - [Release Management & Procedures](docs/RELEASE.md)

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
