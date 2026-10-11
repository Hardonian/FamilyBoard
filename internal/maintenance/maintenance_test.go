package maintenance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Hardonian/FamilyBoard/internal/db"
)

func TestRunOncePersistsDefaultsAndAuditEvidence(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "maintenance.db"), 5000, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)

	if _, err := RunOnce(ctx, store); err != nil {
		t.Fatalf("run maintenance: %v", err)
	}
	if got, _ := store.GetSetting(ctx, "deleted_event_retention_days"); got != "365" {
		t.Fatalf("deleted-event retention = %q, want 365", got)
	}
	if got, _ := store.GetSetting(ctx, "audit_retention_days"); got != "730" {
		t.Fatalf("audit retention = %q, want 730", got)
	}
	entries, err := store.ListAuditLogs(ctx, 10, 0)
	if err != nil || len(entries) == 0 || entries[0].Action != "retention_maintenance" {
		t.Fatalf("maintenance audit evidence missing: entries=%+v err=%v", entries, err)
	}
}

func TestRunOnceRejectsInvalidLiveRetentionSetting(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "maintenance.db"), 5000, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	if err := store.SetSetting(ctx, "audit_retention_days", "invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOnce(ctx, store); err == nil {
		t.Fatal("maintenance accepted an invalid retention setting")
	}
}
