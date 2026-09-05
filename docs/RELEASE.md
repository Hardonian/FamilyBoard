# Release Management & Procedures

This document outlines the versioning strategy, release qualification checklist, and artifact distribution procedures for `FamilyBoard`.

---

## 1. Versioning Scheme

`FamilyBoard` follows [Semantic Versioning 2.0.0](https://semver.org/):
- **MAJOR (`vX.0.0`):** Breaking database schema shifts requiring manual migration, incompatible API changes, or fundamental architecture overhauls.
- **MINOR (`v1.X.0`):** New features (e.g. new mail providers, additional UI themes, enhanced extraction rules), backward-compatible schema migrations.
- **PATCH (`v1.0.X`):** Bug fixes, security patches, performance improvements, and documentation updates.

---

## 2. Release Qualification Checklist

Before creating any release tag, all items in this checklist must be verified:

- [ ] **Formatting:** `gofmt -l .` returns zero unformatted files.
- [ ] **Static Analysis:** `go vet ./...` completes with 0 warnings or errors.
- [ ] **Unit & Integration Tests:** `go test -v ./tests/...` passes 100%.
- [ ] **Data Race Detector:** `go test -race ./tests/...` completes with 0 data races.
- [ ] **ARM64 Compilation:** Cross-compilation for `linux/arm64` completes successfully without CGO.
- [ ] **Shell Script Syntax:** `bash -n` validates all scripts in `deploy/pi/`.
- [ ] **Documentation:** `CHANGELOG.md` updated with release notes and migration notices.
- [ ] **Clean Git Workspace:** No uncommitted changes or untracked temporary files.
- [ ] **Canonical Makefile Check:** `make release-check` exits 0.

---

## 3. Step-by-Step Release Procedure

### 1. Run Automated Release Validation
```bash
make release-check
```

### 2. Build Production Release Binaries
```bash
# Build Linux ARM64 static binaries
make build-arm64

# Generate SHA-256 checksums
cd dist
sha256sum familyboard-linux-arm64 familyboardctl-linux-arm64 > SHA256SUMS
cat SHA256SUMS
```

### 3. Create Annotated Git Tag
```bash
git tag -a v1.0.0 -m "Release v1.0.0: Production appliance release for Raspberry Pi 3B"
git push origin v1.0.0
```

### 4. Create GitHub Release
Publish a new GitHub Release with:
- Target tag: `v1.0.0`
- Release title: `FamilyBoard v1.0.0`
- Release notes copied from `CHANGELOG.md`
- Uploaded assets:
  - `familyboard-linux-arm64`
  - `familyboardctl-linux-arm64`
  - `SHA256SUMS`

---

## 4. Verifying Release Artifacts on the Appliance

On the Raspberry Pi:
```bash
# Verify checksums before installing
sha256sum -c SHA256SUMS

# Verify binary architecture
file familyboard-linux-arm64
# Output must indicate: ELF 64-bit LSB executable, ARM aarch64, statically linked
```
