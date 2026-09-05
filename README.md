# FamilyBoard

[![CI](https://github.com/Hardonian/FamilyBoard/actions/workflows/ci.yml/badge.svg)](https://github.com/Hardonian/FamilyBoard/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform: Linux ARM64](https://img.shields.io/badge/Platform-Linux%20ARM64-orange.svg)](https://www.raspberrypi.com/)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://golang.org)

**FamilyBoard** is an appliance-grade, sovereign, local-first digital family calendar and email event ingestion system designed for dedicated kitchen and refrigerator touchscreen displays (specifically tuned for a 15-inch 1080p display driven by a Raspberry Pi 3B with 1 GB RAM running Raspberry Pi OS 64-bit).

---

## Key Features

- **Sovereign & Local-First:** Runs an embedded SQLite database in WAL mode. If home Wi-Fi drops, the kitchen touchscreen never freezes, blanks, or drops edits. All changes are stored locally and replayed automatically upon reconnection.
- **Dual-Identity Household Isolation:** Native support for independent accounts for Scott (`scottrmhardie@gmail.com`) and Michelle (`michelleyounghardie@gmail.com`). Strict identity isolation blocks cross-assignment, cross-pollination, or silent token remapping.
- **Autonomous Email Ingestion Pipeline:** Ingests calendar invitations, school newsletters, soccer practice schedules, and dentist appointments from Gmail and Microsoft Graph. Uses RFC 5545 `.ics` parsing, natural language date extraction (`America/Toronto`), and optional LAN AI fallback (Ollama/vLLM) with strict SSRF protection.
- **Ultra-Low Resource Footprint:** Pure Go daemon using `modernc.org/sqlite` (no CGO cross-compilation headaches). Baseline memory consumption is under **20 MB RAM**, leaving 980+ MB for the OS and Chromium kiosk.
- **Zero Runtime Dependencies:** Server-rendered HTML templates via embedded Go `embed.FS`, vanilla CSS with light/dark high-contrast themes, and vanilla JavaScript. Zero CDN calls, zero npm packages, and zero external runtime requirements.
- **Pi-hole Coexistence:** Binds strictly to `127.0.0.1:8080` (or `0.0.0.0:8080`), preserving standard ports `53` (DNS), `80` (HTTP), and `443` (HTTPS) for Pi-hole and Lighttpd.
- **Durable Mutation Queue:** Offline creations, edits, and deletions are persisted to SQLite and synced upstream with exponential backoff, jitter, and dead-letter handling.
- **Turnkey Appliance Tooling:** Includes complete automation scripts (`bootstrap.sh`, `install.sh`, `update.sh`, `verify.sh`, `uninstall.sh`), systemd integration, and `familyboardctl` CLI management utility.

---

## Hardware Target & Specifications

- **Device:** Raspberry Pi 3 Model B (1.2 GHz 64-bit quad-core ARMv8, 1 GB LPDDR2 RAM).
- **Operating System:** Raspberry Pi OS Lite (64-bit / Debian Bookworm).
- **Display:** 15-inch HDMI 1080p (1920x1080) capacitive USB touchscreen.
- **Storage:** 32 GB or 64 GB High Endurance Class 10 MicroSD card.
- **Power:** Official Raspberry Pi 5.1V / 2.5A Micro-USB power supply.

---

## Turnkey Appliance Deployment

On a fresh installation of Raspberry Pi OS 64-bit:

```bash
git clone https://github.com/Hardonian/FamilyBoard.git
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
- **Raspberry Pi Operations & Runbooks:**
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
