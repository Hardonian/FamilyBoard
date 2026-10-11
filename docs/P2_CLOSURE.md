# P2 Reliability and Operations Closure

This matrix records the shipped evidence for the P2 reliability and appliance
operations tranche. P3 architecture, distribution-image, and product-policy
work is tracked separately in the documents that explicitly reference P3.

| Item | Outcome | Evidence |
| --- | --- | --- |
| #35 | Provider contract and capability parity | `internal/calendar/provider.go`, `internal/calendar/capabilities.go` |
| #36 | Google Calendar pagination, cursor recovery, CRUD, and stable create IDs | `internal/calendar/google`, provider tests |
| #37 | Microsoft Graph pagination, delta sync, CRUD, and transaction IDs | `internal/calendar/microsoft`, provider tests |
| #38 | Apple Webcal/CalDAV discovery, RFC 5545 CRUD, and stable UIDs | `internal/calendar/apple`, provider tests |
| #39 | OAuth refresh and revoked-account handling | `internal/oauth`, `internal/web/server.go`, token refresh tests |
| #40 | Household identity and provider-account isolation | account isolation and ecosystem interoperability tests |
| #41 | Calendar discovery and multi-provider synchronization | `internal/sync`, integration tests |
| #42 | Incremental cursor recovery without advancing on partial failure | sync engine and fault-injection tests |
| #43 | Atomic local mutation plus durable outbound queue | store transaction helpers and queue semantics tests |
| #44 | Error taxonomy, bounded retry, backoff, and dead-letter recovery | `internal/errdefs`, `internal/queue`, queue UI/tests |
| #45 | Crash replay and provider create idempotency | processing-row recovery plus Google/Microsoft/Apple idempotency tests |
| #46 | Unbounded conflict detection and atomic administrator resolution | sync/store conflict paths and database transaction tests |
| #47 | Bounded recurrence expansion | `internal/recurrence` and recurrence tests |
| #48 | IANA/Windows timezone and DST correctness | `internal/timezone`, provider and recurrence tests |
| #49 | Bounded email parsing, policy approval, and deduplication | `internal/mail`, `internal/candidates`, mail tests |
| #50 | Verified, private, atomic backup generation | `internal/backup`, nightly systemd backup timer |
| #51 | Bounded archive validation, schema checks, atomic restore, and rollback | `internal/restore`, restore tests, recovery docs |
| #52 | Forward migration compatibility and database invariants | migration and invariant test suites |
| #53 | Cheap liveness plus schema/queue-aware readiness | `internal/web/health_handlers.go` |
| #54 | Actionable redacted diagnostics and atomic support bundles | diagnostics, admin support handlers/tests |
| #55 | Scheduled retention, safe manual cleanup, WAL checkpoint, and compaction | `internal/maintenance`, Admin System controls |
| #56 | Clock, timezone, and available NTP-state reporting | `internal/timezone/clock_health.go` |
| #57 | systemd readiness/watchdog and bounded worker shutdown | `internal/watchdog`, queue/sync lifecycle |
| #58 | Correlation IDs and live sync/queue/disk/memory metrics | `internal/web/correlation.go`, `internal/metrics` |
| #59 | Privacy-first remote-support boundary | `docs/REMOTE_SUPPORT.md`, redacted support bundle |
| #60 | Hardware/browser/version support lifecycle | `docs/SUPPORT_LIFECYCLE.md` |

## Closure gates

P2 closes only when `make release-check` passes, provider and ecosystem tests
pass, backup/restore tests pass, the browser suite passes, vulnerability
scanning reports no reachable vulnerability, and all release archives verify
against `SHA256SUMS`.

Remote support does not install a tunnel, public listener, static SSH key, or
vendor backdoor. In this release, the implemented support path is the
household-controlled redacted bundle. Any future live session feature must be
separately threat-modeled and explicitly enabled by the household.
