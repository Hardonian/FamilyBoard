# Email Event Ingestion & Extraction Pipeline

`FamilyBoard` features an automated, multi-tier email ingestion pipeline that continuously turns unstructured family communications—such as school sports schedules, soccer tournaments, dentist appointments, and party invitations—into structured calendar events.

---

## 1. Pipeline Lifecycle

```text
[ Inbound Message ] (Gmail / Microsoft Graph / Raw EML)
       |
       v
+------------------------------------+
| 1. Ingestion & Size Guard          |  -> Reject if > 10 MB or malformed
+------------------------------------+
       |
       v
+------------------------------------+
| 2. Deduplication Filter            |  -> Skip if Message-ID or Content Hash
+------------------------------------+     already in `processed_messages`
       |
       v
+------------------------------------+
| 3. Safe MIME Unpacking             |  -> Recursion depth <= 10; max 50 parts;
+------------------------------------+     Extract text/plain, text/html, text/calendar
       |
       +---------------------------------------+
       |                                       |
       v                                       v
+------------------------------------+  +------------------------------------+
| 4A. RFC 5545 ICS Extraction        |  | 4B. Natural Language Body Parsing  |
| - Parse VEVENT components          |  | - Sanitize HTML body               |
| - Unfold RFC lines                 |  | - Match date/time regexes          |
| - Resolve RRULE & EXDATE           |  | - Infer start/end in America/Tor.  |
| - Handle CANCELLED status          |  | - Score extraction confidence      |
+------------------------------------+  +------------------------------------+
       |                                       |
       | (If high confidence)                  | (If ambiguous or low confidence)
       |                                       v
       |                                +------------------------------------+
       |                                | 4C. LAN AI Fallback (Optional)     |
       |                                | - Calls private Ollama/vLLM on LAN |
       |                                | - Enforces JSON schema             |
       |                                | - Strict SSRF validation           |
       |                                +------------------------------------+
       |                                       |
       +-------------------+-------------------+
                           |
                           v
+------------------------------------+
| 5. Policy & Rules Engine           |
+------------------------------------+
       |                                       |
       | (Confidence >= Threshold &            | (Confidence < Threshold or
       |  Trusted Sender / Rule Match)         |  Requires Confirmation)
       v                                       v
+------------------------------------+  +------------------------------------+
| 6A. Auto-Approval                  |  | 6B. Candidate Review Queue         |
| - Create in `events`               |  | - Store in `event_candidates`      |
| - Enqueue upstream mutation        |  | - Display badge on Kiosk UI        |
| - Broadcast SSE refresh            |  | - One-touch Accept / Reject        |
+------------------------------------+  +------------------------------------+
```

---

## 2. Ingestion Tiers Detailed

### Tier 1: RFC 5545 ICS Parser (`internal/mail/ics`)
The most accurate method. Handles direct `.ics` calendar attachments:
- **Line Unfolding:** Reconstructs lines folded with leading whitespace (`CRLF + SPACE/TAB`).
- **Escaped Characters:** Decodes `\,`, `\;`, `\\`, and `\n`.
- **Timezone Resolution:** Maps standard VTIMEZONE definitions to IANA identifiers (default: `America/Toronto`).
- **All-Day vs. Timed Events:** Distinguishes `VALUE=DATE` (e.g. `20261015`) from `VALUE=DATE-TIME` (e.g. `20261015T140000Z`).
- **Cancellations:** Detects `STATUS:CANCELLED` or `METHOD:CANCEL` to automatically mark local events as deleted.

### Tier 2: Deterministic Natural Language Parser (`internal/mail/parser/natural`)
Extracts events from email bodies lacking `.ics` attachments:
- **Date Matching:** Recognizes full month names, three-letter abbreviations, numeric dates (`YYYY-MM-DD`, `MM/DD/YYYY`), and relative weekdays ("next Tuesday", "this Friday").
- **Time Window Matching:** Parses time ranges ("from 4:00 PM to 5:30 PM", "7 - 8:30pm", "10am").
- **Context Inference:** If end time is omitted, defaults to a 1-hour duration. If meridian (AM/PM) is ambiguous for hours 1–6, infers PM for typical sports and school events.
- **Subject Extraction:** Cleans noise prefixes (`Re:`, `Fwd:`, `[EXTERNAL]`) to generate clean event titles.

### Tier 3: LAN AI Fallback (`internal/mail/parser/llm`)
For complex, multi-paragraph messages (e.g., coach emails detailing tournament itineraries across several weekends):
- **Local Host Only:** Dispatches to an on-premise LLM server (Ollama, LM Studio, vLLM) on the local home network.
- **SSRF Block:** Destination address must be a private RFC 1918 IPv4 address (`192.168.x.x`, `10.x.x.x`, `172.16.x.x`) or loopback. Cloud metadata and public IPs are strictly blocked.
- **JSON Schema:** The prompt instructs the model to return strictly typed JSON with fields `title`, `start_at`, `end_at`, `location`, `all_day`, and `confidence`.

---

## 3. Policy & Approval Engine (`internal/rules`)

The rules engine evaluates each candidate against configured criteria:

```toml
# /etc/familyboard/config.toml
[rules]
auto_approve_ics = true
auto_approve_trusted_senders = true
min_confidence_auto_approve = 0.85
trusted_senders = [
    "coachmike@soccerclub.org",
    "principal@school.edu",
    "noreply@dentistclinic.com"
]
```

- **Auto-Approved:** Directly inserted into the calendar and pushed to the kiosk display. An audit record is created noting the extraction source and confidence.
- **Pending Review:** Displayed in the Kiosk's candidate banner (`Inbox (N)`). Tapping **Accept** instantly promotes the candidate to the active calendar; tapping **Dismiss** records the rejection to prevent re-prompting.
