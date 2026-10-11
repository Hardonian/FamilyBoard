# STRIDE Threat Model & Security Analysis

This document provides a formal STRIDE-based threat assessment and security boundary analysis for the `FamilyBoard` sovereign appliance.

---

## 1. System Trust Boundaries

```text
[ UNTRUSTED WAN ]
      |
      | External Email & Calendar APIs (Google / Microsoft / Inbound Mail)
      v
+-------------------------------------------------------------------------------+
| TRUST BOUNDARY 1: External Data Intake                                        |
|   - MIME parser, ICS parser, Natural Language engine, OAuth redirects         |
+-------------------------------------------------------------------------------+
      |
      | Validated DTOs & Models
      v
+-------------------------------------------------------------------------------+
| TRUST BOUNDARY 2: Core Daemon Execution                                       |
|   - Synchronization engine, Mutation queue, Rules policy, SSE broker          |
+-------------------------------------------------------------------------------+
      |                                                   ^
      | Storage Operations                                | Web Session / API
      v                                                   |
+------------------------------------+  +---------------------------------------+
| TRUST BOUNDARY 3: Local Storage    |  | TRUST BOUNDARY 4: Presentation & UI   |
|   - SQLite DB (WAL mode, 0640)     |  |   - Local Kiosk (127.0.0.1)           |
|   - OAuth Tokens (0600)            |  |   - LAN Admin UI (RFC 1918)           |
|   - Config TOML (0640)             |  |   - CSRF & Rate-limited sessions      |
+------------------------------------+  +---------------------------------------+
```

---

## 2. STRIDE Threat Matrix

| Threat Category | Potential Attack Vector | Impact | Implemented Mitigation |
| :--- | :--- | :--- | :--- |
| **Spoofing** | Attacker spoofs an OAuth callback or attempts cross-account token linkage | Low | `state` parameter is cryptographically signed using HMAC-SHA256 with nonce and timestamp. Email identity is explicitly re-verified against provider API via `VerifyAccountIsolation`. |
| **Spoofing** | Forged sender header in an email to trigger auto-approval of malicious events | Low | Senders must match approved domain/address whitelist in rules engine. Unrecognized senders are relegated to `pending` event candidate queue for manual approval. |
| **Tampering** | Malicious script tags or HTML injections in event titles or email bodies | Medium | Strict HTML stripping (`StripHTML`), safe DOM text rendering, entity escaping in Go HTML templates, and a nonce-free Content Security Policy (`script-src 'self'`). |
| **Tampering** | Local file tampering or directory traversal via ICS attachments | High | Filename paths are sanitized to base names; attachments are not executed; strict filesystem permissions (`0600`/`0640`). |
| **Repudiation** | Unauthorized deletion or alteration of calendar events | Low | Transactional `audit_log` table records all creation, update, delete, sync, and queue dispatch events with timestamps and actor metadata. |
| **Information Disclosure** | Leakage of OAuth refresh tokens or passwords in diagnostic exports | High | Diagnostics bundle engine scrubs sensitive keys (`token`, `secret`, `password`, `key`, `credential`) from output before archiving. |
| **Information Disclosure** | Token theft via physical SD card extraction | Medium | OAuth token files use AES-256-GCM authenticated encryption under a per-installation secret plus restrictive filesystem permissions. Full-disk encryption remains recommended against complete device/config theft. |
| **Spoofing** | A copied or replayed phone-pairing QR grants ongoing access | High | Pairing codes expire after ten minutes, are stored hashed, are consumed atomically once, and issue unique per-device credentials that can be revoked in Admin. |
| **Information Disclosure** | Malicious upload or filename exposes household data | High | Image content and dimensions are decoded and bounded; extensions must match content; random server filenames remove original metadata from URLs; library quotas limit resource exhaustion. |
| **Denial of Service** | Deeply nested MIME multipart bomb or infinite loop in ICS parsing | High | Bounded parsing: maximum 10 recursion levels, 10 MB payload ceiling, and 50 part maximum limit. |
| **Denial of Service** | Flash storage wear-out via rapid database writes | Medium | SQLite WAL mode with 64 MB memory cache, pooled transactions, and batch mutation queue execution. |
| **Elevation of Privilege** | Unauthenticated user attempts to access administrative settings | Medium | All `/admin/*` routes require an active, expiring session verified by a SHA-256 token hash; double-submit CSRF validation protects every state-changing admin operation. |
| **Elevation of Privilege** | Development or CI Linux host is mistaken for appliance hardware and receives reboot/storage commands | High | Privileged hardware controls are enabled only when the device-tree model identifies supported Radxa/Dragon/QCS6490 hardware. |

---

## 3. Mail Parser Blast Radius & Containment

Email messages are untrusted inputs from arbitrary external senders. The parser architecture implements defense-in-depth isolation:

1. **Size Bounding:**
   Incoming payloads exceeding 10 MB are rejected immediately before parsing.
2. **Deterministic Timeouts:**
   All parsing functions operate under an explicit `context.WithTimeout` (default: 5 seconds) to prevent CPU starvation.
3. **LAN AI Fallback Isolation:**
   - Network requests to LAN AI endpoints (e.g. Ollama) validate destination IP addresses against RFC 1918 private ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) or loopback.
   - External WAN IPs and cloud metadata IP (`169.254.169.254`) are blocked, preventing Server-Side Request Forgery (SSRF).
4. **Structured Sanitization:**
   Output fields (`Title`, `Description`, `Location`) are cleaned of ASCII control characters, newlines (in single-line fields), and HTML markup before persistence.
