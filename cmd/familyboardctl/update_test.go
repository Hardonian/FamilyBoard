package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
