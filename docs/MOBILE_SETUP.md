# Mobile Smartphone Setup & Real-Time Sync Guide

FamilyBoard provides sovereign, appliance-grade digital family calendar and dashboard capabilities that work seamlessly across both the dedicated kitchen touchscreen (Radxa Dragon Q64 or Raspberry Pi 3B) and household smartphones: **Apple iPhone (iOS Safari)** and **Poco F7 / Modern Android (Chrome / HyperOS)**.

---

## Key Mobile Capabilities

1. **Native-Grade PWA Installation:**
   - Edge-to-edge fullscreen presentation without browser URL bars or navigation clutter.
   - Notch and Dynamic Island tolerance with CSS `viewport-fit=cover` and safe area insets (`env(safe-area-inset-top)` / `env(safe-area-inset-bottom)`).
   - Dynamic viewport sizing (`100dvh`) eliminating jumping address bars on mobile browsers.
   - Pure vector SVG and high-resolution maskable application icons.

2. **Zero-Friction Camera QR Code Pairing:**
   - No typing passwords or complex account logins on mobile phones.
   - Tap **"📱 Phone"** on the kitchen board to display a one-time QR code that expires after ten minutes.
   - Point your phone camera at the screen to establish a unique household-device credential (`fb_kiosk_cap`), granting event creation, editing, and deletion privileges until that device is revoked.

3. **Flawless Real-Time Push Synchronization:**
   - Server-Sent Events (SSE) with high-frequency 15-second keepalive heartbeats (`: keepalive\n\n`) preventing carrier NAT drops and battery-saver TCP sleeps.
   - Dynamic reconnection backoff with jitter and `retry: 2500` server directives.
   - Screen wake and app focus sync listeners (`visibilitychange`, `pageshow`, `focus`): as soon as you unlock your phone or switch back to the FamilyBoard tab, changes made on the kitchen board or by other family members appear instantaneously.

4. **Offline Resilience & Service Worker:**
   - Production Service Worker (`/sw.js`) caches the application shell and assets.
   - If Wi-Fi drops while you are in the garage, yard, or away from home, events remain fully readable and cached in localStorage and IndexedDB/CacheStorage.

5. **Tactile 1-Thumb Ergonomics:**
   - Fixed bottom navigation dock for instant 1-thumb switching between **Month**, **Week**, **Schedule (Agenda)**, and **Day** views.
   - Floating Action Button (FAB) for one-tap event creation.
   - Native bottom-sheet modals with touch drag handles and rounded top corners.
   - Automatic suppression of the on-screen physical touch keyboard on smartphones, letting your phone's native iOS or Android keyboard handle typing.
   - Subtle haptic feedback (`navigator.vibrate`) on Android (Poco F7) for tactile confirmation.

---

## Setup on Apple iPhone (iOS Safari)

### Step 1: Scan & Pair

1. On the kitchen FamilyBoard screen, tap the **"📱 Phone"** button in the top navigation bar.
2. Open the built-in **Camera** app on your iPhone.
3. Point your camera at the QR code on the kitchen display.
4. Tap the yellow **"FamilyBoard"** Safari link banner that appears under the code.
5. Safari consumes the one-time code and receives its own revocable household-device credential. Reopening or sharing the QR link cannot pair another phone.

### Step 2: Add to Home Screen (PWA Mode)

1. At the bottom of Safari, tap the **Share** button (⎋ square with arrow pointing up).
2. Scroll down in the share sheet and tap **"Add to Home Screen"** (⊞).
3. Confirm the name **FamilyBoard** and tap **Add** in the top-right corner.
4. FamilyBoard is now installed on your iPhone Home Screen with the high-resolution app icon!

### Step 3: Launch Sovereign App

- Tap the **FamilyBoard** icon on your Home Screen.
- It launches as a full-screen, standalone application without Safari's top or bottom address bars.
- Schedule, Day, Week, and Month views adapt fluidly to the iPhone's screen size and Dynamic Island.

---

## Setup on Poco F7 / Modern Android (Chrome & HyperOS)

### Step 1: Scan & Pair

1. Tap **"📱 Phone"** on the kitchen touchscreen.
2. Open your Poco F7 Camera app (or Google Lens).
3. Point the camera at the QR code and tap the link banner to open in Google Chrome.
4. Your phone is immediately authenticated with full household write capability.

### Step 2: Install as Native Web App

1. Chrome will automatically display an **"Install FamilyBoard"** banner at the bottom or top.
2. Tap **"Install"** (or open the Chrome menu **⋮** in the upper-right corner and select **"Install App"** / **"Add to Home screen"**).
3. Tap **Install** to confirm.
4. FamilyBoard will be added to your app drawer and home screen.

### Step 3: Ultra-Smooth High Refresh Rate

- Launch FamilyBoard from your home screen.
- On the Poco F7's 120Hz CrystalRes display, scrolling through the family Agenda schedule is silky smooth with zero stutter.
- Tapping view tabs, adding events, or saving changes provides subtle haptic vibration pulses.

---

## Real-Time Synchronization Architecture

```text
+---------------------------------------------------------------------------------+
|                                 Home Network (LAN)                              |
|                                                                                 |
|   +--------------------------+                         +----------------------+ |
|   |   Kitchen Touchscreen    |                         |     Apple iPhone     | |
|   | (Radxa Dragon / Pi 3B)   |                         |     (iOS Safari)     | |
|   +------------+-------------+                         +-----------+----------+ |
|                | Loopback                                          | Wi-Fi 6E   |
|                | (http://127.0.0.1:8080)                           | SSE Stream |
|                |                                                   v            |
|                |          +---------------------------------------------+       |
|                +--------->|           FamilyBoard Daemon (Go)           |       |
|                           |                                             |       |
|                           |  * SSE Event Broker (/api/events/stream)    |       |
|                           |  * 15s Keepalive Heartbeats (: keepalive)   |       |
|                           |  * Dynamic QR Code (/api/system/mobile-pair)|       |
|                           |  * Durable Mutation Queue & SQLite WAL      |       |
|                +--------->|                                             |       |
|                |          +---------------------------------------------+       |
|                |                                                   ^            |
|                | Wi-Fi 6E                                          | Wi-Fi 6E   |
|                | SSE Stream                                        | SSE Stream |
|   +------------+-------------+                         +-----------+----------+ |
|   |     Poco F7 Android      |                         |      Other Devices   | |
|   |  (HyperOS / Chrome PWA)  |                         |     (Tablets, Mac)   | |
|   +--------------------------+                         +----------------------+ |
+---------------------------------------------------------------------------------+
```

### Automatic Sync Triggers

FamilyBoard does not require manual refresh. It triggers live sync across all devices via:

- **Server Push (SSE):** Whenever an event is created, edited, approved from email, or deleted on any device, the server broadcasts an immediate reload event.
- **Screen Wake (`visibilitychange`):** When you take your phone out of your pocket and unlock the screen, FamilyBoard checks connection health and pulls any new items within milliseconds.
- **Wi-Fi Reconnection (`online`):** If you step outside and return home, the app immediately re-establishes the SSE stream with exponential backoff and jitter.

---

## Mobile First-Run Setup (`/setup`)

If initializing an appliance for the first time from an iPhone or Poco F7:

1. Connect your phone to the `FamilyBoard-Setup` Wi-Fi hotspot (or visit `http://familyboard.local:8080/setup`).
2. The First-Run Setup Wizard automatically formats inputs for mobile soft keyboards:
   - Names use capitalized words.
   - Emails disable autocorrect and autocompletion of wrong domains.
   - Passwords and OAuth secrets disable spellcheck.
   - Steps fit cleanly in vertical portrait layout.
3. Once completed, your phone redirects to the operational family board.

---

## Security & Privacy Considerations

- **Strict Appliance Isolation:** One-time pairing codes expire in ten minutes, are stored only as hashes, and are atomically consumed. Each paired phone receives a random credential stored only as a hash on the appliance and can be revoked under **Admin → Privacy & Devices**.
- **Physical Proximity Assurance:** Only family members physically in front of the kitchen display can scan the active QR code.
- **Zero Cloud Relay:** Synchronization occurs 100% locally on your home private LAN. No third-party servers, no telemetry, and no push notification services ever inspect your family's daily calendar activities.
