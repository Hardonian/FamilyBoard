package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
)

func TestAccountIsolation_E2E(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fb-isolation-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(filepath.Join(tmpDir, "test.db"), 5000, 2, 1)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(database)

	storage, _ := oauth.NewTokenStorage(filepath.Join(tmpDir, "credentials"))

	member1Email := "alex@example.com"
	member2Email := "jordan@example.com"

	// 1. Connect Member 1
	m1Token := &oauth.StoredToken{
		AccessToken:  "m1-access-token-1",
		RefreshToken: "m1-refresh-token-1",
		Expiry:       time.Now().Add(time.Hour),
		Email:        member1Email,
	}
	_ = storage.Save("acc-member-1", m1Token)
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-member-1",
		PersonID:            "person-1",
		Provider:            "google",
		ProviderAccountID:   "g-m1-sub-1",
		VerifiedEmail:       member1Email,
		Scopes:              oauth.ScopeGoogleCalendar,
		CredentialReference: "acc-member-1",
		ExpiresAt:           m1Token.Expiry,
		Status:              "active",
	})

	// 2. Connect Member 2
	m2Token := &oauth.StoredToken{
		AccessToken:  "m2-access-token-1",
		RefreshToken: "m2-refresh-token-1",
		Expiry:       time.Now().Add(time.Hour),
		Email:        member2Email,
	}
	_ = storage.Save("acc-member-2", m2Token)
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-member-2",
		PersonID:            "person-2",
		Provider:            "google",
		ProviderAccountID:   "g-m2-sub-1",
		VerifiedEmail:       member2Email,
		Scopes:              oauth.ScopeGoogleCalendar,
		CredentialReference: "acc-member-2",
		ExpiresAt:           m2Token.Expiry,
		Status:              "active",
	})

	// 3. Verify both accounts are active and isolated
	m1Acc, _ := store.GetOAuthAccount(ctx, "person-1", "google")
	m2Acc, _ := store.GetOAuthAccount(ctx, "person-2", "google")
	if m1Acc == nil || m2Acc == nil {
		t.Fatalf("expected both accounts to exist")
	}
	if m1Acc.VerifiedEmail != member1Email {
		t.Errorf("expected member 1 email %s, got %s", member1Email, m1Acc.VerifiedEmail)
	}
	if m2Acc.VerifiedEmail != member2Email {
		t.Errorf("expected member 2 email %s, got %s", member2Email, m2Acc.VerifiedEmail)
	}

	// 4. Revoke Member 1 -> Member 2 must remain active and unaffected
	m1Acc.Status = "revoked"
	_ = store.UpsertOAuthAccount(ctx, m1Acc)

	m1Stored, _ := storage.Load("acc-member-1")
	if m1Stored.AccessToken != "m1-access-token-1" {
		t.Errorf("expected Member 1 token to remain unchanged, got %s", m1Stored.AccessToken)
	}

	// 5. Cross-Assignment Rejection Test:
	// If a user clicks "Connect Member 1" but authenticates with Member 2's email:
	err = oauth.VerifyAccountIsolation(member1Email, member2Email)
	if err == nil {
		t.Fatalf("expected cross-assignment error when Member 1 receives Member 2's email, got nil")
	}
}
