# Google Cloud Platform, Google Workspace & OAuth 2.0 Setup Guide

This guide walks through configuring Google Cloud Platform (GCP) and Google Workspace (formerly G Suite) to provide OAuth 2.0 credentials for `FamilyBoard`. It covers connecting personal `@gmail.com` accounts, custom domain Google Workspace accounts, and synchronizing a shared family **"HOME"** calendar between mobile phones and the kitchen touchscreen display.

---

## 1. Create a Google Cloud Project

1. Navigate to the [Google Cloud Console](https://console.cloud.google.com/).
2. Click the Project drop-down at the top left and select **New Project**.
3. Name the project `FamilyBoard-Appliance` and click **Create**.
4. Ensure the newly created project is selected in the top navigation bar.

---

## 2. Enable Required APIs

1. In the navigation menu, go to **APIs & Services > Library**.
2. Search for **Google Calendar API** and click **Enable**.
3. Search for **Gmail API** and click **Enable**.

---

## 3. Configure OAuth Consent Screen

1. In the navigation menu, select **APIs & Services > OAuth consent screen**.
2. **Choose User Type:**
   - **Internal (Recommended for Google Workspace / G Suite):** If your family uses a custom Google Workspace domain (e.g. `@smithfamily.com`), select **Internal**.
     - *Advantage:* No Google verification screens, no 7-day test user token expiry, and any family member in your domain can sign in immediately!
   - **External (For personal `@gmail.com` or mixed accounts):** Select **External**.
3. Fill in the required application details:
   - **App name:** `FamilyBoard`
   - **User support email:** `<your-admin-email>`
   - **Developer contact information:** `<your-admin-email>`
4. Click **Save and Continue**.
5. **Scopes Step:**
   Click **Add or Remove Scopes** and select:
   - `openid`
   - `https://www.googleapis.com/auth/userinfo.email`
   - `https://www.googleapis.com/auth/calendar` (full calendar read & write)
   - `https://www.googleapis.com/auth/gmail.readonly` (for email event ingestion)
6. Click **Save and Continue**.
7. **Test Users Step (Only required if External):**
   If you selected **External**, add your family members' email addresses under Test users:
   - `<member1@gmail.com>`
   - `<member2@gmail.com>`
8. Click **Save and Continue**, then return to the dashboard.

---

## 4. Create OAuth 2.0 Client Credentials

1. Go to **APIs & Services > Credentials**.
2. Click **Create Credentials** at the top and select **OAuth client ID**.
3. In **Application type**, select **Web application**.
4. Set **Name** to `FamilyBoard Appliance`.
5. Under **Authorized redirect URIs**, click **Add URI** and enter:
   - `http://localhost:8080/oauth/callback/google`
   - `http://127.0.0.1:8080/oauth/callback/google`
   - `http://<PI_LAN_IP>:8080/oauth/callback/google` (replace `<PI_LAN_IP>` with your Pi's LAN IP, e.g. `http://192.168.1.150:8080/oauth/callback/google`)
6. Click **Create**.
7. Copy the **Client ID** and **Client Secret**.

---

## 5. Setting Up the Shared "HOME" Calendar on Phones

To have a shared family calendar that syncs between everyone's mobile phones (iPhone / Android) and the FamilyBoard kitchen display:

1. **Create the Shared Calendar in Google Calendar:**
   - On your phone or computer, open [Google Calendar](https://calendar.google.com/).
   - In the left sidebar next to **Other calendars**, click **+ > Create new calendar**.
   - Name the calendar **`HOME`** (or `Home`).
   - Time zone: Select your local time zone (e.g. `America/Toronto` or `America/New_York`).
   - Click **Create calendar**.
2. **Share with Household Members:**
   - Click on the new **HOME** calendar settings.
   - Under **Share with specific people or groups**, click **Add people and groups**.
   - Enter your partner/spouse/family members' email addresses.
   - Set Permissions to **Make changes to events** (so they can add/edit events from their phones).
3. **Enable on Mobile Phone Calendar Apps:**
   - **Google Calendar App (Android & iOS):**
     - Open the app, tap the hamburger menu (`☰`), and tap **Settings**.
     - Find the **HOME** calendar and tap **Sync** (toggle on).
     - Set a distinctive color (e.g., green / emerald).
   - **Apple Calendar (iPhone / iPad):**
     - Go to **Settings > Calendar > Accounts** and ensure your Google account is added with Calendars enabled.
     - Open the Calendar app, tap **Calendars** at the bottom, and make sure **HOME** is checked.
     - When adding an event on the phone, set the **Calendar** field to **HOME**.

---

## 6. Configure & Authorize FamilyBoard

1. Enter your Google Client ID and Secret during the first-run setup wizard (`http://<PI_LAN_IP>:8080/setup`), or in `/etc/familyboard/config.toml`:

   ```toml
   [google]
   client_id = "YOUR_CLIENT_ID.apps.googleusercontent.com"
   client_secret = "YOUR_CLIENT_SECRET"
   redirect_url = "http://192.168.1.150:8080/oauth/callback/google"
   ```

2. Open the Admin UI (`http://<PI_LAN_IP>:8080/admin?tab=accounts`).
3. Click **Connect Google Calendar** next to Family Member 1. Sign in with their Google account and approve permissions.
4. If you have a second adult member, click **Connect Google Calendar** next to Family Member 2.
5. FamilyBoard automatically triggers **Calendar Auto-Discovery**:
   - Discovers all primary, secondary, and shared calendars.
   - Automatically detects the shared **`HOME`** calendar.
   - Marks **`HOME`** as the **Default Write Target** (⭐ Default) for the kitchen touchscreen.
   - Assigns a distinctive emerald theme color (`#10b981`).
6. Go to **Admin > Synced Calendars** tab (`http://<PI_LAN_IP>:8080/admin?tab=calendars`):
   - You will see the **HOME** calendar with the `🏠 HOME Calendar` badge and `⭐ Default Write Target`.
   - Click **🔍 Discover Calendars** at any time to scan for newly added or shared calendars.
   - Click **🔄 Sync All Events** to trigger immediate synchronization.

---

## 7. How Two-Way Sync Works

- **Phone → FamilyBoard:** When anyone adds an event to the **HOME** calendar from their iPhone or Android phone, FamilyBoard's background sync engine downloads the event into the local SQLite database and live-updates the kitchen touchscreen via Server-Sent Events (SSE).
- **FamilyBoard → Phone:** When anyone taps **+ Add Event** on the kitchen touchscreen display, **HOME** is selected by default. The event is queued and sent directly to Google Calendar API, immediately appearing on all family members' mobile phones.
- **Offline Resilient:** If home internet or Wi-Fi drops, the kitchen touchscreen continues to work seamlessly with cached events. Any edits made on the screen are stored in the durable mutation queue and automatically replayed to Google once internet connectivity is restored.
