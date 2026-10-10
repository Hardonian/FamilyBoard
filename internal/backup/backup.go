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
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return "", fmt.Errorf("create backup output dir: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	archiveName := fmt.Sprintf("familyboard-backup-%s.tar.gz", timestamp)
	archivePath := filepath.Join(outDir, archiveName)

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

	// 2. Build tar.gz archive
	outFile, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("create archive file: %w", err)
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

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

	return archivePath, nil
}
