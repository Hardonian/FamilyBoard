# Contributing to FamilyBoard

Thank you for contributing to `FamilyBoard`! This document outlines development guidelines, local testing, and coding standards.

---

## 1. Development Prerequisites

- **Go:** Version 1.26.10 or higher. Earlier 1.26 patch releases contain reachable standard-library vulnerabilities and are not supported for release builds.
- **Git:** Modern git client.
- **Make:** GNU Make (or `mingw32-make` on Windows).
- **Bash:** Required for testing deployment scripts (`bash -n`).

---

## 2. Setting Up Local Development

1. **Clone the repository:**

   ```bash
   git clone https://github.com/Hardonian/FamilyBoard.git
   cd FamilyBoard
   ```

2. **Verify dependencies:**

   ```bash
   go mod download
   go mod verify
   ```

3. **Copy example configuration:**

   ```bash
   cp config.example.toml config.toml
   ```

4. **Run the local development server:**

   ```bash
   go run ./cmd/familyboard --config config.toml
   ```

   Open your browser to `http://127.0.0.1:8080/kiosk` or `http://127.0.0.1:8080/admin`.

---

## 3. Testing & Code Quality

Before opening a pull request, run the canonical verification targets:

```bash
# Format code
make fmt

# Static analysis
make vet

# Reachable-vulnerability and advanced static analysis gates
# (install the pinned tools printed by `make tools-check` first)
make security-check

# Run all unit and integration tests
make test

# Run race detector
make test-race

# Verify Linux ARM64 cross-compilation
make build-arm64

# Run all release qualification checks
make release-check
```

---

## 4. Coding Guidelines

- **No CGO Dependencies:** All database drivers and libraries must be pure Go to maintain frictionless cross-compilation for ARM64.
- **Zero Runtime UI Dependencies:** No npm packages, external JS CDNs, or CSS frameworks. All web assets are embedded into the Go binary using `embed.FS`.
- **Timezone Awareness:** All calendar operations, event calculations, and date parsing must explicitly respect the configured timezone (default: `America/Toronto`).
- **Resource Constraints:** Keep memory footprint strictly bounded. Remember the target hardware is a Raspberry Pi 3B with 1 GB of shared RAM.
- **Account Isolation:** Any changes touching authentication, OAuth, tokens, or sync must preserve strict isolation between independent household identities.

---

## 5. Submitting Changes

1. Create a feature branch: `git checkout -b feature/my-new-feature`
2. Commit your changes with clear, imperative commit messages.
3. Ensure all tests pass.
4. Push to your fork and submit a Pull Request against `main`.
