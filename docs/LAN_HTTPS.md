# FamilyBoard Secure LAN HTTPS Architecture

This guide details the security model, local certificate deployment, reverse proxy configurations, and mobile trust enrollment for running FamilyBoard over encrypted HTTPS on the household local area network (LAN).

---

## 1. Direct TLS Deployment (Built-In)

FamilyBoard contains a zero-dependency TLS engine capable of serving direct HTTPS without external proxies.

### Step 1: Generate Appliance Certificate & Key
Run the built-in control utility on the appliance:

```bash
familyboardctl generate-cert -hosts "familyboard.local,192.168.1.50" -out credentials/
```

This creates:
- `credentials/familyboard.crt` (X.509 Certificate with P-256 ECDSA signature, valid for 3 years)
- `credentials/familyboard.key` (Private key, secured with `0600` permissions)

Subject Alternative Names (SANs) automatically include `localhost`, `127.0.0.1`, `::1`, `familyboard.local`, and all active physical LAN IP addresses.

### Step 2: Configure `config.toml`
Enable direct TLS in your configuration file:

```toml
[server]
port = 8443
tls_cert_file = "credentials/familyboard.crt"
tls_key_file  = "credentials/familyboard.key"

[security]
cookie_secure = true
```

Restart the appliance service:
```bash
sudo systemctl restart familyboard
```

---

## 2. Reverse Proxy Deployment (Recommended for Advanced LANs)

If your home network already uses a reverse proxy, FamilyBoard integrates cleanly behind Caddy, Nginx, or Traefik.

### Caddy (Automatic Local CA)
Caddy automatically manages and provisions a local Certificate Authority:

```caddyfile
familyboard.local {
    tls internal
    reverse_proxy 127.0.0.1:8080 {
        header_up X-Forwarded-Proto https
        header_up X-Forwarded-Host {host}
    }
}
```

### Nginx
```nginx
server {
    listen 443 ssl http2;
    server_name familyboard.local;

    ssl_certificate /etc/familyboard/credentials/familyboard.crt;
    ssl_certificate_key /etc/familyboard/credentials/familyboard.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket & Server-Sent Events (SSE) support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

---

## 3. Trusted-Device Mobile Enrollment

To eliminate self-signed certificate warnings when accessing the mobile PWA from iPhones and Android devices:

### Apple iOS (iPhone & iPad)
1. Send `familyboard.crt` to your Apple device via AirDrop or email.
2. Open **Settings** -> tap **Profile Downloaded** -> tap **Install**.
3. Open **Settings** -> **General** -> **About** -> **Certificate Trust Settings**.
4. Under *Enable full trust for root certificates*, toggle **FamilyBoard Household Appliance** to **ON**.
5. Navigate to `https://familyboard.local:8443` in Safari and tap **Share** -> **Add to Home Screen**.

### Google Android
1. Download `familyboard.crt` to device storage.
2. Open **Settings** -> **Security & Privacy** -> **More Security Settings** -> **Encryption & Credentials**.
3. Tap **Install a certificate** -> **CA certificate**.
4. Select `familyboard.crt` and confirm PIN/fingerprint.
5. Open Chrome, navigate to `https://familyboard.local:8443`, and tap **Install App**.
