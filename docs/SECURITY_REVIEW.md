# Independent Security Review Handoff

FamilyBoard's internal P1 security controls and automated evidence are implemented. Independent review must be performed by a reviewer who did not author the controls; this document makes that external gate reproducible and prevents it from being mistaken for self-certification.

## Required reviewer scope

1. Authentication, session expiry, login throttling, CSRF, and cross-site request rejection.
2. One-time phone pairing, replay resistance, device revocation, and kiosk/admin authorization boundaries.
3. OAuth state validation, account isolation, encrypted credential storage, rotation, and backup/restore behavior.
4. Calendar/email parser boundaries, SSRF controls, upload decoding/quotas, path handling, and privacy exports.
5. LAN exposure, TLS/reverse-proxy configuration, security headers, logs, diagnostics, and supply-chain workflows.
6. Abuse cases: stolen SD card, hostile guest Wi-Fi client, malicious email/ICS, compromised paired phone, and provider outage.

## Evidence package

Run from a clean checkout:

```sh
make release-check
npm ci
npx playwright install chromium
npm run test:e2e
```

Attach the resulting test log, Playwright report, redacted `/admin/diagnostics` export, release SBOM, and `gh attestation verify` output. Review migrations `001`–`005`, `docs/THREAT_MODEL.md`, and every route registered in `internal/web/server.go`.

## Exit criteria

- No unresolved critical/high findings.
- Medium findings have an owner, mitigation, and due date.
- Pairing replay/revocation and account-isolation tests pass independently.
- A release artifact's SBOM and provenance attestation verify.
- Reviewer records scope, version/commit, dates, methodology, limitations, and signed conclusion.

Until that signed report exists, releases must state **“independent security review pending”** rather than “audited” or “certified.”
