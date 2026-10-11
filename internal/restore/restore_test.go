package restore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hardonian/FamilyBoard/internal/backup"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/restore"
)

func setupTestDB(t *testing.T) (*db.DB, *db.Store, string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "restore-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "source.db")
	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("open db: %v", err)
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("migrate: %v", err)
	}

	store := db.NewStore(database)
	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}
	return database, store, tmpDir, cleanup
}

func TestRestore_EndToEndBackupAndRestore(t *testing.T) {
	database, store, tmpDir, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Populate records in source database
	p := &models.Person{
		ID:          "p-restore",
		DisplayName: "Restore User",
		Email:       "restore@example.com",
		Enabled:     true,
	}
	if err := store.UpsertPerson(ctx, p); err != nil {
		t.Fatalf("upsert person: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.toml")
	_ = os.WriteFile(cfgPath, []byte("[server]\nhost = \"127.0.0.1\"\n"), 0600)

	backupOutDir := filepath.Join(tmpDir, "backups")
	archivePath, err := backup.CreateBackup(ctx, database, cfgPath, backupOutDir)
	if err != nil {
		t.Fatalf("create backup failed: %v", err)
	}

	// 2. Verify backup
	if err := restore.VerifyBackup(ctx, archivePath); err != nil {
		t.Fatalf("verify backup failed: %v", err)
	}

	// 3. Restore into clean target destination
	targetDB := filepath.Join(tmpDir, "restored.db")
	targetCfg := filepath.Join(tmpDir, "restored_config.toml")
	if err := os.WriteFile(targetDB, []byte("previous-database-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restore.RestoreBackup(ctx, archivePath, targetDB, targetCfg); err != nil {
		t.Fatalf("restore backup failed: %v", err)
	}
	if previous, err := os.ReadFile(targetDB + ".bak-prerestore"); err != nil || string(previous) != "previous-database-bytes" {
		t.Fatalf("pre-restore rollback copy missing or incorrect: %q err=%v", previous, err)
	}

	// 4. Verify restored DB content
	restoredDatabase, err := db.Open(targetDB, 5000, 2, 1)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer restoredDatabase.Close()

	if err := restoredDatabase.CheckIntegrity(); err != nil {
		t.Fatalf("restored db integrity failed: %v", err)
	}

	restoredStore := db.NewStore(restoredDatabase)
	fetched, err := restoredStore.GetPersonByEmail(ctx, "restore@example.com")
	if err != nil || fetched == nil {
		t.Fatalf("expected restored person, got %v (err: %v)", fetched, err)
	}
	if fetched.DisplayName != "Restore User" {
		t.Errorf("expected Restore User, got %s", fetched.DisplayName)
	}

	// Verify restored config
	cfgBytes, err := os.ReadFile(targetCfg)
	if err != nil || len(cfgBytes) == 0 {
		t.Fatalf("expected restored config to exist and not be empty: %v", err)
	}
}

func TestRestore_InvalidArchiveFailsCleanly(t *testing.T) {
	tmpDir := t.TempDir()
	invalidArchive := filepath.Join(tmpDir, "corrupt.tar.gz")
	_ = os.WriteFile(invalidArchive, []byte("not a tar gz archive"), 0600)

	err := restore.VerifyBackup(context.Background(), invalidArchive)
	if err == nil {
		t.Errorf("expected error on corrupt archive")
	}
}
