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

	scottEmail := "scottrmhardie@gmail.com"
	michelleEmail := "michelleyounghardie@gmail.com"

	// 1. Connect Scott
	scottToken := &oauth.StoredToken{
		AccessToken:  "scott-access-token-1",
		RefreshToken: "scott-refresh-token-1",
		Expiry:       time.Now().Add(time.Hour),
		Email:        scottEmail,
	}
	_ = storage.Save("acc-scott", scottToken)
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-scott",
		PersonID:            "person-scott",
		Provider:            "google",
		ProviderAccountID:   "g-scott-sub-1",
		VerifiedEmail:       scottEmail,
		Scopes:              oauth.ScopeGoogleCalendar,
		CredentialReference: "acc-scott",
		ExpiresAt:           scottToken.Expiry,
		Status:              "active",
	})

	// 2. Connect Michelle
	michelleToken := &oauth.StoredToken{
		AccessToken:  "michelle-access-token-1",
		RefreshToken: "michelle-refresh-token-1",
		Expiry:       time.Now().Add(time.Hour),
		Email:        michelleEmail,
	}
	_ = storage.Save("acc-michelle", michelleToken)
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-michelle",
		PersonID:            "person-michelle",
		Provider:            "google",
		ProviderAccountID:   "g-michelle-sub-1",
		VerifiedEmail:       michelleEmail,
		Scopes:              oauth.ScopeGoogleCalendar,
		CredentialReference: "acc-michelle",
		ExpiresAt:           michelleToken.Expiry,
		Status:              "active",
	})

	// 3. Verify both accounts are active and isolated
	scottAcc, _ := store.GetOAuthAccount(ctx, "person-scott", "google")
	michelleAcc, _ := store.GetOAuthAccount(ctx, "person-michelle", "google")
	if scottAcc == nil || michelleAcc == nil {
		t.Fatalf("expected both accounts to exist")
	}
	if scottAcc.VerifiedEmail != scottEmail {
		t.Errorf("expected scott email %s, got %s", scottEmail, scottAcc.VerifiedEmail)
	}
	if michelleAcc.VerifiedEmail != michelleEmail {
		t.Errorf("expected michelle email %s, got %s", michelleEmail, michelleAcc.VerifiedEmail)
	}

	// 4. Revoke Scott -> Michelle must remain active and unaffected
	scottAcc.Status = "revoked"
	_ = store.UpsertOAuthAccount(ctx, scottAcc)

	michelleCheck, _ := store.GetOAuthAccount(ctx, "person-michelle", "google")
	if michelleCheck.Status != "active" {
		t.Errorf("expected Michelle to remain active after Scott revocation, got %s", michelleCheck.Status)
	}

	// 5. Refresh Michelle's token -> Scott's stored token remains unchanged
	michelleToken.AccessToken = "michelle-refreshed-token-2"
	_ = storage.Save("acc-michelle", michelleToken)

	scottStored, _ := storage.Load("acc-scott")
	if scottStored.AccessToken != "scott-access-token-1" {
		t.Errorf("expected Scott token to remain unchanged, got %s", scottStored.AccessToken)
	}

	// 6. Cross-Assignment Rejection Test:
	// If a user clicks "Connect Scott" but authenticates with Michelle's email:
	err = oauth.VerifyAccountIsolation(scottEmail, michelleEmail)
	if err == nil {
		t.Fatalf("expected cross-assignment error when Scott receives Michelle's email, got nil")
	}
}
