package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/config"
)

func TestInstallDaemonBinaryReplacesTargetAndKeepsRollback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "candidate")
	target := filepath.Join(dir, "familyboard")
	if err := os.WriteFile(source, []byte("new-daemon"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old-daemon"), 0755); err != nil {
		t.Fatal(err)
	}

	validatorCalled := false
	version, err := installDaemonBinaryWithValidator(source, target, func(path string) (string, error) {
		validatorCalled = true
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		if string(data) != "new-daemon" {
			t.Fatalf("validator received unexpected staged data %q", data)
		}
		return "FamilyBoard vtest", nil
	})
	if err != nil {
		t.Fatalf("install daemon: %v", err)
	}
	if !validatorCalled {
		t.Fatal("candidate validator was not called")
	}
	if version != "FamilyBoard vtest" {
		t.Fatalf("unexpected version %q", version)
	}

	assertFileContents(t, target, "new-daemon")
	assertFileContents(t, target+".old", "old-daemon")
	if info, err := os.Stat(target); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0100 == 0 {
		t.Fatalf("installed daemon is not executable: mode %v", info.Mode())
	}
}

func TestInstallDaemonBinaryLeavesTargetUntouchedWhenValidationFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "candidate")
	target := filepath.Join(dir, "familyboard")
	if err := os.WriteFile(source, []byte("bad-daemon"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("working-daemon"), 0755); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("invalid candidate")
	_, err := installDaemonBinaryWithValidator(source, target, func(string) (string, error) {
		return "", wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	assertFileContents(t, target, "working-daemon")
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf("rollback file should not exist after validation failure, stat err=%v", err)
	}
}

func TestInstallDaemonBinaryRejectsSamePath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "familyboard")
	if err := os.WriteFile(path, []byte("daemon"), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := installDaemonBinaryWithValidator(path, path, func(string) (string, error) {
		return "FamilyBoard vtest", nil
	}); err == nil {
		t.Fatal("expected same-path update to fail")
	}
	assertFileContents(t, path, "daemon")
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s contains %q, want %q", path, data, want)
	}
}

func TestPrintUpdateStatus(t *testing.T) {
	// Ensure channels and update status prints cleanly without panic
	PrintUpdateStatus(string(ChannelStable))
	PrintUpdateStatus(string(ChannelBeta))
	PrintUpdateStatus(string(ChannelDev))
}

func TestLocalAPIClientUsesLoopbackAndConfiguredTLSRoot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Port = 8443
	client, baseURL, err := localAPIClient(cfg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if baseURL != "http://127.0.0.1:8443" || client.Timeout != time.Second {
		t.Fatalf("unexpected local HTTP client: base=%q timeout=%v", baseURL, client.Timeout)
	}

	certPath := filepath.Join(t.TempDir(), "familyboard.crt")
	keyPath := filepath.Join(filepath.Dir(certPath), "familyboard.key")
	if err := generateSelfSignedCert(certPath, keyPath, "familyboard.local"); err != nil {
		t.Fatal(err)
	}
	cfg.Server.BaseURL = "https://familyboard.local:8443"
	cfg.Server.TLSCertFile = certPath
	cfg.Server.TLSKeyFile = keyPath
	client, baseURL, err = localAPIClient(cfg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatalf("TLS client did not configure certificate verification: %#v", client.Transport)
	}
	if baseURL != "https://127.0.0.1:8443" || transport.TLSClientConfig.ServerName != "familyboard.local" {
		t.Fatalf("unexpected local HTTPS client: base=%q server_name=%q", baseURL, transport.TLSClientConfig.ServerName)
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate")
	contents := []byte("familyboard candidate bytes")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	if err := verifyFileSHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}
	if err := verifyFileSHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("mismatched checksum accepted")
	}
	if err := verifyFileSHA256(path, "invalid"); err == nil {
		t.Fatal("malformed checksum accepted")
	}
}
