# Offline-First Architecture & Synchronization Engine

`FamilyBoard` is built from the ground up to be fully functional without active internet connectivity. A kitchen wall display that goes blank or fails to respond because home Wi-Fi is temporarily down is unacceptable.

---

## 1. Storage Engine & WAL Mode

All state is persisted in an embedded SQLite database using modern pure-Go drivers (`modernc.org/sqlite`).

### SQLite Pragmas:
```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA cache_size = -64000;       -- 64 MB RAM cache
PRAGMA mmap_size = 268435456;     -- 256 MB memory-mapped I/O
PRAGMA temp_store = MEMORY;
```

### Rationale:
- **Write-Ahead Logging (WAL):** Allows readers (kiosk display renders, REST queries) to execute concurrently without blocking writers (sync engine, event mutations).
- **Synchronous = NORMAL:** In WAL mode, `NORMAL` is transactionally crash-safe against power failure while significantly reducing microsecond sync calls to the Raspberry Pi's microSD card.
- **Busy Timeout = 5000ms:** If a write transaction encounters a lock during flash I/O stalls, SQLite will automatically retry for up to 5 seconds before returning `busy`.
- **Memory Optimization:** In-memory temp stores and bounded caches ensure memory usage remains strictly under 30 MB on a 1 GB RAM Pi 3B.

---

## 2. Durable Mutation Queue

When a user creates, edits, or deletes an event on the kiosk touchscreen while offline:

```text
+---------------------+
| Kiosk User Action   |
+----------+----------+
           |
           v
+-------------------------------------------------------+
| Atomic SQLite Transaction                             |
|   1. Write Event to `events` table (Status = 'local') |
|   2. Insert Job into `mutation_queue`                 |
|   3. Write Action to `audit_log`                      |
+-------------------------------------------------------+
           |
           v
+---------------------+
| Immediate UI Render | -> User sees immediate update (< 5ms)
+---------------------+
```

### Mutation Queue States:
```text
[ Pending ]
    |
    v
[ In-Flight ] ----( Success )----> [ Completed ] (Archived)
    |
( Network Error / 5xx / 429 )
    v
[ Retry ]
    | (Attempt < 5 with backoff + jitter)
    v
[ In-Flight ]
    |
( Max Retries Exceeded / 400 Bad Request )
    v
[ Dead-Letter ] (Requires Admin Review)
```

### Backoff & Jitter Algorithm:
Failed mutations are retried with exponential backoff:
$$\text{Delay} = \min(\text{InitialDelay} \times 2^{\text{attempt}}, \text{MaxDelay}) + \text{random\_jitter}$$
- `InitialDelay`: 1 second
- `MaxDelay`: 5 minutes
- `MaxRetries`: 5 attempts before quarantine to `dead_letter` status.

---

## 3. Conflict Detection & Resolution

When connectivity returns, the synchronization engine encounters potential three-way concurrency conflicts (e.g. event modified locally on the kitchen display while simultaneously edited remotely on Google Calendar mobile app).

### Resolution Strategy:
1. **Deterministic Last-Write-Wins with Remote Precedence:**
   - If a local event was edited offline and the remote event was also edited upstream:
   - If remote version has a later timestamp or sequence number, remote version updates local display.
   - The displaced local modification is saved to the `conflicts` table, and a notification banner appears on the kiosk: *"Remote update accepted for Soccer Practice (local version preserved in history)"*.
2. **Deletion Symmetry:**
   - If an event is deleted remotely upstream, local status transitions to `cancelled` and is hidden from the primary calendar view.
   - If an event is deleted locally while offline, a `delete` mutation is queued and dispatched upstream upon reconnection.

---

## 4. Sync Token Lifecycle & 410 Gone Recovery

- **Incremental Sync Tokens:** `FamilyBoard` stores `sync_cursor` in the `calendars` table. Polling requests pass this cursor to Google (`syncToken`) and Microsoft (`deltaToken`) to fetch only events changed since the last poll.
- **410 Gone Recovery:**
  - Upstream providers periodically invalidate sync tokens (e.g. after prolonged offline periods exceeding 30 days).
  - When the provider responds with `HTTP 410 Gone`, `FamilyBoard` immediately clears the stale cursor and triggers a full, transaction-safe baseline synchronization without dropping local mutations.
