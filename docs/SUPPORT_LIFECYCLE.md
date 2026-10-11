# FamilyBoard Support Lifecycle Policy

This document establishes the official hardware, operating system, browser, release cadence, and security maintenance lifecycle for the sovereign FamilyBoard smart home appliance.

---

## 1. Supported Hardware Platforms

| Platform | Model / Variant | Status | Minimum Firmware / OS | Storage Target |
| :--- | :--- | :--- | :--- | :--- |
| **Radxa Dragon** | RK3588 ARM64 | **Tier 1 (Flagship)** | Debian 12 ARM64 (Kernel 6.1+) | NVMe SSD (M.2) / eMMC |
| **Raspberry Pi 5** | 4GB / 8GB RAM | **Tier 1 (Primary)** | Raspberry Pi OS 64-bit (Bookworm) | NVMe HAT / High-end A2 microSD |
| **Raspberry Pi 4 Model B** | 4GB / 8GB RAM | **Tier 1 (Primary)** | Raspberry Pi OS 64-bit (Bookworm) | High-end A2 microSD / USB3 SSD |
| **x86_64 PC / Server** | Intel / AMD64 | **Tier 2 (Supported)** | Ubuntu 22.04+, Debian 12+, Windows 10/11 | Local NVMe/SATA SSD |

> [!NOTE]
> 32-bit platforms (e.g. Raspberry Pi Zero 1, Raspberry Pi 2) are not supported. FamilyBoard requires 64-bit architectures (`arm64`, `amd64`) for SQLite concurrency and modern browser memory safety.

---

## 2. Browser & Client Matrix

The FamilyBoard interactive frontend runs in two operational modes: Dedicated Kiosk (Appliance Display) and Mobile Household PWA.

| Client Type | Target Surface | Minimum Supported Version | Hardware Acceleration |
| :--- | :--- | :--- | :--- |
| **Touch Kiosk** | Embedded Wayland / X11 Chromium | Chromium 110+ | Glamor / GPU accelerated |
| **Apple iOS** | Mobile Safari PWA (iPhone / iPad) | iOS 16.4+ (Web Push & PWA manifest) | Metal / WebKit default |
| **Android** | Chrome / Android WebAPK | Chrome 112+ (Android 10+) | SurfaceView / Skia |
| **Admin Desktop** | Firefox, Safari, Chromium-based | Current ESR or last 2 major versions | Standard desktop canvas |

---

## 3. Release Lifecycle & Maintenance Cadence

### Release Versioning

FamilyBoard adheres to Semantic Versioning (`MAJOR.MINOR.PATCH`):

- **MAJOR** (`v2.0.0`): Breaking changes in database schema requiring non-automated migration, architectural shifts in authentication, or hardware deprecation.
- **MINOR** (`v1.5.0`): New capabilities, provider updates (e.g. Microsoft Graph API revisions), hardware integrations, and performance improvements with forward-compatible database migrations.
- **PATCH** (`v1.5.1`): Bug fixes, security patches, upstream dependency updates with zero schema changes.

### Support Windows

- **Active Release (`Current`)**: Full feature updates, bug fixes, and security patches.
- **Previous Minor (`N-1`)**: Critical security vulnerabilities and major bug fixes supported for 6 months post-supersession.
- **Long-Term Appliance Maintenance**: Each minor release receives at least 12 months of backward database forward-migration qualification.

---

## 4. End-of-Life (EOL) Policy & Appliance Survivability

FamilyBoard is built for household permanence and digital sovereignty:

1. **Zero Cloud Deprecation Kill-Switches**: If the FamilyBoard project ceases development or internet connectivity is disconnected, appliances continue serving sovereign local calendars indefinitely.
2. **Local Data Portability**: Users can export full household records via `/admin/privacy/export` or snapshot databases via `/admin/backup/download` at any point in the appliance lifecycle.
3. **Database Longevity**: All storage is plain SQLite3 adhering to strict backwards-compatible forward migrations (`migrations/*.sql`).
