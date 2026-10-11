# Performance Budgets and Asset Pipeline

Covers P3 #66 and #67.

## Budgets (#66)

Enforced by `tests/frontend_test.js` (run by `make check-frontend` and the
release gate). Sizes are measured raw and gzip level 9.

| Asset | Raw budget | Gzip budget | Current (raw / gzip) |
| --- | --- | --- | --- |
| `kiosk.js` | 160 KB | 40 KB | 114 KB / 24 KB |
| `admin.js` | 80 KB | 22 KB | 29 KB / 6 KB |
| `setup.js` | 16 KB | 5 KB | 10 KB / 3 KB |
| `sw.js` | 12 KB | 4 KB | 3.5 KB / 1 KB |
| `app.css` | 160 KB | 28 KB | 107 KB / 18 KB |
| `kiosk.html` | 60 KB | 12 KB | 36 KB / 8 KB |
| `admin.html` | 100 KB | 16 KB | 61 KB / 13 KB |
| `setup.html` | 20 KB | 5 KB | 12 KB / 3 KB |

Runtime budgets (verified by tests and manual hardware acceptance, not CI):

- Events API for a one-month window across 6 calendars / 12k stored events:
  under 500 ms (measured about 3 ms; `tests/db/db_scale_test.go`).
- A single window never returns more than `db.MaxEventsPerWindow` (5000) rows
  and the API rejects ranges over 366 days.
- Idle kiosk CPU: clock updates every 15 s; no timers under 1 s while idle.

Raising a budget requires a note here stating what feature needed the bytes.

## Asset pipeline (#67)

- Static assets are embedded in the binary and served with content
  fingerprinting (`{{asset "/static/..."}}`), verified by
  `TestWeb_StaticAssetsUseContentFingerprinting`. Responses are gzip-compressed
  at the server (`internal/web/compress.go`).
- Source is shipped unminified on purpose: no build toolchain on the appliance
  or in CI keeps the supply chain small and keeps diagnostics readable.
  Gzip already yields the majority of the benefit (see table).
- No source maps are produced or packaged, so there is nothing private to
  exclude. If a minifier is introduced later it must run in the release job,
  emit no `.map` files into the archive, and keep the budgets above passing.
