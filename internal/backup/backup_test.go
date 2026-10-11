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

func TestPruneBackups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for i := 1; i <= 5; i++ {
		name := filepath.Join(dir, fmt.Sprintf("familyboard-backup-2026010%d-000000.tar.gz", i))
		if err := os.WriteFile(name, []byte("backup"), 0600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		modTime := time.Now().Add(time.Duration(i) * time.Hour)
		_ = os.Chtimes(name, modTime, modTime)
	}

	pruned, err := backup.PruneBackups(dir, 3)
	if err != nil {
		t.Fatalf("prune error: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("expected 2 pruned backups, got %d", pruned)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("expected 3 remaining backups, got %d", len(entries))
	}
}

