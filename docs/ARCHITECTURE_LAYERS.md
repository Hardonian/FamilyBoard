# Architecture Layers and Incremental Updates

Covers P3 #61 (domain/transport separation), #62 (incremental UI updates),
#68 (config hot-reload boundaries).

## Layer rules (#61)

| Layer | Packages | May depend on | Must not depend on |
| --- | --- | --- | --- |
| Transport (HTTP) | `internal/web` | domain, store interfaces, models | provider SDK details |
| Domain / policy | `internal/sync`, `internal/queue`, `internal/rules`, `internal/dedupe`, `internal/recurrence`, `internal/candidates` | `internal/calendar` interface, `internal/db` store, models | `net/http` handlers, templates |
| Provider adapters | `internal/calendar/{google,microsoft,apple}`, `internal/mail/*` | `internal/calendar` interface, `internal/oauth`, models | `internal/db`, `internal/web` |
| Persistence | `internal/db` | models | any higher layer |
| Platform | `internal/hardware`, `internal/watchdog`, `internal/timezone`, `internal/backup` | stdlib | domain, web |

Rules that keep each layer independently testable:

1. Provider adapters speak HTTP to remote APIs only; they never touch SQLite.
2. Sync and queue policy (retry, backoff, conflict detection) talks to providers
   only through `calendar.Provider` and reads capabilities from
   `calendar.CapabilitiesFor`, never from provider-name string checks.
3. Handlers parse/validate input, call domain or store methods, and render.
   They contain no retry or merge policy.
4. `internal/db` returns models; it does not know about HTTP or providers.

Verification: provider packages are tested against `httptest` servers, the queue
and sync engines against fake providers, and `tests/db` against real SQLite.

## Incremental UI updates (#62)

Current behaviour: the kiosk subscribes to `GET /api/events/stream` (SSE) and
refetches the visible window when notified.

Design for revision-based updates (not yet wired; refetch remains the safe
default):

- Each event row already carries `local_updated_at`. A change notice carries
  `{calendar_id, event_ids[], revision}` rather than "something changed".
- The client keeps its window cache keyed by event id and applies the delta; if
  it sees a revision gap or a notice references an id outside its window, it
  falls back to one windowed refetch.
- Notices are bounded (max 100 ids; larger changes send `resync:true`).

This keeps correctness identical to today's refetch (the fallback) while
removing the broad refetch for single-event edits.

## Configuration reload boundaries (#68)

| Setting | Reloads live? | Notes |
| --- | --- | --- |
| Kiosk preferences (default view, time format, birthday highlight) | Yes | Stored in DB, applied on next page load |
| Calendar display/default-write flags | Yes | DB-backed, takes effect immediately |
| Privacy retention days | Yes | Read at each retention pass |
| Google OAuth client id/secret | Yes | Rebuilt in-process on setup/admin save |
| Admin password hash | Yes | Read per login |
| Server listen address / port | **Restart** | Socket bound at startup |
| TLS certificate paths | **Restart** | Loaded at startup |
| Database path and pool sizes | **Restart** | Connection set up at startup |
| Session secret | **Restart** | Invalidates all sessions by design |
| Timezone (`server.timezone`) | **Restart** | Cached by clock/timezone services |

Admin pages that edit a **Restart** setting must show "takes effect after
restart" next to the control.
