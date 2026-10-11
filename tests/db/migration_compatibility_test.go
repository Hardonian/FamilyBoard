package db_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestMigration_CompatibilityStepByStep(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compat-migration-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "compat.db")
	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Load all available embedded migrations
	migrations, err := db.LoadMigrations()
	if err != nil {
		t.Fatalf("failed to load migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatalf("expected embedded migrations to be present")
	}

	// 1. Apply only Migration 1 (v1 schema)
	v1SQL := migrations[0].SQL
	if _, err := database.ExecContext(ctx, v1SQL); err != nil {
		t.Fatalf("failed to execute v1 migration: %v", err)
	}
	_, _ = database.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?);", migrations[0].Version, time.Now())

	// 2. Populate v1 records
	store := db.NewStore(database)
	p := &models.Person{
		ID:          "person-v1",
		DisplayName: "Vintage User",
		Email:       "vintage@example.com",
		Enabled:     true,
	}
	if err := store.UpsertPerson(ctx, p); err != nil {
		t.Fatalf("failed to seed v1 person: %v", err)
	}

	acc := &models.OAuthAccount{
		ID:                  "oa-v1",
		PersonID:            "person-v1",
		Provider:            "local",
		ProviderAccountID:   "v1-account",
		VerifiedEmail:       "vintage@example.com",
		Scopes:              "local",
		CredentialReference: "ref",
		ExpiresAt:           time.Now().AddDate(10, 0, 0),
		Status:              "active",
	}
	if err := store.UpsertOAuthAccount(ctx, acc); err != nil {
		t.Fatalf("failed to seed v1 oauth account: %v", err)
	}

	cal := &models.Calendar{
		ID:                 "cal-v1",
		OAuthAccountID:     "oa-v1",
		Provider:           "local",
		ProviderCalendarID: "local-v1",
		Name:               "Vintage Calendar",
		Timezone:           "America/Toronto",
		EnabledForDisplay:  true,
		Writable:           true,
	}
	if err := store.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("failed to seed v1 calendar: %v", err)
	}

	ev := &models.Event{
		ID:         "ev-v1",
		CalendarID: "cal-v1",
		Title:      "Historic Event",
		StartAt:    time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		EndAt:      time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC),
		Provider:   "local",
	}
	if err := store.UpsertEvent(ctx, ev); err != nil {
		t.Fatalf("failed to seed v1 event: %v", err)
	}

	// 3. Now run standard Migrate (applies all subsequent migrations 002 -> 005)
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("failed to upgrade from v1 to latest schema: %v", err)
	}

	// 4. Verify integrity
	if err := database.CheckIntegrity(); err != nil {
		t.Fatalf("database integrity check failed after upgrade: %v", err)
	}

	// 5. Verify v1 records survived completely intact
	fetchedPerson, err := store.GetPersonByEmail(ctx, "vintage@example.com")
	if err != nil || fetchedPerson == nil {
		t.Fatalf("expected v1 person to survive upgrade, got %v (err: %v)", fetchedPerson, err)
	}
	if fetchedPerson.DisplayName != "Vintage User" {
		t.Errorf("expected Vintage User, got %s", fetchedPerson.DisplayName)
	}

	fetchedEv, err := store.GetEvent(ctx, "ev-v1")
	if err != nil || fetchedEv == nil {
		t.Fatalf("expected v1 event to survive upgrade, got %v (err: %v)", fetchedEv, err)
	}
	if fetchedEv.Title != "Historic Event" {
		t.Errorf("expected Historic Event, got %s", fetchedEv.Title)
	}

	// 6. Verify newly added tables (paired_devices, privacy_exports) from migration 005 are functional
	var tableCount int
	_ = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM paired_devices;").Scan(&tableCount)
	if tableCount != 0 {
		t.Errorf("expected empty paired_devices table, got %d", tableCount)
	}

	// 7. Verify version reaches latest
	latestVer, _ := db.LatestVersion()
	curVer, _ := db.CurrentVersion(ctx, database)
	if curVer != latestVer {
		t.Errorf("expected current version %d to equal latest version %d", curVer, latestVer)
	}
}
