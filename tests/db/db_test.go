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
	latest, err := db.LatestVersion()
	if err != nil {
		t.Fatalf("failed to get latest embedded migration: %v", err)
	}
	if ver != latest {
		t.Errorf("expected migration version %d, got %d", latest, ver)
	}

	// Verify Quick Templates seeded
	tmpls, err := store.ListQuickTemplates(ctx)
	if err != nil || len(tmpls) < 9 {
		t.Errorf("expected at least 9 seeded quick templates, got %d (err: %v)", len(tmpls), err)
	}

	// Verify initial family accounts seeded
	people, err := store.ListPeople(ctx)
	if err != nil {
		t.Fatalf("failed to list people: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("expected 2 people, got %d", len(people))
	}

	alex, err := store.GetPersonByEmail(ctx, "alex@example.com")
	if err != nil || alex == nil {
		t.Fatalf("expected Alex to exist: %v", err)
	}
	if alex.DisplayName != "Alex" {
		t.Errorf("expected display name Alex, got %s", alex.DisplayName)
	}

	jordan, err := store.GetPersonByEmail(ctx, "jordan@example.com")
	if err != nil || jordan == nil {
		t.Fatalf("expected Jordan to exist: %v", err)
	}
	if jordan.DisplayName != "Jordan" {
		t.Errorf("expected display name Jordan, got %s", jordan.DisplayName)
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

	// Create oauth account for member 1
	acc := &models.OAuthAccount{
		ID:                  "acc-member-1",
		PersonID:            "person-1",
		Provider:            "google",
		ProviderAccountID:   "google-member-1",
		VerifiedEmail:       "alex@example.com",
		Scopes:              "https://www.googleapis.com/auth/calendar",
		CredentialReference: "tokens/member1.json",
		ExpiresAt:           time.Now().Add(1 * time.Hour),
		Status:              "active",
	}
	if err := store.UpsertOAuthAccount(ctx, acc); err != nil {
		t.Fatalf("failed to upsert oauth account: %v", err)
	}

	// Create calendar
	cal := &models.Calendar{
		ID:                 "cal-primary",
		OAuthAccountID:     acc.ID,
		Provider:           "google",
		ProviderCalendarID: "alex@example.com",
		Name:               "Primary Calendar",
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

func TestStoreApplyRemoteConflictResolutionIsAtomic(t *testing.T) {
	database, store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC()

	account := &models.OAuthAccount{
		ID: "acc-conflict", PersonID: "person-1", Provider: "google", ProviderAccountID: "google-conflict",
		VerifiedEmail: "alex@example.com", Scopes: "calendar", CredentialReference: "test", ExpiresAt: now.Add(time.Hour), Status: "active",
	}
	if err := store.UpsertOAuthAccount(ctx, account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	calendar := &models.Calendar{
		ID: "cal-conflict", OAuthAccountID: account.ID, Provider: "google", ProviderCalendarID: "remote-cal", Name: "Family",
		Timezone: "America/Toronto", DisplayColor: "#3b82f6", EnabledForDisplay: true, Writable: true,
	}
	if err := store.UpsertCalendar(ctx, calendar); err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	local := &models.Event{
		ID: "evt-conflict", Provider: "google", ProviderEventID: "remote-event", CalendarID: calendar.ID, Title: "Local title",
		StartAt: now, EndAt: now.Add(time.Hour), Timezone: "America/Toronto", Status: "confirmed", Source: "local_ui", LocalCreatedAt: now, LocalUpdatedAt: now,
	}
	if err := store.UpsertEvent(ctx, local); err != nil {
		t.Fatalf("create local event: %v", err)
	}
	if err := store.EnqueueMutation(ctx, &models.MutationQueueItem{
		ID: "mut-conflict", Operation: "update", Provider: "google", CalendarID: calendar.ID, LocalEventID: local.ID,
		Payload: "{}", IdempotencyKey: "idemp-conflict", NextAttemptAt: now, State: models.QueuePending,
	}); err != nil {
		t.Fatalf("enqueue local mutation: %v", err)
	}
	if err := store.CreateConflict(ctx, &models.Conflict{
		ID: "conflict-1", LocalEventID: local.ID, RemoteEventID: "remote-event", ConflictType: "concurrent_edit", State: "pending",
	}); err != nil {
		t.Fatalf("create conflict: %v", err)
	}

	remote := *local
	remote.Title = "Remote title"
	remote.ETag = "remote-etag"
	remote.Source = "remote_sync"
	remote.LocalUpdatedAt = now.Add(time.Minute)
	if err := store.ApplyRemoteConflictResolution(ctx, "conflict-1", &remote, "Remote record accepted after review"); err != nil {
		t.Fatalf("apply remote conflict resolution: %v", err)
	}

	resolved, err := store.GetEvent(ctx, local.ID)
	if err != nil || resolved == nil {
		t.Fatalf("load resolved event: %v", err)
	}
	if resolved.Title != "Remote title" || resolved.ETag != "remote-etag" {
		t.Fatalf("remote event was not applied: %#v", resolved)
	}
	pending, err := store.CountPendingMutations(ctx)
	if err != nil || pending != 0 {
		t.Fatalf("expected no pending mutations after accepting remote, got %d (err: %v)", pending, err)
	}
	var state, details string
	if err := database.QueryRowContext(ctx, "SELECT state, resolution FROM conflicts WHERE id = ?", "conflict-1").Scan(&state, &details); err != nil {
		t.Fatalf("load resolved conflict: %v", err)
	}
	if state != "resolved_remote" || details != "Remote record accepted after review" {
		t.Fatalf("unexpected conflict resolution: state=%q details=%q", state, details)
	}
}
