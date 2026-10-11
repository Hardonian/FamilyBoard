# FamilyBoard Network Trust Boundaries & Security Architecture

This document defines the network trust perimeter, proxy handling, capability tokens, and address validation for the FamilyBoard appliance.

---

## 1. Trust Hierarchy & Classification

FamilyBoard categorizes incoming traffic into four distinct security trust domains:

| Domain | Transport & Source | Privileges Granted | Authentication Mechanism |
| :--- | :--- | :--- | :--- |
| **Physical Display (Kiosk)** | Loopback (`127.0.0.1`, `::1`) | Touchscreen calendar views, ambient photo frame, Wi-Fi onboarding | Socket `RemoteAddr` loopback verification (`ip.IsLoopback()`) |
| **Paired Mobile Device** | Local LAN (`192.168.x.x`, etc.) | Calendar interaction, event creation, photo upload | Individually revocable bearer capability token (`fb_kiosk_cap`) |
| **Admin Console** | Loopback or Local LAN | Full appliance configuration, account link, database backup/restore | Password-authenticated admin session (`fb_session`) + CSRF token |
| **Untrusted / WAN** | External or unauthenticated | Login page, Web Manifest, Service Worker | None (all mutations and private APIs rejected) |

---

## 2. Localhost Verification Policy

To prevent privilege escalation across the household network:

1. **Direct Socket Inspection**: The physical display capability (`isLocalhost`) inspects the actual TCP socket connection (`r.RemoteAddr`).
2. **Rejection of Client-Spoofed Headers**: Unauthenticated clients on the LAN cannot forge physical kiosk privileges by sending `X-Forwarded-For: 127.0.0.1` or `X-Real-IP: 127.0.0.1`.
3. **Loopback IP Validation**: Only standard loopback addresses (IPv4 `127.0.0.0/8` and IPv6 `::1`) satisfy `isLocalhost`.

---

## 3. Reverse Proxy & Forwarded Header Handling

When FamilyBoard is deployed behind a trusted local reverse proxy (e.g. Caddy or Nginx):

```
[ Browser / Phone ] ---> HTTPS (443) ---> [ Caddy Proxy (127.0.0.1) ] ---> HTTP (8080) ---> [ FamilyBoard Daemon ]
```

- When proxying, the reverse proxy must bind to `127.0.0.1`.
- If direct physical kiosk interaction is desired in proxy setups, the kiosk browser connects directly to `http://127.0.0.1:8080/` on loopback.
- Mobile phones connecting via HTTPS through the reverse proxy receive individually revocable paired device capabilities via QR-code pairing, ensuring strict identity segregation.

---

## 4. Private LAN Address Spaces

FamilyBoard is intended for private sovereign network deployment within RFC 1918 and RFC 4193 subnets:
- `10.0.0.0/8` (Class A private)
- `172.16.0.0/12` (Class B private)
- `192.168.0.0/16` (Class C private, standard home routers)
- `fc00::/7` (IPv6 Unique Local Addresses)
- `fe80::/10` (IPv6 Link-Local)

The built-in appliance firewall scripts (`iptables` / `nftables` in `deploy/pi/` and `deploy/radxa/`) reject WAN routing and drop unauthenticated broadcast scans.
