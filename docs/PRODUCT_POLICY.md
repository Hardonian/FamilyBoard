# Product Policy, Research, Support, and Branding

Covers P3 #74, #75, #76, #77.

## Analytics policy (#74)

**FamilyBoard has no analytics.** The appliance sends no telemetry, usage
events, crash reports, or identifiers to the project or any third party.
Operational metrics (`/admin/api/health`, support bundle) are local and leave
the device only when a household member downloads and shares them.

If analytics are ever proposed they must be: opt-in with default off,
independently disableable, documented field-by-field here before shipping,
free of event titles/people/email/tokens, and reviewed against
`docs/THREAT_MODEL.md`. Absent that, none will be added.

## User research protocol (#75)

Observe, do not coach. Run with 5 households per round before expanding
features; fix repeated confusion first.

| Task | Success signal | Record |
| --- | --- | --- |
| Complete first-run setup from a phone | Reaches Admin without help | time, step where they paused, errors |
| Add a second person and connect a calendar | Calendar shows on the board | wrong clicks, provider confusion |
| Create, edit, delete, and undo an event on the kiosk | Undo used correctly | hesitation at confirm dialogs |
| Resolve a sync conflict | Picks an option and can explain it | which option, misread consequences |
| Recover from Wi-Fi loss | Understands offline badge and queued changes | what they assumed was lost |

Rules: sessions are recorded only with consent, results contain no household
data, and each finding becomes an issue labeled `research`. Status: protocol
defined; **no sessions run yet**. This item is complete only when rounds are
done, which requires real households.

## Support workflow (#76)

- Issue templates in `.github/ISSUE_TEMPLATE/` (bug, setup help, feature).
- Always request the redacted support bundle (Admin, System, Download redacted
  diagnostics); never ask for tokens, passwords, or event content.
- Response targets: security reports (see `SECURITY.md`) acknowledged in 3
  business days; bugs triaged within 7 days; setup help best effort.
- Escalation: reproducible data-loss or security issue becomes `priority:p0`
  and blocks the next release; supported versions follow
  `docs/SUPPORT_LIFECYCLE.md`.
- Known issues live in `docs/TROUBLESHOOTING.md`.

## Branding and information architecture (#77)

One product name (**FamilyBoard**) and the canonical nouns in
`docs/VOCABULARY.md`. Surfaces and their single purpose:

| Surface | Purpose | Primary action |
| --- | --- | --- |
| Kiosk | Glance and quick edits at the board | Add event |
| Mobile PWA | The same board from a phone | Add event |
| Setup | First-run guided journey | Complete setup |
| Admin | Accounts, calendars, health, privacy | Connect account |
| Recovery | Restore and repair when things fail | Restore backup |

Shared visual language: one design-token set in `web/static/css/app.css`, the
same accent color for primary actions, destructive actions always red and
always confirmed with the affected calendar named, and the same status badge
vocabulary (Synced / Pending / Needs attention / Offline) on every surface.
