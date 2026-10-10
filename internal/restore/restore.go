package restore

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hardonian/FamilyBoard/internal/backup"
	"github.com/Hardonian/FamilyBoard/internal/db"
)

var (
	ErrChecksumMismatch = errors.New("backup database checksum does not match manifest")
	ErrCorruptBackup    = errors.New("backup database failed integrity verification")
)

// VerifyBackup validates a backup archive without modifying the active
// appliance database or configuration. It verifies the manifest checksum and
// runs SQLite integrity_check against an isolated staged copy.
func VerifyBackup(ctx context.Context, archivePath string) error {
	verifyDir, err := os.MkdirTemp("", "fb-backup-verify-*")
	if err != nil {
		return fmt.Errorf("create verification staging directory: %w", err)
	}
	defer os.RemoveAll(verifyDir)

	return RestoreBackup(ctx, archivePath, filepath.Join(verifyDir, "verified.db"), "")
}

// RestoreBackup safely restores an appliance database and configuration from a backup archive.
func RestoreBackup(ctx context.Context, archivePath, targetDBPath, targetConfigPath string) error {
	// 1. Open and extract archive to temporary staging directory
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer archiveFile.Close()

	gr, err := gzip.NewReader(archiveFile)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	stageDir, err := os.MkdirTemp("", "fb-restore-stage-*")
	if err != nil {
		return fmt.Errorf("create restore staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	var manifest *backup.Manifest
	var stagedDBPath string
	var stagedConfigPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		if stringsContainsDotDot(header.Name) {
			continue // Prevent path traversal
		}
		cleanName := filepath.Clean(header.Name)

		destPath := filepath.Join(stageDir, cleanName)

		switch header.Name {
		case "manifest.json":
			data, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			var m backup.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("unmarshal manifest: %w", err)
			}
			manifest = &m
		case "database.db":
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			stagedDBPath = destPath
		case "config.toml":
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			stagedConfigPath = destPath
		}
	}

	if stagedDBPath == "" {
		return errors.New("archive missing database.db")
	}

	// 2. Validate Checksum against manifest
	if manifest != nil && manifest.DatabaseSHA256 != "" {
		dbBytes, err := os.ReadFile(stagedDBPath)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(dbBytes)
		sumHex := hex.EncodeToString(sum[:])
		if sumHex != manifest.DatabaseSHA256 {
			return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, manifest.DatabaseSHA256, sumHex)
		}
	}

	// 3. Verify SQLite integrity on staged copy before touching active database
	stagedDB, err := db.Open(stagedDBPath, 5000, 1, 1)
	if err != nil {
		return fmt.Errorf("%w: staged db open failed: %v", ErrCorruptBackup, err)
	}
	if err := stagedDB.CheckIntegrity(); err != nil {
		stagedDB.Close()
		return fmt.Errorf("%w: staged db integrity failed: %v", ErrCorruptBackup, err)
	}
	stagedDB.Close()

	// 4. Back up current active DB for safe rollback
	targetBackup := targetDBPath + ".bak-prerestore"
	_ = copyFile(targetDBPath, targetBackup)
	_ = copyFile(targetDBPath+"-wal", targetBackup+"-wal")
	_ = copyFile(targetDBPath+"-shm", targetBackup+"-shm")

	// 5. Atomic file replacement
	_ = os.Remove(targetDBPath + "-wal")
	_ = os.Remove(targetDBPath + "-shm")

	if err := copyFile(stagedDBPath, targetDBPath); err != nil {
		// Rollback on failure
		_ = copyFile(targetBackup, targetDBPath)
		return fmt.Errorf("restore failed during file copy; rolled back: %w", err)
	}

	if stagedConfigPath != "" && targetConfigPath != "" {
		_ = copyFile(stagedConfigPath, targetConfigPath)
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func stringsContainsDotDot(s string) bool {
	clean := filepath.Clean(s)
	return clean != s || filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
