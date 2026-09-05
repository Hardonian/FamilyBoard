# Database Schema & Performance Architecture

`FamilyBoard` uses SQLite 3 via the modern, pure-Go driver `modernc.org/sqlite`. The database file resides at `/var/lib/familyboard/familyboard.db`.

---

## 1. Schema Overview & Entity Relational Model

```text
       +------------------+
       |      people      |
       +--------+---------+
                |
    +-----------+-----------+---------------------+
    | 1:N                   | 1:N                 | 1:N
    v                       v                     v
+------------------+  +------------------+  +------------------+
|  oauth_accounts  |  |    calendars     |  |   mail_sources   |
+------------------+  +--------+---------+  +--------+---------+
                               |                     |
                               | 1:N                 | 1:N
                               v                     v
                      +------------------+  +------------------+
                      |      events      |  |processed_messages|
                      +--------+---------+  +------------------+
                               |
                +--------------+--------------+
                | 1:N                         | 1:N
                v                             v
       +------------------+          +------------------+
       |  mutation_queue  |          |    conflicts     |
       +------------------+          +------------------+
```

---

## 2. Table Specifications

### `people`

Stores household members and identity assignments. Pre-seeded with default household member accounts (configured during initial setup).

- `id` (TEXT PRIMARY KEY): Unique person identifier (e.g. `person-1`, `person-2`).
- `display_name` (TEXT NOT NULL): Display name on kiosk.
- `email` (TEXT UNIQUE NOT NULL): Canonical primary email.
- `role` (TEXT NOT NULL): `adult`, `child`, or `guest`.
- `color` (TEXT NOT NULL): Hex color code for UI event badges.
- `avatar_path` (TEXT): Local path to avatar image.

### `oauth_accounts`

Maintains OAuth 2.0 provider tokens and expiration metadata per person.

- `id` (TEXT PRIMARY KEY): Unique identifier.
- `person_id` (TEXT NOT NULL REFERENCES people(id)): Bound person identity.
- `provider` (TEXT NOT NULL): `google` or `microsoft`.
- `account_email` (TEXT NOT NULL): Verified email returned by identity provider.
- `token_path` (TEXT NOT NULL): Secure filesystem path to token JSON (`0600`).
- `scopes` (TEXT NOT NULL): Comma-separated list of approved scopes.
- `status` (TEXT NOT NULL): `active`, `expired`, or `revoked`.

### `calendars`

Configured calendars linked to people.

- `id` (TEXT PRIMARY KEY): Unique local calendar ID.
- `person_id` (TEXT NOT NULL REFERENCES people(id)): Owner.
- `provider` (TEXT NOT NULL): `google`, `microsoft`, or `local`.
- `remote_calendar_id` (TEXT NOT NULL): Upstream provider calendar identifier.
- `name` (TEXT NOT NULL): Calendar title.
- `color` (TEXT NOT NULL): Calendar color override.
- `is_primary` (BOOLEAN NOT NULL DEFAULT 0).
- `sync_cursor` (TEXT): Incremental sync token (`syncToken` or `deltaToken`).
- `last_synced_at` (TIMESTAMP): Last successful sync timestamp.

### `events`

Primary calendar events cache and local storage.

- `id` (TEXT PRIMARY KEY): Local event ID (UUID or prefixed slug).
- `calendar_id` (TEXT NOT NULL REFERENCES calendars(id)).
- `person_id` (TEXT NOT NULL REFERENCES people(id)).
- `remote_event_id` (TEXT): Provider upstream event ID.
- `title` (TEXT NOT NULL): Sanitized summary.
- `description` (TEXT): Sanitized description.
- `location` (TEXT): Physical or virtual location.
- `start_at` (TIMESTAMP NOT NULL): Start date/time.
- `end_at` (TIMESTAMP NOT NULL): End date/time.
- `all_day` (BOOLEAN NOT NULL DEFAULT 0).
- `recurrence_rule` (TEXT): RFC 5545 RRULE string.
- `status` (TEXT NOT NULL): `confirmed`, `tentative`, `cancelled`.
- `source` (TEXT NOT NULL): `remote_sync`, `email_extraction`, `local_ui`.
- `etag` (TEXT): Upstream entity tag.

### `mutation_queue`

Durable queue for offline actions and synchronization retries.

- `id` (TEXT PRIMARY KEY): Mutation ID (`mut-XXXX`).
- `operation` (TEXT NOT NULL): `create`, `update`, `delete`.
- `provider` (TEXT NOT NULL): Destination provider.
- `calendar_id` (TEXT NOT NULL): Target calendar.
- `local_event_id` (TEXT NOT NULL): Event reference.
- `payload` (TEXT NOT NULL): Serialized JSON event DTO.
- `idempotency_key` (TEXT NOT NULL UNIQUE): De-duplication key.
- `attempt_count` (INTEGER NOT NULL DEFAULT 0).
- `next_attempt_at` (TIMESTAMP NOT NULL).
- `state` (TEXT NOT NULL): `pending`, `in_flight`, `retry`, `completed`, `dead_letter`.
- `last_error` (TEXT): Diagnostic failure message.

### `event_candidates`

Pending email extractions awaiting kiosk review.

- `id` (TEXT PRIMARY KEY): Candidate ID (`cand-XXXX`).
- `source_message_id` (TEXT NOT NULL): Originating message ID.
- `person_id` (TEXT NOT NULL REFERENCES people(id)).
- `title` (TEXT NOT NULL).
- `extracted_data` (TEXT NOT NULL): JSON object of extracted fields.
- `confidence` (REAL NOT NULL): Extraction score (0.0 to 1.0).
- `extraction_tier` (TEXT NOT NULL): `ics`, `natural_language`, `lan_ai`.
- `status` (TEXT NOT NULL): `pending`, `approved`, `rejected`.

### `conflicts`

Records synchronization divergences for audit and user review.

- `id` (TEXT PRIMARY KEY).
- `local_event_id` (TEXT NOT NULL).
- `remote_event_id` (TEXT NOT NULL).
- `conflict_type` (TEXT NOT NULL): `concurrent_edit`, `remote_deleted`.
- `local_payload` (TEXT NOT NULL).
- `remote_payload` (TEXT NOT NULL).
- `resolution` (TEXT NOT NULL): `remote_accepted`, `local_accepted`.

### `audit_log`

Immutable transactional audit trail.

- `id` (TEXT PRIMARY KEY).
- `actor` (TEXT NOT NULL): `kiosk`, `sync_worker`, `mail_worker`, `admin`.
- `action` (TEXT NOT NULL): e.g. `event_created`, `oauth_linked`.
- `entity_type` (TEXT NOT NULL).
- `entity_id` (TEXT NOT NULL).
- `details` (TEXT): JSON metadata.
- `created_at` (TIMESTAMP NOT NULL).

---

## 3. Index Strategy

Indexes are defined in `migrations/002_indexes.sql`:

```sql
-- Fast date range queries for Kiosk 1-month and 1-week views
CREATE INDEX IF NOT EXISTS idx_events_range 
ON events(start_at, end_at, status);

-- Rapid lookup by remote ID for synchronization
CREATE INDEX IF NOT EXISTS idx_events_remote 
ON events(calendar_id, remote_event_id);

-- Filter by person/calendar
CREATE INDEX IF NOT EXISTS idx_events_calendar ON events(calendar_id);
CREATE INDEX IF NOT EXISTS idx_events_person ON events(person_id);

-- Mutation queue polling efficiency
CREATE INDEX IF NOT EXISTS idx_mutation_queue_pending 
ON mutation_queue(state, next_attempt_at) 
WHERE state IN ('pending', 'retry');

-- Fast email deduplication
CREATE INDEX IF NOT EXISTS idx_processed_messages_hash 
ON processed_messages(content_hash);
CREATE INDEX IF NOT EXISTS idx_processed_messages_source 
ON processed_messages(source_id, message_id);
```

### Benchmark Performance

- **10,000 active events:** Range query across 1 month executes in **< 5 ms**.
- **Memory footprint:** Under **20 MB RAM** during 10,000-row batch scans.

---

## 4. Migration Architecture

Migrations are versioned SQL scripts embedded into the binary using Go `embed.FS`:

1. The migration runner creates `schema_migrations` if missing.
2. It discovers all files matching `migrations/*.sql` sorted lexically.
3. For each unapplied migration, it begins an immediate transaction, applies the SQL statements, records the version in `schema_migrations`, and commits.
4. If any migration fails, the entire transaction rolls back, and the daemon halts before opening routes.
