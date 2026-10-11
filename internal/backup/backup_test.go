package backup_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/backup"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/restore"
)

func TestCreateAndVerifyBackupAtExplicitPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "familyboard.db"), 5000, 1, 1)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	archivePath := filepath.Join(dir, "verified-backup.tar.gz")
	createdPath, err := backup.CreateBackup(context.Background(), database, "", archivePath)
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if createdPath != archivePath {
		t.Fatalf("created path = %q, want %q", createdPath, archivePath)
	}
	if info, err := os.Stat(archivePath); err != nil || info.Size() == 0 {
		t.Fatalf("backup archive missing or empty: info=%v err=%v", info, err)
	}
	if err := restore.VerifyBackup(context.Background(), archivePath); err != nil {
		t.Fatalf("verify backup: %v", err)
	}
}

func TestVerifyBackupRejectsInvalidArchive(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "invalid-backup.tar.gz")
	if err := os.WriteFile(archivePath, []byte("not a gzip archive"), 0600); err != nil {
		t.Fatalf("write invalid archive: %v", err)
	}
	if err := restore.VerifyBackup(context.Background(), archivePath); err == nil {
		t.Fatal("verify backup succeeded for invalid archive")
	}
}
