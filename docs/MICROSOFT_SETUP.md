# Microsoft Entra ID (Azure AD) & Microsoft Graph Setup Guide

This guide covers registering an application in Microsoft Entra ID to allow `FamilyBoard` to synchronize Outlook/Office 365 calendars and ingest schedule emails.

---

## 1. Register an Application in Microsoft Entra ID

1. Sign in to the [Microsoft Entra Admin Center](https://entra.microsoft.com/) or [Azure Portal](https://portal.azure.com/).
2. Navigate to **Identity > Applications > App registrations**.
3. Click **New registration**.
4. Configure the registration:
   - **Name:** `FamilyBoard Appliance`
   - **Supported account types:** Select **Accounts in any organizational directory (Any Microsoft Entra ID tenant - Multitenant) and personal Microsoft accounts (e.g. Skype, Xbox)**.
   - **Redirect URI:**
     - Platform: **Web**
     - URI: `http://127.0.0.1:8080/oauth/microsoft/callback`
5. Click **Register**.

---

## 2. Add Additional Redirect URIs

1. Under the app menu, select **Manage > Authentication**.
2. Under **Redirect URIs**, add your local appliance address:
   - `http://localhost:8080/oauth/microsoft/callback`
   - `http://<PI_LAN_IP>:8080/oauth/microsoft/callback`
3. Click **Save**.

---

## 3. Configure API Permissions

1. Go to **Manage > API permissions**.
2. Click **Add a permission** and select **Microsoft Graph**.
3. Choose **Delegated permissions**.
4. Search for and check the following permissions:
   - `User.Read` (Sign in and read user profile)
   - `Calendars.ReadWrite` (Read and write user calendars)
   - `Mail.Read` (Read user mail)
   - `offline_access` (Maintain access to data you have given it access to / refresh tokens)
5. Click **Add permissions**.

---

## 4. Create Client Secret

1. Go to **Manage > Certificates & secrets**.
2. Select the **Client secrets** tab and click **New client secret**.
3. Description: `FamilyBoard Appliance Secret`.
4. Expires: Choose the longest available period (e.g. 24 months).
5. Click **Add**.
6. **Immediately copy the Secret Value** (it will never be displayed again).

---

## 5. Configure FamilyBoard Appliance

Edit `/etc/familyboard/config.toml`:

```toml
[oauth.microsoft]
client_id = "YOUR_MICROSOFT_APPLICATION_CLIENT_ID"
client_secret = "YOUR_MICROSOFT_CLIENT_SECRET_VALUE"
redirect_url = "http://192.168.1.150:8080/oauth/microsoft/callback"
scopes = [
    "User.Read",
    "Calendars.ReadWrite",
    "Mail.Read",
    "offline_access"
]
```

Restart the appliance service:

```bash
sudo systemctl restart familyboard
```

---

## 6. Authorizing Microsoft Accounts

1. Navigate to `http://<PI_LAN_IP>:8080/admin/accounts`.
2. Click **Link Microsoft Account** for the desired household member.
3. Complete the Microsoft OAuth consent flow.
4. Verify the status reflects **Active** with Microsoft Graph sync enabled.
