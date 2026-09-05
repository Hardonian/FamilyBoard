# Google Cloud Platform & OAuth 2.0 Setup Guide

This guide walks through configuring Google Cloud Platform (GCP) to provide OAuth 2.0 credentials for `FamilyBoard` to access Google Calendar and Gmail for Scott and Michelle.

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
2. Choose **External** user type (unless you have a Google Workspace domain) and click **Create**.
3. Fill in the required application details:
   - **App name:** `FamilyBoard`
   - **User support email:** `scottrmhardie@gmail.com`
   - **Developer contact information:** `scottrmhardie@gmail.com`
4. Click **Save and Continue**.
5. **Scopes Step:**
   Click **Add or Remove Scopes** and select:
   - `openid`
   - `https://www.googleapis.com/auth/userinfo.email`
   - `https://www.googleapis.com/auth/calendar.events`
   - `https://www.googleapis.com/auth/calendar.readonly`
   - `https://www.googleapis.com/auth/gmail.readonly`
6. Click **Save and Continue**.
7. **Test Users Step (Crucial):**
   Add the following email addresses under Test users:
   - `scottrmhardie@gmail.com`
   - `michelleyounghardie@gmail.com`
8. Click **Save and Continue**, then return to the dashboard.

---

## 4. Create OAuth 2.0 Client Credentials

1. Go to **APIs & Services > Credentials**.
2. Click **Create Credentials** at the top and select **OAuth client ID**.
3. In **Application type**, select **Web application**.
4. Set **Name** to `FamilyBoard Appliance`.
5. Under **Authorized redirect URIs**, click **Add URI** and enter:
   - `http://localhost:8080/oauth/google/callback`
   - `http://127.0.0.1:8080/oauth/google/callback`
   - `http://<PI_LAN_IP>:8080/oauth/google/callback` (replace `<PI_LAN_IP>` with your Pi's static LAN IP, e.g. `http://192.168.1.150:8080/oauth/google/callback`)
6. Click **Create**.
7. A dialog will display your **Client ID** and **Client Secret**. Copy these securely.

---

## 5. Configure FamilyBoard Appliance

Edit `/etc/familyboard/config.toml` (or your local `config.toml`):

```toml
[oauth.google]
client_id = "YOUR_CLIENT_ID.apps.googleusercontent.com"
client_secret = "YOUR_CLIENT_SECRET"
redirect_url = "http://192.168.1.150:8080/oauth/google/callback"
scopes = [
    "openid",
    "https://www.googleapis.com/auth/userinfo.email",
    "https://www.googleapis.com/auth/calendar.events",
    "https://www.googleapis.com/auth/calendar.readonly",
    "https://www.googleapis.com/auth/gmail.readonly"
]
```

Restart the appliance service:
```bash
sudo systemctl restart familyboard
```

---

## 6. Linking User Accounts

1. Open your browser to the FamilyBoard Admin UI (`http://<PI_LAN_IP>:8080/admin/accounts`).
2. Log in with your admin credentials.
3. Click **Link Google Account** next to **Scott**. Log in as `scottrmhardie@gmail.com` and accept permissions.
4. Click **Link Google Account** next to **Michelle**. Log in as `michelleyounghardie@gmail.com` and accept permissions.
5. The admin UI will display **Linked & Active** for both identities with token files stored in `/var/lib/familyboard/tokens/`.
