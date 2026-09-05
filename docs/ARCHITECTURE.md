# Architecture & System Design

`FamilyBoard` is an appliance-grade, local-first family calendar and email event ingestion system designed specifically for dedicated wall/refrigerator touchscreen displays (such as a 15-inch display powered by a Raspberry Pi 3B with 1 GB RAM running Raspberry Pi OS 64-bit).

---

## 1. High-Level Design Principles

1. **Sovereign & Local-First:**
   - The appliance operates directly against an embedded SQLite database in WAL (`Write-Ahead Logging`) mode.
   - All calendar events, candidates, mail metadata, and audit logs reside on the device's local filesystem (`/var/lib/familyboard/familyboard.db`).
   - If the home Wi-Fi drops, the local kiosk display continues running seamlessly: creating, editing, and viewing events with zero downtime. Mutations are buffered in a persistent SQLite mutation queue and replayed idempotently when connectivity is restored.

2. **Strict Identity Isolation:**
   - Designed for dual-identity households (Scott: `scottrmhardie@gmail.com` and Michelle: `michelleyounghardie@gmail.com`).
   - Tokens, sync cursors, mail sources, and personal calendars are cryptographically and relationally isolated. Cross-assignment or silent token remapping is blocked at both the API and database constraints levels.

3. **Minimal Resource Footprint (Raspberry Pi 3B / 1 GB RAM):**
   - Pure Go daemon with zero CGO dependencies (`modernc.org/sqlite`).
   - No Docker, no Node.js runtime, no external npm packages, and no CDN dependencies.
   - Server-rendered HTML via embedded Go templates (`embed.FS`) and lightweight, vanilla CSS/JS.
   - Baseline memory footprint is under 20 MB RAM, leaving 980+ MB for OS, graphics, and the Chromium kiosk.

4. **Pi-hole & Local Service Coexistence:**
   - The daemon binds strictly to `127.0.0.1:8080` (or `0.0.0.0:8080` for private LAN administration), preserving standard ports `53` (DNS), `80` (HTTP), and `443` (HTTPS) for Pi-hole, Lighttpd, or reverse proxies.

---

## 2. Component Topology

```text
+-------------------------------------------------------------------------------+
|                             Raspberry Pi 3B Appliance                         |
|                                                                               |
|  +-----------------------------+         +----------------------------------+ |
|  |       Chromium Kiosk        |         |       Remote LAN Browser         | |
|  |  (Touchscreen 1080p Display)|         |       (Admin / Settings)         | |
|  +--------------+--------------+         +----------------+-----------------+ |
|                 | Loopback                                | Private LAN       |
|                 | http://127.0.0.1:8080                   | http://pi.local   |
|                 v                                         v                   |
|  +--------------------------------------------------------------------------+ |
|  |                          FamilyBoard Daemon (Go)                         | |
|  |                                                                          | |
|  |  +------------------------+  +-------------------+  +-----------------+  | |
|  |  |    Web & Kiosk HTTP    |  |  SSE Live Event   |  | CSRF & Session  |  | |
|  |  |      Controllers       |  |      Broker       |  |    Middleware   |  | |
|  |  +-----------+------------+  +---------+---------+  +--------+--------+  | |
|  |              |                         |                     |           | |
|  |              +-------------------------+---------------------+           | |
|  |                                        |                                 | |
|  |  +-------------------+  +--------------v----+  +----------------------+  | |
|  |  |   Calendar Sync   |  |   Mutation Queue  |  |  Mail Intake Engine  |  | |
|  |  |     (Google /     |  | (Durable Replay & |  |  (MIME, ICS, NL,     |  | |
|  |  |    Microsoft)     |  |   Dead-Letter)    |  |   LAN AI Fallback)   |  | |
|  |  +---------+---------+  +---------+---------+  +----------+-----------+  | |
|  |            |                      |                       |              | |
|  |  +---------v----------------------v-----------------------v-----------+  | |
|  |  |                     Data Access Layer (Store)                      |  | |
|  |  +---------------------------------+----------------------------------+  | |
|  |                                    |                                     | |
|  |  +---------------------------------v----------------------------------+  | |
|  |  |                SQLite WAL Database Engine                          |  | |
|  |  |       (/var/lib/familyboard/familyboard.db, busy_timeout=5000)     |  | |
|  |  +--------------------------------------------------------------------+  | |
|  +--------------------------------------------------------------------------+ |
|                                       | Token Files (0600)                    |
|                                       v                                       |
|                    /var/lib/familyboard/tokens/*.json                         |
+---------------------------------------+---------------------------------------+
                                        |
                            Internet / External APIs
                                        v
                    +-------------------+-------------------+
                    | Google Calendar / | Microsoft Graph / |
                    | Gmail API         | Outlook API       |
                    +-------------------+-------------------+
```

---

## 3. Data Flow & Subsystems

### A. Calendar Synchronization Subsystem (`internal/sync`, `internal/calendar`)
- **Direction:** Bidirectional.
- **Remote to Local:**
  - Scheduled background loop (default: every 5 minutes) checks configured Google and Microsoft accounts.
  - Utilizes incremental sync tokens (`syncToken` in Google Calendar, `deltaToken` in Microsoft Graph).
  - Automatically handles `410 Gone` (sync cursor expiration) by flushing stale state and performing a full clean re-sync.
  - Updates SQLite `events` table; broadcasts delta changes to active kiosks via SSE (`/api/kiosk/stream`).
- **Local to Remote (Offline-First):**
  - Kiosk user creates, edits, or deletes an event.
  - Changes immediately commit to local SQLite with an `ephemeral` or `local` flag, providing instant tactile feedback.
  - A corresponding mutation job is transactionally enqueued into the `mutation_queue` table.
  - The queue worker picks up pending items and replays them against the upstream provider using exponential backoff with jitter.

### B. Mail Intake & Event Extraction Subsystem (`internal/candidates`, `internal/mail`)
- **Ingestion:** Polls configured user mailboxes (Gmail REST API, Microsoft Graph Mail) at safe, rate-limited intervals.
- **Deduplication:** High-speed deduplication against `processed_messages` using provider Message-ID and SHA-256 content hashes.
- **Parsing Tier 1 (ICS Attachments):** RFC 5545 compliant recursive parser (`internal/mail/ics`) extracts structured `VEVENT` data, handling line unfolding, recurrence rules, and cancellation notices.
- **Parsing Tier 2 (Natural Language Date/Time):** Rule-based regex parser (`internal/mail/parser/natural`) resolves school calendars, sports practices, and appointments relative to `America/Toronto` timezone.
- **Parsing Tier 3 (LAN AI Fallback):** For unstructured message bodies that fail deterministic parsing, an optional request is sent to a LAN-hosted AI instance (e.g. Ollama/vLLM on a local home server). Strict SSRF filters restrict outbound calls exclusively to private RFC 1918 addresses.
- **Policy Engine:**
  - Events matching trusted senders, known ICS signatures, or high-confidence sports/school rules are auto-approved and written directly to the calendar.
  - Ambiguous or low-confidence extractions are queued in `event_candidates` for one-touch kiosk approval.

### C. Web Presentation & Kiosk Engine (`internal/web`, `web/`)
- **Zero Runtime Dependencies:** No external JS bundles, polyfills, or CSS frameworks. The display UI runs indefinitely without internet access.
- **Server-Side Templates:** Go `html/template` delivers fast, low-memory HTML rendering.
- **Live Updates:** Server-Sent Events (SSE) push calendar mutations and candidate alerts to the kiosk in real time.
- **Touch Targets:** All interactive UI elements have a minimum dimension of `48px x 48px` with clear tactile contrast designed for wall-mounted viewing.

---

## 4. Thread Safety & Concurrency Model

1. **SQLite Concurrency:**
   - SQLite WAL (`PRAGMA journal_mode=WAL;`) allows concurrent readers while a single writer commits transactions.
   - Connection pool is configured with `SetMaxOpenConns(2)` and `SetMaxIdleConns(1)` to minimize lock contention and prevent `database is locked` errors on SD cards.
   - `PRAGMA busy_timeout = 5000;` ensures writes wait up to 5 seconds for SD card I/O spikes before timing out.

2. **Go Goroutines:**
   - Dedicated worker goroutines manage calendar sync, email ingestion, and the mutation queue.
   - Inter-goroutine signaling uses Go channels and standard context cancellation (`context.WithTimeout`, `context.WithCancel`).
   - Clean shutdown signal handling (`SIGINT`, `SIGTERM`) waits for in-flight transactions to drain before terminating.
