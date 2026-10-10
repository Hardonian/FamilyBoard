# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

## [1.4.0] - 2026-10-10

### Added

- Add `FAMILYBOARD_CONFIG` support for relocatable single-binary installations and management commands.
- Resolve common Microsoft/Outlook Windows timezone identifiers in Graph and iCalendar events.
- Transactional SQLite WAL mutations (`UpsertEventAndEnqueueMutation`, `SoftDeleteEventAndEnqueueMutation`, `RestoreEventAndEnqueueMutation`) ensuring all kiosk event mutations commit atomically with their durable outbound queue records.
- Dedicated unit test suites for `internal/calendar/apple`, `internal/calendar/microsoft`, `internal/calendar/google`, and `internal/oauth`.
- Multi-ecosystem interoperability integration test suite in `tests/integration/ecosystem_interop_integration_test.go` covering concurrent Google Workspace, Microsoft Exchange, and Apple iCloud discovery, sync, and mutation replay.
- Reproducible release packaging for Linux AMD64/ARM64, Windows AMD64, and macOS ARM64, with a combined SHA-256 manifest and tag-driven GitHub publishing.

### Fixed

- Recognize Raspberry Pi boards as real appliance hardware so Wi-Fi and setup-hotspot controls are available.
- Bind new Raspberry Pi appliance installs to the LAN and select the actual NetworkManager Wi-Fi interface instead of assuming `wlan0`.
- Prevent the first-run setup endpoint from resetting an appliance after initialization, and keep partial setup failures retryable.
- Preserve calendar-local dates and floating times, accept large bounded vendor properties, skip malformed active provider events, and report correct Microsoft deletion counts.
- Ensure Apple CalDAV all-day events format with RFC 5545 `;VALUE=DATE:` specification for strict Apple iCloud compatibility.
- Fix Apple ID input element binding in Admin modal JavaScript so input field validation and toggling work seamlessly between Webcal and CalDAV modes.
- Remove unsupported autocapitalize attributes on URL and timezone form inputs in admin and setup templates for complete Safari HTML compliance.
- Clear the pinned formatting and static-analysis release gates.

## [1.3.0] - 2026-10-10

### Security

- Raise the minimum Go toolchain from 1.26.3 to 1.26.9 to pick up standard-library security fixes, and gate CI/release qualification with pinned `govulncheck` and `staticcheck` versions.

### Added

- **Google Workspace (G Suite) Enterprise Polish & Compatibility:**
  - Multi-page pagination (`nextPageToken`) in `GoogleCalendarClient.ListCalendars` ensuring complete discovery of all domain secondary, shared family, and resource calendars.
  - Multi-tenant Google Workspace account picker via `prompt=select_account consent`.
  - Automatic hosted domain (`hd`) parameter derivation for custom family domain emails.
  - Automatic Google Meet video conference link extraction from `hangoutLink` and `conferenceData` entry points.
  - Clear, actionable administrative error handling for Google Workspace policy blocks (`admin_policy_enforced`).
- **Microsoft Exchange Online & Office 365 Two-Way Calendar Synchronization:**
  - Dedicated `MicrosoftCalendarClient` in `internal/calendar/microsoft` implementing `calendar.Provider`.
  - Microsoft Graph Calendars API integration (`/v1.0/me/calendars` and `/v1.0/me/calendars/{id}/events`).
  - Full CRUD operations with changeKey ETags and optimistic concurrency.
  - Paged calendar discovery with `@odata.nextLink` and delta token synchronization with `@odata.deltaLink`.
  - Microsoft Teams online meeting link extraction (`onlineMeeting.joinUrl` and `onlineMeetingUrl`).
  - Configurable `tenant_id` support in `config.toml` (`common`, `organizations`, or custom enterprise tenant UUIDs).
  - Preserved `Calendars.ReadWrite` scope persistence across periodic OAuth token refresh cycles.
  - Background mutation queue and sync engine registration for the `microsoft` provider.
- **Apple iCloud Calendar Ecosystem (CalDAV & Shared Webcal):**
  - Dedicated `AppleCalendarClient` in `internal/calendar/apple` implementing `calendar.Provider`.
  - Apple iCloud Shared / Public Family Calendar (`webcal://` or `https://`) feed synchronization using embedded RFC 5545 iCalendar engine (`internal/mail/ics`).
  - Apple iCloud CalDAV integration supporting Apple ID + App-Specific Passwords with `PROPFIND` calendar collection discovery, `REPORT` queries, and two-way event synchronization.
  - RFC 5545 `URL` property parsing for video meetings, links, and Apple FaceTime calls.
  - Preflight credential and feed validation with helpful diagnostic guidance for Apple App-Specific Passwords.
  - Admin modal dialog (`#modal-apple-connect`) and secure endpoint (`POST /admin/apple/connect`) for connecting Apple iCloud calendars.
  - Background mutation queue and sync engine registration for the `apple` provider.
- **Unified Multi-Provider UI & Brand Aesthetics:**
  - One-tap "Join Meeting" action button in Kiosk & Mobile Event Detail modals with distinct platform styling:
    - Google Meet (`📹 Join Google Meet`, `.btn-meet-google`)
    - Microsoft Teams (`👥 Join Microsoft Teams`, `.btn-meet-teams`)
    - Apple FaceTime (`📞 Join FaceTime Call`, `.btn-meet-apple`)
    - Zoom (`🎥 Join Zoom Meeting`, `.btn-meet-zoom`)
  - Ecosystem-aware color-coded event pills and cards on Kiosk:
    - Apple iCloud: Distinct emerald branding (`.pill-apple`, `.card-apple`, `#10b981`).
    - Microsoft Exchange / 365: Classic Microsoft corporate blue (`.pill-microsoft`, `.card-microsoft`, `#0078d4`).
    - Google Workspace: Rich Google blue (`.pill-google`, `.card-google`, `#3b82f6`).
  - Provider indicator tags in Event Detail modal (`Apple iCloud`, `Exchange/365`, `Google Workspace`).
  - Universal Calendar & Mail Ecosystem Guide card in Admin Accounts dashboard.
  - Intelligent email provider detection in `DetectEmailProvider` detecting `@icloud.com`, `@me.com`, `@mac.com`, `@office365.com`, `@onmicrosoft.com`, `@outlook.com`, and `@gmail.com`.
  - Multi-provider connection selectors in Admin Accounts and Calendars tabs allowing manual provider overrides.
  - Updated first-run setup wizard (`/setup`) documentation and email input helpers.

## [1.2.0] - 2026-10-10

### Added

- **Cross-Platform Mobile Interoperability (Apple iPhone & Poco F7 Android):**
  - Full Progressive Web App (PWA) manifest (`/manifest.webmanifest`) with multi-resolution SVG and maskable icons.
  - Apple Web App meta tags (`apple-mobile-web-app-capable`, `apple-mobile-web-app-status-bar-style`, `viewport-fit=cover`, Apple touch icons) for borderless edge-to-edge notch and Dynamic Island support.
  - Dynamic Viewport Units (`100dvh`) eliminating jumping address bars on mobile browsers.
  - Dedicated Mobile Bottom Navigation Dock (`.mobile-bottom-nav`) with tactile one-thumb switching between Month, Week, Schedule (Agenda), and Day views.
  - Mobile Floating Action Button (FAB) for instant one-thumb event creation.
  - Bottom sheet modal dialogs with smooth touch physics, rounded top corners, and drag handle indicators.
  - Dual-keyboard clash prevention: smart mobile device detection suppressing physical kiosk touch keyboard in favor of native iOS and Android soft keyboards.
  - Inactivity screensaver suppression on mobile devices to prevent hijacking phone browsers.
  - Haptic feedback integration (`navigator.vibrate`) on Android (Poco F7) for tactile taps when navigating, adding, or deleting events.
- **Zero-Friction Camera QR Code Smartphone Onboarding:**
  - Pure Go dynamic QR code generator (`GET /api/system/mobile-pair/qrcode`) and pairing metadata API (`GET /api/system/mobile-pair`).
  - Point-and-scan camera pairing from Apple iPhone Camera app and Android / Poco F7 Camera/Lens.
  - Cryptographically signed HMAC pairing token verification (`/?pair=<token>`), automatically granting household write capability (`fb_kiosk_cap`) to smartphones without typing passwords.
  - One-click pairing link copying with tactile feedback.
  - Step-by-step native onboarding guidance for iOS Safari ("Add to Home Screen") and Android Chrome ("Install App").
  - Mobile-optimized first-run setup wizard (`/setup`) with auto-capitalize, auto-correct, and email input modes.
- **Flawless Real-Time Push Synchronization & Offline Resilience:**
  - High-frequency 15s keepalive heartbeat ticker (`: keepalive\n\n`) on Server-Sent Events (`GET /api/events/stream`) preventing mobile router NAT drops and cellular timeouts.
  - Reconnection interval directive (`retry: 2500\n\n`) and proxy buffering prevention (`X-Accel-Buffering: no`).
  - Resilient client-side SSE sync engine with exponential backoff, random jitter, and live status badge indicators.
  - Instant sync wake triggers on mobile lifecycle events: `document.visibilityState === 'visible'`, `window.pageshow`, `window.focus`, and `window.online`.
  - Production Service Worker (`/sw.js`) caching static shell assets and providing network-first offline API fallback for reliable browsing outside home Wi-Fi range.

### Changed

- Fingerprint embedded browser assets so kiosk updates cannot remain stuck on stale JavaScript or CSS.
- Make readiness checks inexpensive and require the database schema to match the migrations embedded in the running binary.
- Centralize build metadata across the daemon, CLI, health response, backups, and diagnostics.
- Align CI and contributor documentation with Go 1.26.9, expand race coverage to every package, and verify tidy module files.
- Strengthen Raspberry Pi and Radxa systemd sandbox settings.

### Fixed

- Refresh expiring Microsoft OAuth credentials before calendar and mail synchronization.
- Keep live-update SSE connections open beyond the ordinary HTTP write timeout and send periodic heartbeats.

---

## [1.1.0] - 2026-10-09

### Added

- **Radxa Dragon Q64 (Qualcomm Snapdragon QCS6490) Appliance Support:**
  - Native Wayland / `cage` compositor integration with direct DRM/KMS Qualcomm Adreno 643 GPU acceleration.
  - Flash longevity protection routing Chromium cache to RAM tmpfs (`/dev/shm/familyboard-chromium-cache`).
  - Turnkey automated installer (`deploy/radxa/setup-radxa.sh`) and portable zero-install runner (`deploy/radxa/run-standalone.sh`).
  - Non-root Polkit rules (`deploy/radxa/10-familyboard-polkit.rules`) enabling hardware control of Wi-Fi and Bluetooth without sudo.
- **FotoCube Ambient Digital Photo Frame Screensaver:**
  - Automated 5-minute inactivity screensaver watchdog with Ken Burns subtle pan & zoom and 15-second cross-fading.
  - Kitchen glanceable HUD: large time, day, date, weather slot, and "NEXT UP" upcoming activity schedule countdown pill.
  - Instant touch wake: any screen tap immediately restores calendar view with zero latency.
  - Admin Photo Gallery Manager with drag-and-drop JPEG/PNG/WebP uploader (`POST /api/photos/upload`) and 1-click deletion (`DELETE /api/photos/{filename}`).
- **Zero-Touch Phone QR Code Onboarding & Wi-Fi 6E:**
  - NetworkManager fallback access point (`FamilyBoard-Setup` on `192.168.4.1`) on boot when unconfigured.
  - Pure Go dynamic PNG QR code generator (`GET /api/system/wifi/qrcode`) for instant camera-based phone pairing without typing passwords.
  - Touchscreen Wi-Fi setup modal with live network scanning and integrated virtual touch keyboard.
- **1-Click SD-to-NVMe SSD Live Migration Engine:**
  - Live M.2 NVMe SSD telemetry and diagnostic reporting in Admin console.
  - Live rootfs cloning script (`deploy/radxa/migrate-to-nvme.sh`) with GPT partitioning, ext4 formatting, full capabilities `rsync -axHAX`, UUID updating in `/etc/fstab`, and extlinux bootloader configuration for ~4s direct NVMe boot.
- **Bluetooth 5.2 Household Presence Detection:**
  - Local, 100% private BLE advertisement scanning tracking household members' smartphones and watches.
  - Real-time glanceable presence chips in kiosk header (`👤 Alex · Jordan`).
- **Standalone SD Distribution Packaging:**
  - Added `scripts/prepare_radxa_sd.ps1` and `scripts/prepare_radxa_sd.sh` creating `dist/familyboard-sd-standalone/`.

### Changed

- Reworked the kiosk into responsive month, week, schedule, and today views with accessible dialogs, focus trapping, keyboard escape handling, actionable empty/error states, and safer DOM rendering.
- Hardened Wi-Fi and Bluetooth discovery, including escaped SSID parsing, deterministic signal sorting, bounded discovery, and production-safe presence defaults.
- Added strict event input validation and partial-update merging so omitted fields can no longer erase existing event dates or details.
- Expanded the release gate to verify modules, browser JavaScript, all Go packages, race behavior, both ARM64 binaries, and Raspberry Pi/Radxa deployment scripts.

### Security

- Added enforced double-submit CSRF validation to login, setup, admin forms, and destructive system controls; lock and logout are now POST-only.
- Added strict-site cookies, absolute session expiry enforcement, safe local-only login redirects, normalized per-IP login throttling, and cross-site mutation rejection.
- Generate and persist a unique 256-bit session secret on first boot instead of using a shared fallback.
- Restricted Wi-Fi QR credentials, hardware telemetry, Bluetooth presence, and family photos to the physical kiosk capability or authenticated administrators.
- Bounded and strictly decoded JSON bodies, verified uploaded image content, and prevented privileged hardware operations on ordinary Linux development or CI hosts.

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
