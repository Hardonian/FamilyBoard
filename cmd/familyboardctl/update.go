package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/releasefeed"
)

// ReleaseChannel represents release channel tiers (P3 #70).
type ReleaseChannel string

const (
	ChannelStable ReleaseChannel = "stable"
	ChannelBeta   ReleaseChannel = "beta"
	ChannelDev    ReleaseChannel = "dev"
)

// PrintUpdateStatus displays comprehensive release channel and update UX information (P3 #70, #71).
func PrintUpdateStatus(channel string) {
	if channel == "" {
		channel = string(ChannelStable)
	}
	channel = strings.ToLower(channel)
	fmt.Printf("FamilyBoard Appliance Update Status\n\n")
	fmt.Printf("  Installed Version:     v%s\n", buildinfo.Version)
	fmt.Printf("  Active Channel:        %s\n", channel)
	fmt.Printf("  Release Notes:         https://github.com/Hardonian/FamilyBoard/blob/main/CHANGELOG.md\n")
	fmt.Printf("  Expected Downtime:     < 60 seconds (zero data loss)\n")
	fmt.Printf("  Rollback Protection:   Automatic (.old executable and pre-upgrade SQLite snapshot)\n")
	fmt.Printf("  Update Status:         Not checked (run 'familyboardctl update check')\n\n")
	fmt.Printf("Commands:\n")
	fmt.Printf("  familyboardctl update check [--channel stable|beta|dev]\n")
	fmt.Printf("  familyboardctl update [--channel <channel>] [--sha256 <digest>] [--target <daemon-path>] <binary-path>\n")
}

func CheckUpdateStatus(ctx context.Context, channel string) error {
	result, err := releasefeed.Check(ctx, &http.Client{Timeout: 10 * time.Second}, releasefeed.OfficialEndpoint, buildinfo.Version, channel)
	if err != nil {
		return err
	}
	fmt.Printf("FamilyBoard Appliance Update Status\n\n")
	fmt.Printf("  Installed Version:     v%s\n", result.Installed)
	fmt.Printf("  Latest %s Version: %s\n", result.Channel, "v"+result.Latest)
	if result.UpdateAvailable {
		fmt.Printf("  Update Status:         Update available\n")
	} else {
		fmt.Printf("  Update Status:         Up to date\n")
	}
	fmt.Printf("  Release Notes:         %s\n", result.ReleaseURL)
	fmt.Printf("  Checked At:            %s\n", result.CheckedAt.Format(time.RFC3339))
	return nil
}

// defaultDaemonPath resolves the FamilyBoard daemon next to familyboardctl.
// Appliance packages install both binaries into the same directory.
func defaultDaemonPath() (string, error) {
	ctlPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve familyboardctl path: %w", err)
	}
	name := "familyboard"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(ctlPath), name), nil
}

// validateDaemonBinary prevents an accidental install of familyboardctl, an
// unrelated executable, or a binary for the wrong platform.
func validateDaemonBinary(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "-version").CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("validate candidate daemon: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("candidate daemon could not run: %w", err)
	}
	version := strings.TrimSpace(string(out))
	if !strings.HasPrefix(version, "FamilyBoard v") {
		return "", fmt.Errorf("candidate is not a FamilyBoard daemon (unexpected version output %q)", version)
	}
	return version, nil
}

// installDaemonBinary stages the candidate in the destination directory and
// atomically swaps it into place. If a target already exists, a complete
// target.old rollback copy is retained.
func installDaemonBinary(sourcePath, targetPath string) (string, error) {
	return installDaemonBinaryWithValidator(sourcePath, targetPath, validateDaemonBinary)
}

func installDaemonBinaryWithValidator(sourcePath, targetPath string, validate func(string) (string, error)) (string, error) {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", fmt.Errorf("resolve candidate path: %w", err)
	}
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	if samePath(sourceAbs, targetAbs) {
		return "", errors.New("candidate and installed daemon paths must be different")
	}

	source, err := os.Open(sourceAbs)
	if err != nil {
		return "", fmt.Errorf("open candidate daemon: %w", err)
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect candidate daemon: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("candidate daemon must be a regular file")
	}

	targetDir := filepath.Dir(targetAbs)
	if dirInfo, statErr := os.Stat(targetDir); statErr != nil || !dirInfo.IsDir() {
		if statErr != nil {
			return "", fmt.Errorf("inspect target directory: %w", statErr)
		}
		return "", fmt.Errorf("target directory %q is not a directory", targetDir)
	}

	staged, err := os.CreateTemp(targetDir, ".familyboard-new-*")
	if err != nil {
		return "", fmt.Errorf("create staged daemon: %w", err)
	}
	stagedPath := staged.Name()
	keepStaged := false
	defer func() {
		if !keepStaged {
			_ = os.Remove(stagedPath)
		}
	}()

	if _, err := io.Copy(staged, source); err != nil {
		_ = staged.Close()
		return "", fmt.Errorf("copy candidate daemon: %w", err)
	}
	if err := staged.Sync(); err != nil {
		_ = staged.Close()
		return "", fmt.Errorf("sync staged daemon: %w", err)
	}
	if err := staged.Close(); err != nil {
		return "", fmt.Errorf("close staged daemon: %w", err)
	}
	if err := os.Chmod(stagedPath, 0755); err != nil {
		return "", fmt.Errorf("mark staged daemon executable: %w", err)
	}

	// Validate the staged bytes on the appliance. On Windows, CreateProcess
	// requires an executable suffix, so validate the original .exe instead.
	validationPath := stagedPath
	if runtime.GOOS == "windows" {
		validationPath = sourceAbs
	}
	version, err := validate(validationPath)
	if err != nil {
		return "", err
	}

	backupPath := targetAbs + ".old"
	targetExists := false
	if targetInfo, statErr := os.Stat(targetAbs); statErr == nil {
		if !targetInfo.Mode().IsRegular() {
			return "", errors.New("installed daemon target is not a regular file")
		}
		targetExists = true
		if err := copyFileDurably(targetAbs, backupPath, targetInfo.Mode().Perm()); err != nil {
			return "", fmt.Errorf("create rollback copy: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("inspect installed daemon: %w", statErr)
	}

	if err := os.Rename(stagedPath, targetAbs); err != nil {
		// Windows cannot replace an existing executable with Rename. Use a
		// short two-rename window and restore the original on any failure.
		if !targetExists {
			return "", fmt.Errorf("install staged daemon: %w", err)
		}
		displacedPath := targetAbs + ".replacing"
		_ = os.Remove(displacedPath)
		if moveErr := os.Rename(targetAbs, displacedPath); moveErr != nil {
			return "", fmt.Errorf("prepare installed daemon replacement: %w", err)
		}
		if moveErr := os.Rename(stagedPath, targetAbs); moveErr != nil {
			_ = os.Rename(displacedPath, targetAbs)
			return "", fmt.Errorf("install staged daemon: %w", moveErr)
		}
		_ = os.Remove(displacedPath)
	}
	keepStaged = true

	if err := syncDirectory(targetDir); err != nil {
		return "", fmt.Errorf("sync installed daemon directory: %w", err)
	}
	return version, nil
}

func copyFileDurably(sourcePath, targetPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	targetDir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(targetDir, ".familyboard-backup-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func verifyFileSHA256(path, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("expected SHA-256 must be exactly 64 hexadecimal characters")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open candidate for checksum: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash candidate: %w", err)
	}
	actual := hash.Sum(nil)
	if !bytes.Equal(actual, decoded) {
		return fmt.Errorf("candidate SHA-256 mismatch: got %s", hex.EncodeToString(actual))
	}
	return nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
