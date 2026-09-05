# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.0.0] - 2026-09-05

### Added
- **Core Daemon & Storage:**
  - Embedded SQLite storage engine using pure Go `modernc.org/sqlite` driver (no CGO runtime dependencies).
  - Configured WAL mode (`PRAGMA journal_mode=WAL;`), memory-mapped I/O, and busy timeout protection for SD card longevity.
  - Transaction-safe embedded SQL migration runner with schema pre-seeding for default household member identities.
  - Automatic corrupt database quarantine mechanism on startup.
- **Identity & Security Isolation:**
  - Strict account isolation preventing cross-assignment or silent token remapping.
  - Verification of account emails against identity provider userinfo endpoints (`VerifyAccountIsolation`).
  - Secure token storage with `0600` file permissions and isolated per-account token paths.
  - Robust session management with bcrypt hashing, SHA-256 session token hashing, CSRF synchronizer tokens, and brute-force rate limiting.
  - Modern HTTP security headers (strict CSP, `X-Frame-Options: DENY`, `nosniff`, `Permissions-Policy`).
- **Calendar Synchronization & Durable Mutation Queue:**
  - Google Calendar client supporting incremental sync tokens (`syncToken`), pagination, `410 Gone` recovery, and exponential backoff with jitter for 429/5xx errors.
  - Microsoft Graph calendar synchronization client.
  - Offline-first mutation queue with automatic retry bounds, jitter, and dead-letter queueing.
  - Conflict detection engine recording three-way merge divergence into `conflicts` table.
- **Email Ingestion & Extraction Engine:**
  - Multi-tier intake pipeline supporting Gmail API and Microsoft Graph Mail polling.
  - RFC 5545 `.ics` recursive parser with line unfolding, recurrence rules, and cancellation tracking.
  - Deterministic natural language date and time extractor with `America/Toronto` timezone awareness and ambiguous time resolution.
  - Safe MIME unpacker with recursion limits (max 10 levels) and payload size guardrails (max 10 MB).
  - HTML body sanitization (`StripHTML`) stripping scripts, iframes, and active content.
  - Optional LAN AI fallback (Ollama/vLLM) with strict RFC 1918 private IP validation against SSRF.
  - Multi-signal deduplication via Message-ID and SHA-256 content hashes.
  - Policy engine evaluating auto-approval criteria vs pending candidate queue.
- **Web UI & Kiosk Presentation:**
  - Zero-runtime-dependency responsive web interface with high-contrast light and dark themes.
  - Minimum 48px tactile touch targets optimized for 15-inch wall displays.
  - Real-time live updates via Server-Sent Events (SSE) `/api/kiosk/stream`.
  - Offline localStorage caching for instant boot and network disconnects.
  - Administrative console (`/admin`) for account management, calendar mappings, candidate approvals, and diagnostic logs.
- **Raspberry Pi 3B Appliance Automation:**
  - Complete turnkey appliance deployment suite in `deploy/pi/`:
    - `bootstrap.sh`: One-step automated installer from bare Raspberry Pi OS 64-bit to running kiosk.
    - `install.sh`: Package installation, user setup, and directory creation.
    - `update.sh`: Safe in-place software upgrades with automated backup, health check, and rollback.
    - `verify.sh`: Pre-flight and post-deployment validation.
    - `uninstall.sh`: Clean service and asset teardown.
    - `create-migration-bundle.sh`: Cryptographic appliance snapshot archive.
    - `restore-migration-bundle.sh`: Verified archive restore with permission enforcement.
  - Systemd service unit (`familyboard.service`) with auto-restart.
  - Minimal Chromium kiosk launcher (`familyboard-kiosk.sh`) with RAM cache and unclutter cursor hiding.
  - Port preservation for existing Pi-hole installations (ports 53, 80, 443 untouched).
- **Command-Line Interface (`familyboardctl`):**
  - Commands for `status`, `health`, `version`, `sync-now`, `backup`, `restore`, `db-check`, `diagnostics`, `repair`, and `os-update`.
- **Quality Assurance & Verification:**
  - Comprehensive unit, integration, stress, and security test suites.
  - Verified clean execution under Go data race detector (`-race`).
  - Cross-compiled ARM64 static binaries (`familyboard-linux-arm64`, `familyboardctl-linux-arm64`).
