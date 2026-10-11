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

func setupInvariantDB(t *testing.T) (*db.DB, *db.Store, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "invariant-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "invariant.db")
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
	return database, store, cleanup
}

func TestDBInvariant_ForeignKeyEnforcementAndCascade(t *testing.T) {
	database, store, cleanup := setupInvariantDB(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Try to insert OAuth account pointing to non-existent person ID
	invalidAccount := &models.OAuthAccount{
		ID:                  "oa-invalid",
		PersonID:            "person-does-not-exist",
		Provider:            "google",
		ProviderAccountID:   "acc-inv",
		VerifiedEmail:       "ghost@example.com",
		Scopes:              "openid",
		CredentialReference: "ref",
		ExpiresAt:           time.Now().Add(time.Hour),
		Status:              "active",
	}

	err := store.UpsertOAuthAccount(ctx, invalidAccount)
	if err == nil {
		t.Fatalf("expected foreign key constraint error when inserting orphan OAuth account")
	}

	// 2. Cascade delete test: insert person, account, calendar
	p := &models.Person{ID: "p-cascade", DisplayName: "Cascade", Email: "cascade@example.com"}
	if err := store.UpsertPerson(ctx, p); err != nil {
		t.Fatalf("upsert person: %v", err)
	}
	acc := &models.OAuthAccount{
		ID:                  "oa-cascade",
		PersonID:            "p-cascade",
		Provider:            "google",
		ProviderAccountID:   "acc-casc",
		VerifiedEmail:       "cascade@example.com",
		Scopes:              "openid",
		CredentialReference: "ref",
		ExpiresAt:           time.Now().Add(time.Hour),
		Status:              "active",
	}
	if err := store.UpsertOAuthAccount(ctx, acc); err != nil {
		t.Fatalf("upsert oauth: %v", err)
	}

	cal := &models.Calendar{
		ID:                 "cal-cascade",
		OAuthAccountID:     "oa-cascade",
		Provider:           "google",
		ProviderCalendarID: "pri-casc",
		Name:               "Cascade Cal",
	}
	if err := store.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("upsert cal: %v", err)
	}

	// Delete person; verify account and calendar were CASCADE deleted by SQLite foreign keys
	_, err = database.ExecContext(ctx, "DELETE FROM people WHERE id = 'p-cascade';")
	if err != nil {
		t.Fatalf("delete person: %v", err)
	}

	storedAcc, _ := store.GetOAuthAccountByID(ctx, "oa-cascade")
	if storedAcc != nil {
		t.Errorf("expected oauth account to be cascade deleted")
	}
	storedCal, _ := store.GetCalendar(ctx, "cal-cascade")
	if storedCal != nil {
		t.Errorf("expected calendar to be cascade deleted")
	}
}

func TestDBInvariant_UniquenessConstraints(t *testing.T) {
	_, store, cleanup := setupInvariantDB(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Duplicate email on person
	p1 := &models.Person{ID: "p-u1", DisplayName: "User 1", Email: "duplicate@example.com"}
	if err := store.UpsertPerson(ctx, p1); err != nil {
		t.Fatalf("upsert p1: %v", err)
	}
	p2 := &models.Person{ID: "p-u2", DisplayName: "User 2", Email: "duplicate@example.com"}
	err := store.UpsertPerson(ctx, p2)
	if err == nil {
		t.Errorf("expected uniqueness constraint error on duplicate email")
	}
}

func TestDBInvariant_DefaultWriteCalendar(t *testing.T) {
	_, store, cleanup := setupInvariantDB(t)
	defer cleanup()

	ctx := context.Background()

	if err := store.EnsureDefaultHouseholdCalendar(ctx); err != nil {
		t.Fatalf("ensure default calendar: %v", err)
	}

	defaultCal, err := store.GetDefaultWriteCalendar(ctx)
	if err != nil || defaultCal == nil {
		t.Fatalf("expected default calendar, got %v (err: %v)", defaultCal, err)
	}
	if !defaultCal.DefaultWriteTarget || !defaultCal.Writable {
		t.Errorf("expected default calendar to be marked default write target and writable")
	}
}

func TestDBInvariant_PairingCodeExpirationAndCleanup(t *testing.T) {
	_, store, cleanup := setupInvariantDB(t)
	defer cleanup()

	ctx := context.Background()

	now := time.Now()
	// Create expired code
	expiredToken := "hash-expired"
	if err := store.CreatePairingCode(ctx, expiredToken, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("create pairing code: %v", err)
	}

	// Consuming expired code should return false
	consumed, err := store.ConsumePairingCode(ctx, expiredToken)
	if err != nil {
		t.Fatalf("consume pairing code: %v", err)
	}
	if consumed {
		t.Errorf("expired pairing code should not be consumable")
	}

	// Create valid code
	validToken := "hash-valid"
	if err := store.CreatePairingCode(ctx, validToken, now.Add(10*time.Minute)); err != nil {
		t.Fatalf("create valid pairing code: %v", err)
	}
	consumed, err = store.ConsumePairingCode(ctx, validToken)
	if err != nil {
		t.Fatalf("consume valid code: %v", err)
	}
	if !consumed {
		t.Errorf("valid pairing code should be consumed")
	}

	// Replay attempt should return false
	consumedAgain, err := store.ConsumePairingCode(ctx, validToken)
	if err != nil {
		t.Fatalf("consume again: %v", err)
	}
	if consumedAgain {
		t.Errorf("already consumed pairing code must not be reusable")
	}
}
