# Gmail API Integration & Configuration Guide

This document describes how `FamilyBoard` interacts with the Gmail REST API to ingest calendar invitations, school newsletters, and activity schedules.

---

## 1. Architecture: Polling vs. Webhooks

For a sovereign home kitchen appliance running behind a domestic residential router (often behind CGNAT or dynamic IP), **scheduled rate-limited polling** is the recommended, resilient design pattern:

| Feature | Polling (Implemented) | Push Webhooks (Pub/Sub) |
| :--- | :--- | :--- |
| **Network Configuration** | Zero router ports opened, works behind CGNAT / NAT. | Requires public static IP, reverse proxy, or cloud subscription. |
| **Reliability** | Recovers automatically after internet outages. | Dropped webhooks require out-of-band replay loops. |
| **API Quota Usage** | ~50 quota units per poll (well within 1,000,000,000 units/day quota). | Incurs GCP Cloud Pub/Sub overhead and per-message billing. |
| **Latency** | 5 to 15 minutes (configurable). | Sub-minute. |

`FamilyBoard` defaults to a **10-minute polling cycle**, balancing real-time responsiveness with minimal power and network usage.

---

## 2. Permission Scopes & Boundaries

To adhere to the principle of least privilege, `FamilyBoard` requests **strictly read-only access**:

```text
https://www.googleapis.com/auth/gmail.readonly
```

- **What FamilyBoard CAN do:**
  - Retrieve message headers and metadata (Subject, From, Date).
  - Download message MIME bodies and `.ics` attachments for calendar extraction.
- **What FamilyBoard CANNOT do:**
  - Send emails on behalf of any family member.
  - Delete, modify, mark as read, or trash emails.
  - Access user contact lists or Google Drive files.

---

## 3. Query Optimization & Quota Protection

Rather than retrieving every incoming email, `FamilyBoard` applies targeted search filters via the `q` query parameter to reduce bandwidth and SD card write cycles:

```text
after:{timestamp} (has:attachment filename:ics OR "practice" OR "game" OR "rehearsal" OR "schedule" OR "tournament" OR "appointment")
```

### Quota Calculation:
- `users.messages.list`: 5 quota units
- `users.messages.get`: 5 quota units per candidate message
- Polling 2 accounts every 10 minutes (144 polls/day):
  - 144 * 2 * 5 units = **1,440 units/day**
  - Daily free limit: **1,000,000,000 units/day**
  - Consumes less than **0.00015%** of daily quota.

---

## 4. Deduplication & State Tracking

1. **Message-ID Tracking:**
   Every scanned message has its RFC 2822 `Message-ID` or Gmail `id` recorded in `processed_messages`. Subsequent polls immediately skip already-analyzed messages without downloading their body.
2. **Content Hashing:**
   MIME payloads are hashed using SHA-256 to ensure identical messages forwarded across accounts do not create duplicate calendar entries.
3. **Cursor Persistence:**
   The latest processed timestamp is recorded in the `mail_sources` table. If the appliance reboots, it resumes polling from the last saved cursor.
