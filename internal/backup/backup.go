package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/db"
)

type Manifest struct {
	Version        string    `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	DeviceName     string    `json:"device_name"`
	DatabaseSHA256 string    `json:"database_sha256"`
	SchemaVersion  int       `json:"schema_version"`
}

// CreateBackup creates a consistent SQLite backup archive with config and manifest.
func CreateBackup(ctx context.Context, database *db.DB, configPath, outDir string) (string, error) {
	archivePath, err := resolveArchivePath(outDir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(archivePath); err == nil {
		return "", fmt.Errorf("backup archive already exists: %s", archivePath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect backup destination: %w", err)
	}

	// 1. Create a staged SQLite snapshot via VACUUM INTO
	tmpDir, err := os.MkdirTemp("", "fb-backup-stage-*")
	if err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dbSnapshotPath := filepath.Join(tmpDir, "database.db")
	escapedSnapshotPath := strings.ReplaceAll(dbSnapshotPath, "'", "''")

	_, err = database.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s';", escapedSnapshotPath))
	if err != nil {
		return "", fmt.Errorf("execute vacuum into: %w", err)
	}
	stagedDB, err := db.Open(dbSnapshotPath, 5000, 1, 1)
	if err != nil {
		return "", fmt.Errorf("open database snapshot: %w", err)
	}
	if err := stagedDB.CheckIntegrity(); err != nil {
		_ = stagedDB.Close()
		return "", fmt.Errorf("verify database snapshot integrity: %w", err)
	}
	if err := stagedDB.Close(); err != nil {
		return "", fmt.Errorf("close verified database snapshot: %w", err)
	}

	// Calculate DB hash
	dbBytes, err := os.ReadFile(dbSnapshotPath)
	if err != nil {
		return "", fmt.Errorf("read db snapshot: %w", err)
	}
	dbHash := sha256.Sum256(dbBytes)
	dbHashHex := hex.EncodeToString(dbHash[:])

	ver, _ := db.CurrentVersion(ctx, database)

	manifest := Manifest{
		Version:        buildinfo.Version,
		CreatedAt:      time.Now(),
		DeviceName:     "FamilyBoard Appliance",
		DatabaseSHA256: dbHashHex,
		SchemaVersion:  ver,
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")

	// 2. Build the archive beside its final path, then atomically publish it.
	archiveDir := filepath.Dir(archivePath)
	outFile, err := os.CreateTemp(archiveDir, ".familyboard-backup-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary backup archive: %w", err)
	}
	tmpArchivePath := outFile.Name()
	defer os.Remove(tmpArchivePath)
	if err := outFile.Chmod(0600); err != nil {
		outFile.Close()
		return "", fmt.Errorf("secure temporary backup archive: %w", err)
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gw)

	// Add database.db
	dbHeader := &tar.Header{
		Name:    "database.db",
		Size:    int64(len(dbBytes)),
		Mode:    0600,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(dbHeader); err != nil {
		return "", err
	}
	if _, err := tw.Write(dbBytes); err != nil {
		return "", err
	}

	// Add manifest.json
	manifestHeader := &tar.Header{
		Name:    "manifest.json",
		Size:    int64(len(manifestBytes)),
		Mode:    0600,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(manifestHeader); err != nil {
		return "", err
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return "", err
	}

	// Add config.toml if it exists
	if configPath != "" {
		if configBytes, err := os.ReadFile(configPath); err == nil {
			configHeader := &tar.Header{
				Name:    "config.toml",
				Size:    int64(len(configBytes)),
				Mode:    0600,
				ModTime: time.Now(),
			}
			if err := tw.WriteHeader(configHeader); err == nil {
				_, _ = tw.Write(configBytes)
			}
		}
	}

	if err := tw.Close(); err != nil {
		outFile.Close()
		return "", fmt.Errorf("finalize backup archive: %w", err)
	}
	if err := gw.Close(); err != nil {
		outFile.Close()
		return "", fmt.Errorf("compress backup archive: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return "", fmt.Errorf("close backup archive: %w", err)
	}
	if err := os.Rename(tmpArchivePath, archivePath); err != nil {
		return "", fmt.Errorf("publish backup archive: %w", err)
	}

	return archivePath, nil
}

func resolveArchivePath(outPath string) (string, error) {
	if strings.TrimSpace(outPath) == "" {
		outPath = "backups"
	}
	if strings.HasSuffix(strings.ToLower(outPath), ".tar.gz") {
		dir := filepath.Dir(outPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("create backup output dir: %w", err)
		}
		return outPath, nil
	}
	if err := os.MkdirAll(outPath, 0700); err != nil {
		return "", fmt.Errorf("create backup output dir: %w", err)
	}
	timestamp := time.Now().Format("20060102-150405")
	return filepath.Join(outPath, fmt.Sprintf("familyboard-backup-%s.tar.gz", timestamp)), nil
}
