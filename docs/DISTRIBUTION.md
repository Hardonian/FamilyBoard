# Distribution: Images, Channels, and Update Experience

Covers P3 #69, #70, #71, #72, #73.

## Installation image strategy (#69)

Goal: a non-technical household flashes a card and boards boots straight into
setup, without running shell installers.

Plan and status:

| Step | Status |
| --- | --- |
| Shell installers for Pi / Radxa (`deploy/`) with preflight and rollback | Shipped |
| One-shot microSD procedure (`docs/MICROSD_ONE_SHOT.md`) | Shipped |
| Release job builds image from a pinned base OS + the release archive, verified by checksum | **Not built** (needs CI runners with image tooling) |
| Imaging tool (flash + preseed Wi-Fi and device name) | **Not built** |

Acceptance for the image job, when built: boots on supported boards
(`docs/SUPPORT_LIFECYCLE.md`), serves `/health/ready` within 120 s of first
boot, reports the expected version, and has a published SHA-256 attached to the
GitHub release alongside the SBOM.

## Release channels (#70)

| Channel | Source | Audience | Downgrade |
| --- | --- | --- | --- |
| `stable` | tagged `vX.Y.Z` | households | Restore pre-upgrade backup only |
| `beta` | tagged `vX.Y.Z-beta.N` | volunteers | Restore pre-upgrade backup only |
| `dev` | CI builds of `main` (`-dirty` never published) | developers | Not supported |

Rules:

1. Schema migrations are forward-only. A binary refuses to start against a
   database newer than it understands.
2. Every upgrade takes an automatic backup first (`docs/PI_UPGRADE.md`); that
   backup is the only supported downgrade path.
3. A device never moves channel automatically. Moving `beta` to `stable`
   requires a stable version greater than or equal to the installed beta base.
4. Beta releases are never offered to a device configured for `stable`.

## Update experience (#71)

Target flow shown in Admin, in order: release notes (from `CHANGELOG.md`),
expected downtime (typically under 60 s), automatic backup confirmation,
download with checksum verification, install, post-restart health check
(`/health/ready`), and the rollback instruction if health fails.

Status: the underlying steps exist in the installers and upgrade docs; the
Admin one-click surface is **not built**. Until it is, `docs/PI_UPGRADE.md` is
the supported procedure.

## License and dependency notices (#72)

`scripts/generate_notices.sh` writes `THIRD_PARTY_NOTICES.md` for modules
linked into the binaries and exits non-zero if any is GPL/AGPL/SSPL. Modules
whose license it cannot classify are flagged for manual review. Run it as part
of every release; commit the regenerated file.

## Documentation task testing (#73)

`scripts/check_docs.sh` verifies, non-destructively, that links resolve, that
referenced scripts exist, and that every `familyboardctl <command>` named in
docs is implemented. Destructive procedures (fresh install, restore, reimage,
NVMe migration) must be executed exactly as written on a disposable device
before each minor release; record the date, device, version, and result in the
release notes. This manual step cannot be replaced by CI.
