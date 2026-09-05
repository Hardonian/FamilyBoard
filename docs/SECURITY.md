# Security Policy & Safeguards

This document describes the security model, credential protection, attack mitigations, and data isolation mechanisms built into the `FamilyBoard` appliance.

---

## 1. Identity & Credential Isolation

The `FamilyBoard` appliance is designed for multi-user family households with independent identities (e.g., Scott: `scottrmhardie@gmail.com` and Michelle: `michelleyounghardie@gmail.com`).

### Strict Identity Boundaries
- **No Token Sharing:** Each user's OAuth refresh tokens, access tokens, and sync cursors are stored in separate token files restricted to filesystem permissions `0600` (`-rw-------`).
- **Cryptographic Verification:** During OAuth callback handling, `VerifyAccountIsolation` queries the identity provider's user profile endpoint to verify the returned email matches the expected `person_id`.
- **Silent Remap Prevention:** If Scott attempts to authenticate with Michelle's Google account (or vice versa), the callback is immediately aborted, an audit security event is logged, and the existing token is preserved unmodified.
- **Database Constraints:** Foreign key constraints (`ON DELETE CASCADE`) enforce relation between `oauth_accounts`, `mail_sources`, `calendars`, and their respective `person_id`.

---

## 2. Mail Ingestion Security & Defense-in-Depth

Untrusted email parsing is a critical attack vector for appliances. `FamilyBoard` treats all incoming emails as untrusted hostile data.

### Threat Mitigations:
1. **MIME Parser Depth & Size Limits:**
   - Maximum recursion depth: **10 levels** (prevents stack overflow via deeply nested multipart messages).
   - Maximum message size: **10 MB** (prevents memory exhaustion on 1 GB RAM appliance).
   - Maximum parts processed: **50 parts** (protects against zip/mime bombs).
2. **HTML Sanitization:**
   - All email HTML bodies are sanitized via `StripHTML`, completely stripping:
     - `<script>`, `<iframe>`, `<object>`, `<embed>`, `<style>`, `<link>`, and `<svg>` tags.
     - Dangerous HTML attributes (`onload`, `onerror`, `onclick`, `javascript:` URIs).
   - All text output is entity-escaped before presentation in templates.
3. **ICS (iCalendar) Injection Safeguards:**
   - Line folding and parameter injection protections.
   - Malformed RRULEs, invalid dates, and out-of-bounds timestamps are safely discarded without crashing.
   - Attachment filenames are sanitized to prevent directory traversal (`../../`) vulnerabilities.
4. **LAN AI Fallback (SSRF Prevention):**
   - When calling local AI models (e.g. Ollama), the endpoint URL is strictly validated:
     - Must resolve exclusively to private RFC 1918 subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) or loopback (`127.0.0.0/8`).
     - Cloud metadata endpoints (`169.254.169.254`) and WAN public IPs are rejected.

---

## 3. Web & Kiosk Security

### A. Content Security Policy (CSP) & HTTP Headers
Every HTTP response emitted by `FamilyBoard` includes strict, modern security headers:
- `Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy: geolocation=(), camera=(), microphone=(), payment=()`

### B. Session Security & CSRF Protection
- **Session Tokens:** Cryptographically random 256-bit entropy (`crypto/rand`), stored hashed via SHA-256 in the database.
- **Cookies:** `HttpOnly`, `SameSite=Lax`, and `Path=/`.
- **CSRF Tokens:** Synchronizer token pattern enforced on all state-changing `POST`, `PUT`, and `DELETE` requests.
- **Brute-Force Rate Limiting:** In-memory sliding window rate limiter protects admin login endpoints (5 attempts per minute per IP).

---

## 4. Storage & Filesystem Security

- **Database Filesystem Location:** `/var/lib/familyboard/familyboard.db` owned by unprivileged system user `familyboard:familyboard` with mode `0640`.
- **Token Directory:** `/var/lib/familyboard/tokens/` owned by `familyboard:familyboard` with mode `0700`.
- **Configuration File:** `/etc/familyboard/config.toml` owned by `root:familyboard` with mode `0640`.
- **Log Privacy:** Passwords, OAuth tokens, and client secrets are automatically redacted by the diagnostics scrubber before report export.

---

## 5. Reporting Vulnerabilities

Please report security vulnerabilities directly to the maintainers via private channels:
- Email: `scottrmhardie@gmail.com`
- Please include steps to reproduce, impact assessment, and affected versions.
- Security disclosures will be acknowledged within 48 hours and patched in a critical release.
