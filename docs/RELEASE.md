# Release Management

FamilyBoard follows [Semantic Versioning 2.0.0](https://semver.org/). The root
`VERSION` file is the authoritative release version. Build metadata is injected
into both executables; unversioned developer builds report `dev`.

## Supported release artifacts

Each release contains static, CGO-free archives and a `SHA256SUMS` manifest:

| Target | Archive |
| --- | --- |
| Linux AMD64 | `familyboard-VERSION-linux-amd64.tar.gz` |
| Linux ARM64 (Raspberry Pi/Radxa) | `familyboard-VERSION-linux-arm64.tar.gz` |
| Windows AMD64 | `familyboard-VERSION-windows-amd64.tar.gz` |
| macOS Apple Silicon | `familyboard-VERSION-darwin-arm64.tar.gz` |

Linux archives include appliance deployment assets. Every archive includes the
daemon, management CLI, example configuration, license, changelog, operations
guide, and recovery card.

## Release qualification

Run the canonical gate from a clean checkout:

```bash
make release-check
```

It checks version consistency, formatting, vet, pinned static analysis and
reachable-vulnerability scanning, module integrity, browser JavaScript syntax,
required standalone runtime data, PowerShell syntax, all tests, the full race
suite, native and Linux ARM64 compilation, shell deployment syntax, and systemd units where
`systemd-analyze` is available. The gate verifies formatting without modifying
the checkout.

Build and checksum every supported artifact with:

```bash
make release-package
cd "dist/releases/$(cat VERSION)"
sha256sum -c SHA256SUMS
```

On Windows with Git Bash, use `mingw32-make` in place of `make`. Generated
archives are written to `dist/releases/VERSION/`.

## Publishing v1.5.0

1. Confirm `VERSION` is `1.5.0` and `CHANGELOG.md` contains a matching dated
   section.
2. Run `make release-package` and test the archive for the target appliance.
3. Commit the release changes, then create and push the annotated tag:

   ```bash
   git tag -a v1.5.0 -m "FamilyBoard v1.5.0"
   git push origin v1.5.0
   ```

4. The `Release` GitHub Actions workflow reruns the complete gate, rebuilds the
   matrix from the tagged source, verifies checksums, and publishes all archives
   plus `SHA256SUMS` to the GitHub release.

The workflow refuses a tag whose name does not equal `v` plus the contents of
`VERSION`, preventing mislabeled binaries.

## Verifying a download

Place `SHA256SUMS` beside the downloaded archives and run:

```bash
sha256sum -c SHA256SUMS
./familyboard --version
```

The reported version, commit, and build date should match the GitHub release.
