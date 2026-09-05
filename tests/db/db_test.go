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

func setupTestDB(t *testing.T) (*db.DB, *db.Store, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "familyboard-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test db: %v", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to run migrations: %v", err)
	}

	store := db.NewStore(database)

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, store, cleanup
}

func TestDB_MigrationsAndInitialSeeds(t *testing.T) {
	database, store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Verify migrations version
	ver, err := db.CurrentVersion(ctx, database)
	if err != nil {
		t.Fatalf("failed to get current version: %v", err)
	}
	if ver != 2 {
		t.Errorf("expected migration version 2, got %d", ver)
	}

	// Verify Scott & Michelle seeded
	people, err := store.ListPeople(ctx)
	if err != nil {
		t.Fatalf("failed to list people: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("expected 2 people, got %d", len(people))
	}

	scott, err := store.GetPersonByEmail(ctx, "scottrmhardie@gmail.com")
	if err != nil || scott == nil {
		t.Fatalf("expected Scott to exist: %v", err)
	}
	if scott.DisplayName != "Scott" {
		t.Errorf("expected display name Scott, got %s", scott.DisplayName)
	}

	michelle, err := store.GetPersonByEmail(ctx, "michelleyounghardie@gmail.com")
	if err != nil || michelle == nil {
		t.Fatalf("expected Michelle to exist: %v", err)
	}
	if michelle.DisplayName != "Michelle" {
		t.Errorf("expected display name Michelle, got %s", michelle.DisplayName)
	}
}

func TestDB_PragmasAndIntegrity(t *testing.T) {
	database, _, cleanup := setupTestDB(t)
	defer cleanup()

	if err := database.CheckIntegrity(); err != nil {
		t.Errorf("expected integrity check to pass: %v", err)
	}
	if err := database.QuickCheck(); err != nil {
		t.Errorf("expected quick check to pass: %v", err)
	}

	var journalMode string
	row := database.QueryRow("PRAGMA journal_mode;")
	if err := row.Scan(&journalMode); err != nil {
		t.Fatalf("failed to scan journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected wal journal mode, got %s", journalMode)
	}
}

func TestDB_ForeignKeysAndConstraints(t *testing.T) {
	_, store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Inserting calendar with non-existent oauth account should fail foreign key check
	cal := &models.Calendar{
		ID:                 "cal-invalid",
		OAuthAccountID:     "non-existent-account",
		Provider:           "google",
		ProviderCalendarID: "cal123",
		Name:               "Test Calendar",
		Timezone:           "America/Toronto",
		DisplayColor:       "#3b82f6",
		EnabledForDisplay:  true,
		Writable:           true,
	}

	err := store.UpsertCalendar(ctx, cal)
	if err == nil {
		t.Fatalf("expected foreign key constraint violation, got nil error")
	}
}

func TestDB_EventCRUDAndRangeQueries(t *testing.T) {
	database, store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create oauth account for Scott
	acc := &models.OAuthAccount{
		ID:                  "acc-scott",
		PersonID:            "person-scott",
		Provider:            "google",
		ProviderAccountID:   "google-scott-1",
		VerifiedEmail:       "scottrmhardie@gmail.com",
		Scopes:              "https://www.googleapis.com/auth/calendar",
		CredentialReference: "tokens/scott.json",
		ExpiresAt:           time.Now().Add(1 * time.Hour),
		Status:              "active",
	}
	if err := store.UpsertOAuthAccount(ctx, acc); err != nil {
		t.Fatalf("failed to upsert oauth account: %v", err)
	}

	// Create calendar
	cal := &models.Calendar{
		ID:                 "cal-scott-primary",
		OAuthAccountID:     acc.ID,
		Provider:           "google",
		ProviderCalendarID: "scottrmhardie@gmail.com",
		Name:               "Scott's Calendar",
		Timezone:           "America/Toronto",
		DisplayColor:       "#3b82f6",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
	}
	if err := store.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("failed to upsert calendar: %v", err)
	}

	// Create events
	baseTime := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	event1 := &models.Event{
		ID:              "evt-1",
		Provider:        "google",
		ProviderEventID: "pevt-1",
		ProviderUID:     "uid-1@google.com",
		CalendarID:      cal.ID,
		Title:           "Dentist Appointment",
		StartAt:         baseTime,
		EndAt:           baseTime.Add(1 * time.Hour),
		Timezone:        "America/Toronto",
		Status:          "confirmed",
		Source:          "local_ui",
		LocalCreatedAt:  time.Now(),
		LocalUpdatedAt:  time.Now(),
	}
	if err := store.UpsertEvent(ctx, event1); err != nil {
		t.Fatalf("failed to upsert event: %v", err)
	}

	// Query event range
	events, err := store.ListEventsRange(ctx, baseTime.Add(-1*time.Hour), baseTime.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("failed to list events range: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Title != "Dentist Appointment" {
		t.Errorf("expected title 'Dentist Appointment', got '%s'", events[0].Title)
	}

	// Soft delete event
	if err := store.SoftDeleteEvent(ctx, event1.ID); err != nil {
		t.Fatalf("failed to soft delete event: %v", err)
	}

	// Ensure deleted event is excluded from range query
	events, err = store.ListEventsRange(ctx, baseTime.Add(-1*time.Hour), baseTime.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("failed to query after delete: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events after soft delete, got %d", len(events))
	}

	_ = database
}
