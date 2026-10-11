package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReleaseGate_VersionFormat(t *testing.T) {
	data, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatalf("failed to read VERSION file: %v", err)
	}
	version := strings.TrimSpace(string(data))
	semverRegex := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`)
	if !semverRegex.MatchString(version) {
		t.Fatalf("VERSION %q does not match semantic versioning format", version)
	}
}

func TestReleaseGate_InstallerSync(t *testing.T) {
	rootInstaller, err := os.ReadFile("../familyboard-setup.sh")
	if err != nil {
		t.Fatalf("failed to read root familyboard-setup.sh: %v", err)
	}
	piInstaller, err := os.ReadFile("../deploy/pi/familyboard-setup.sh")
	if err != nil {
		t.Fatalf("failed to read deploy/pi/familyboard-setup.sh: %v", err)
	}

	if string(rootInstaller) != string(piInstaller) {
		t.Fatalf("root familyboard-setup.sh and deploy/pi/familyboard-setup.sh have drifted")
	}

	verData, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatalf("failed to read VERSION: %v", err)
	}
	ver := strings.TrimSpace(string(verData))
	expectedDefault := fmt.Sprintf(`VERSION="${FAMILYBOARD_VERSION:-%s}"`, ver)
	if !strings.Contains(string(rootInstaller), expectedDefault) {
		t.Errorf("installer does not contain expected default version string %q", expectedDefault)
	}
}

func TestReleaseGate_ChangelogSync(t *testing.T) {
	verData, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatalf("failed to read VERSION: %v", err)
	}
	ver := strings.TrimSpace(string(verData))

	changelog, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatalf("failed to read CHANGELOG.md: %v", err)
	}

	expectedHeader := fmt.Sprintf("## [%s]", ver)
	if !strings.Contains(string(changelog), expectedHeader) {
		t.Fatalf("CHANGELOG.md is missing expected release section header %q", expectedHeader)
	}
}

func TestReleaseGate_RequiredReleaseFilesExist(t *testing.T) {
	requiredFiles := []string{
		"../VERSION",
		"../LICENSE",
		"../CHANGELOG.md",
		"../README.md",
		"../config.example.toml",
		"../docs/OPERATIONS.md",
		"../docs/RECOVERY_CARD.md",
		"../docs/QUEUE_SEMANTICS.md",
		"../docs/MIGRATIONS.md",
		"../docs/openapi.yaml",
		"../docs/SUPPORT_LIFECYCLE.md",
		"../docs/REMOTE_SUPPORT.md",
		"../docs/LAN_HTTPS.md",
		"../docs/NETWORK_TRUST.md",
		"../docs/SECURITY_AUDIT_READINESS.md",
		"../docs/VOCABULARY.md",
	}

	for _, path := range requiredFiles {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("required release file is missing: %s", path)
		}
	}
}

func TestReleaseGate_ChecksumFormatting(t *testing.T) {
	sampleContent := []byte("familyboard release artifact test")
	sum := sha256.Sum256(sampleContent)
	hashStr := hex.EncodeToString(sum[:])

	// Canonical sha256sum line format: 64 hex characters followed by two spaces and filename
	line := fmt.Sprintf("%s  familyboard-test.tar.gz", hashStr)
	parts := strings.Split(line, "  ")
	if len(parts) != 2 {
		t.Fatalf("expected two spaces separator in checksum line, got: %q", line)
	}
	if len(parts[0]) != 64 {
		t.Fatalf("expected 64 character sha256 hex string, got %d chars: %q", len(parts[0]), parts[0])
	}
}

func TestReleaseGate_ReleaseArchiveLayout(t *testing.T) {
	verData, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatalf("failed to read VERSION: %v", err)
	}
	ver := strings.TrimSpace(string(verData))

	releasesDir := filepath.Join("..", "dist", "releases", ver)
	if _, err := os.Stat(releasesDir); os.IsNotExist(err) {
		t.Skipf("releases directory %s does not exist yet (pre-release build)", releasesDir)
		return
	}

	checksumFile := filepath.Join(releasesDir, "SHA256SUMS")
	data, err := os.ReadFile(checksumFile)
	if err != nil {
		t.Skipf("SHA256SUMS file not found in %s: %v", releasesDir, err)
		return
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		// Match standard sha256 outputs: "<hash>  <filename>" or "<hash> *<filename>" or "<hash> *./<filename>"
		fields := strings.Fields(l)
		if len(fields) != 2 {
			t.Errorf("malformed SHA256SUMS line: %q", l)
			continue
		}
		hash := fields[0]
		fileName := strings.TrimPrefix(fields[1], "*")
		fileName = strings.TrimPrefix(fileName, "./")
		if len(hash) != 64 {
			t.Errorf("invalid hash length in SHA256SUMS line: %q", l)
		}
		archivePath := filepath.Join(releasesDir, fileName)
		if _, err := os.Stat(archivePath); os.IsNotExist(err) {
			t.Errorf("archive referenced in SHA256SUMS does not exist: %s", archivePath)
		}
	}
}
