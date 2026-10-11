# FamilyBoard External Security Review & Audit Readiness

This document provides external security auditors with technical architectural context, threat models, attack surfaces, and reproduction commands for assessing FamilyBoard.

---

## 1. System Architecture & Attack Surfaces

FamilyBoard is a sovereign household calendar appliance deployed on embedded Linux hardware (Radxa Dragon, Raspberry Pi 5/4) or standard PC hardware.

```
                           +---------------------------+
                           |  Physical Touch Display   |
                           |  (127.0.0.1 Loopback)     |
                           +-------------+-------------+
                                         |
                                         v
+------------------------+         +-----+-----+         +------------------------+
|   Household LAN Client | ------> | HTTP Server| <------ | External Providers     |
|   (Paired Mobile PWA)  | (8080)  |  (Go 1.26) | (OAuth) | (Google, MS, Apple)    |
+------------------------+         +-----+-----+         +------------------------+
                                         |
                       +-----------------+-----------------+
                       |                                   |
                       v                                   v
             +-------------------+               +-------------------+
             | SQLite3 Database  |               | Token Storage     |
             | (WAL, 0600 mode)  |               | (AES-GCM, 0600)   |
             +-------------------+               +-------------------+
```

### Primary Attack Surfaces

1. **HTTP/LAN Transport (`:8080` / `:8443`)**:
   - Authentication bypass & session fixation
   - CSRF on state-changing endpoints
   - Rate limiting and brute-force protection on `/login`
   - Kiosk display capability forgery (`isLocalhost` vs LAN tokens)
2. **FotoCube Photo Upload (`POST /api/photos/upload`)**:
   - MIME confusion / polyglot execution
   - Decompression bombs & memory exhaustion
   - Path traversal via sanitized filenames
   - Quota enforcement (max 32MB upload, 1GB library)
3. **OAuth 2.0 & Credential Management**:
   - OAuth state forgery & HMAC tampering
   - Account cross-assignment prevention
   - Credential encryption at rest (AES-GCM)
   - Redaction of tokens from diagnostics & backup exports
4. **Local Privilege Escalation**:
   - Backup injection / arbitrary file overwrite via `VACUUM INTO`
   - Systemd unit tampering and watchdog exploitation

---

## 2. Threat Mitigations & Controls

| Threat Category | Primary Risk | Implemented Control | Test Verification |
| :--- | :--- | :--- | :--- |
| **Authentication** | Brute force admin password | In-memory token bucket rate limiter (5 attempts / 10m), returning HTTP 429 | `TestSecurity_RateLimit_BruteForceProtection` |
| **Tampering** | Forged pairing code | Atomic `UPDATE ... state = 'consumed'` transaction with code replay rejection | `TestSecurity_PairingReplayProtection` |
| **Elevation** | LAN client spoofing kiosk | Direct TCP socket address inspection (`r.RemoteAddr`), rejecting client-controlled headers | `TestSecurity_PathTraversal_Rejection` |
| **Injection** | Malicious photo payloads | Strict format decoding (`png.Decode`/`jpeg.Decode`), strip metadata by re-encoding, random 16-byte hex filenames | `TestSecurity_PhotoUpload_PolyglotAndMIMEConfusion` |
| **Information Disclosure** | Token exfiltration via support | Sanitized `config_shape.json` and redacted diagnostics, excluding token directory | `TestAdminSupportBundle` |

---

## 3. How to Run Verification locally

Auditors can run the complete qualification suite with zero setup:

```bash
# 1. Run all security regression tests
go test -v ./tests -run TestSecurity

# 2. Run race detector across all packages
go test -race ./...

# 3. Run static security and vulnerability scanner
make security-check
```

---

## 4. Coordinated Vulnerability Disclosure

Security researchers can report findings according to our policy in [SECURITY.md](file:///c:/Users/scott/GitHub/FamilyBoard/SECURITY.md).
