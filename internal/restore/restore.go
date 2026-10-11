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
	"runtime"
	"strings"

	"github.com/Hardonian/FamilyBoard/internal/backup"
	"github.com/Hardonian/FamilyBoard/internal/db"
)

var (
	ErrChecksumMismatch = errors.New("backup database checksum does not match manifest")
	ErrCorruptBackup    = errors.New("backup database failed integrity verification")
	ErrMissingManifest  = errors.New("backup archive is missing a valid manifest")
	ErrSchemaTooNew     = errors.New("backup schema is newer than this FamilyBoard release")
)

const (
	maxManifestBytes = 1 << 20
	maxConfigBytes   = 4 << 20
	maxDatabaseBytes = 16 << 30
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
	seen := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		if stringsContainsDotDot(header.Name) {
			return fmt.Errorf("backup contains unsafe path %q", header.Name)
		}
		cleanName := filepath.Clean(header.Name)

		destPath := filepath.Join(stageDir, cleanName)

		switch header.Name {
		case "manifest.json":
			if seen[header.Name] {
				return errors.New("backup contains duplicate manifest.json")
			}
			seen[header.Name] = true
			if !isRegularTarEntry(header) || header.Size < 0 || header.Size > maxManifestBytes {
				return errors.New("backup manifest has an invalid type or size")
			}
			data, err := readBoundedEntry(tr, header.Size, maxManifestBytes)
			if err != nil {
				return fmt.Errorf("read backup manifest: %w", err)
			}
			var m backup.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("unmarshal manifest: %w", err)
			}
			manifest = &m
		case "database.db":
			if seen[header.Name] {
				return errors.New("backup contains duplicate database.db")
			}
			seen[header.Name] = true
			if !isRegularTarEntry(header) || header.Size <= 0 || header.Size > maxDatabaseBytes {
				return errors.New("backup database has an invalid type or size")
			}
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(out, tr, header.Size); err != nil {
				out.Close()
				return fmt.Errorf("extract backup database: %w", err)
			}
			if err := out.Sync(); err != nil {
				out.Close()
				return fmt.Errorf("sync staged backup database: %w", err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close staged backup database: %w", err)
			}
			stagedDBPath = destPath
		case "config.toml":
			if seen[header.Name] {
				return errors.New("backup contains duplicate config.toml")
			}
			seen[header.Name] = true
			if !isRegularTarEntry(header) || header.Size < 0 || header.Size > maxConfigBytes {
				return errors.New("backup configuration has an invalid type or size")
			}
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(out, tr, header.Size); err != nil {
				out.Close()
				return fmt.Errorf("extract backup configuration: %w", err)
			}
			if err := out.Sync(); err != nil {
				out.Close()
				return fmt.Errorf("sync staged backup configuration: %w", err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close staged backup configuration: %w", err)
			}
			stagedConfigPath = destPath
		}
	}

	if stagedDBPath == "" {
		return errors.New("archive missing database.db")
	}
	if manifest == nil || manifest.DatabaseSHA256 == "" {
		return ErrMissingManifest
	}
	latestSchema, err := db.LatestVersion()
	if err != nil {
		return fmt.Errorf("read supported schema version: %w", err)
	}
	if manifest.SchemaVersion > latestSchema {
		return fmt.Errorf("%w: backup=%d supported=%d", ErrSchemaTooNew, manifest.SchemaVersion, latestSchema)
	}

	// 2. Validate Checksum against manifest
	dbBytes, err := os.ReadFile(stagedDBPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(dbBytes)
	sumHex := hex.EncodeToString(sum[:])
	if sumHex != manifest.DatabaseSHA256 {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, manifest.DatabaseSHA256, sumHex)
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
	stagedSchema, err := db.CurrentVersion(ctx, stagedDB)
	if err != nil {
		stagedDB.Close()
		return fmt.Errorf("read staged database schema: %w", err)
	}
	if stagedSchema != manifest.SchemaVersion {
		stagedDB.Close()
		return fmt.Errorf("backup manifest schema %d does not match database schema %d", manifest.SchemaVersion, stagedSchema)
	}
	if err := stagedDB.Close(); err != nil {
		return fmt.Errorf("close staged database: %w", err)
	}

	// 4. Back up the active files before publishing the staged restore.
	targetBackup := targetDBPath + ".bak-prerestore"
	dbExisted, err := copyFileIfExists(targetDBPath, targetBackup)
	if err != nil {
		return fmt.Errorf("back up active database: %w", err)
	}
	walExisted, err := copyFileIfExists(targetDBPath+"-wal", targetBackup+"-wal")
	if err != nil {
		return fmt.Errorf("back up active WAL: %w", err)
	}
	shmExisted, err := copyFileIfExists(targetDBPath+"-shm", targetBackup+"-shm")
	if err != nil {
		return fmt.Errorf("back up active shared-memory file: %w", err)
	}

	rollbackDatabase := func() {
		if dbExisted {
			_ = replaceFileAtomically(targetBackup, targetDBPath, 0600)
		} else {
			_ = os.Remove(targetDBPath)
		}
		if walExisted {
			_ = replaceFileAtomically(targetBackup+"-wal", targetDBPath+"-wal", 0600)
		}
		if shmExisted {
			_ = replaceFileAtomically(targetBackup+"-shm", targetDBPath+"-shm", 0600)
		}
	}
	for _, sidecar := range []string{targetDBPath + "-wal", targetDBPath + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			rollbackDatabase()
			return fmt.Errorf("remove stale SQLite sidecar %s: %w", sidecar, err)
		}
	}

	// 5. Publish each file through a synced same-directory temporary file.
	if err := replaceFileAtomically(stagedDBPath, targetDBPath, 0600); err != nil {
		rollbackDatabase()
		return fmt.Errorf("restore failed during file copy; rolled back: %w", err)
	}

	if stagedConfigPath != "" && targetConfigPath != "" {
		configBackup := targetConfigPath + ".bak-prerestore"
		if _, err := copyFileIfExists(targetConfigPath, configBackup); err != nil {
			rollbackDatabase()
			return fmt.Errorf("back up active configuration: %w", err)
		}
		if err := replaceFileAtomically(stagedConfigPath, targetConfigPath, 0600); err != nil {
			rollbackDatabase()
			return fmt.Errorf("restore configuration; database rolled back: %w", err)
		}
	}

	return nil
}

func copyFileIfExists(src, dst string) (bool, error) {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer in.Close()

	if err := replaceReaderAtomically(in, dst, 0600); err != nil {
		return false, err
	}
	return true, nil
}

func replaceFileAtomically(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return replaceReaderAtomically(in, dst, mode)
}

func replaceReaderAtomically(in io.Reader, dst string, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	out, err := os.CreateTemp(dir, ".familyboard-restore-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := out.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		if _, statErr := os.Stat(dst); statErr != nil {
			return err
		}
		displaced := dst + ".replacing"
		_ = os.Remove(displaced)
		if moveErr := os.Rename(dst, displaced); moveErr != nil {
			return fmt.Errorf("move existing target: %w", moveErr)
		}
		if moveErr := os.Rename(tmpPath, dst); moveErr != nil {
			_ = os.Rename(displaced, dst)
			return fmt.Errorf("publish staged file: %w", moveErr)
		}
		_ = os.Remove(displaced)
	}
	if runtime.GOOS != "windows" {
		if directory, err := os.Open(dir); err == nil {
			_ = directory.Sync()
			_ = directory.Close()
		}
	}
	return nil
}

func isRegularTarEntry(header *tar.Header) bool {
	return header.Typeflag == tar.TypeReg || header.Typeflag == 0
}

func readBoundedEntry(reader io.Reader, size, maximum int64) ([]byte, error) {
	if size < 0 || size > maximum {
		return nil, errors.New("archive entry exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(reader, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, errors.New("archive entry size does not match header")
	}
	return data, nil
}

func stringsContainsDotDot(s string) bool {
	clean := filepath.Clean(s)
	return clean != s || filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
