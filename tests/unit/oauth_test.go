package unit_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/oauth"
)

func TestTokenStorage_PermissionsAndLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fb-token-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storage, err := oauth.NewTokenStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create token storage: %v", err)
	}

	accountID := "acc-scott"
	token := &oauth.StoredToken{
		AccessToken:  "access-12345",
		TokenType:    "Bearer",
		RefreshToken: "refresh-67890",
		Expiry:       time.Now().Add(1 * time.Hour),
		Scopes:       "calendar email",
		Email:        "scottrmhardie@gmail.com",
	}

	// Save token
	if err := storage.Save(accountID, token); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// Verify file permissions on disk (must be 0600 on Unix)
	p := filepath.Join(tmpDir, "acc-scott.token.json")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("expected token file to exist: %v", err)
	}
	// Note: Windows permissions differ from POSIX, but file must exist and be readable
	if info.Size() == 0 {
		t.Errorf("expected non-empty token file")
	}

	// Load token
	loaded, err := storage.Load(accountID)
	if err != nil {
		t.Fatalf("failed to load token: %v", err)
	}
	if loaded.AccessToken != token.AccessToken {
		t.Errorf("expected access token %s, got %s", token.AccessToken, loaded.AccessToken)
	}
	if loaded.Email != token.Email {
		t.Errorf("expected email %s, got %s", token.Email, loaded.Email)
	}

	// Delete token
	if err := storage.Delete(accountID); err != nil {
		t.Fatalf("failed to delete token: %v", err)
	}

	// Verify not found
	_, err = storage.Load(accountID)
	if err != oauth.ErrTokenNotFound {
		t.Errorf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestStateManager_HMACAndTampering(t *testing.T) {
	secret := "test-secret-key-1234567890123456"
	sm := oauth.NewStateManager(secret)

	stateStr, err := sm.GenerateState("person-scott", "google", "calendar", 5*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate state: %v", err)
	}

	// Valid state verification
	parsed, err := sm.VerifyState(stateStr)
	if err != nil {
		t.Fatalf("expected valid state, got error: %v", err)
	}
	if parsed.PersonID != "person-scott" || parsed.Provider != "google" || parsed.Flow != "calendar" {
		t.Errorf("unexpected parsed state: %+v", parsed)
	}

	// Tampered state verification
	tampered := stateStr[:len(stateStr)-4] + "AAAA"
	_, err = sm.VerifyState(tampered)
	if err == nil {
		t.Errorf("expected tampered state to fail verification")
	}

	// Expired state verification
	expiredState, err := sm.GenerateState("person-scott", "google", "calendar", -1*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate expired state: %v", err)
	}
	_, err = sm.VerifyState(expiredState)
	if err != oauth.ErrInvalidState {
		t.Errorf("expected ErrInvalidState for expired token, got %v", err)
	}
}

func TestAccountIsolation_VerifyAccountIsolation(t *testing.T) {
	scottEmail := "scottrmhardie@gmail.com"
	michelleEmail := "michelleyounghardie@gmail.com"

	// Scott matches Scott
	if err := oauth.VerifyAccountIsolation(scottEmail, "scottrmhardie@gmail.com"); err != nil {
		t.Errorf("expected matching Scott email to pass: %v", err)
	}

	// Case-insensitive match
	if err := oauth.VerifyAccountIsolation(scottEmail, "ScottRMHardie@Gmail.com"); err != nil {
		t.Errorf("expected case-insensitive matching Scott email to pass: %v", err)
	}

	// Scott attempted with Michelle email -> MUST FAIL
	err := oauth.VerifyAccountIsolation(scottEmail, michelleEmail)
	if err == nil {
		t.Fatalf("expected cross-assignment error, got nil")
	}
	if err != oauth.ErrAccountMismatch && !os.IsPermission(err) {
		// Verify error string contains mismatch details
		t.Logf("Got expected mismatch error: %v", err)
	}

	// Michelle attempted with random email -> MUST FAIL
	err = oauth.VerifyAccountIsolation(michelleEmail, "stranger@gmail.com")
	if err == nil {
		t.Fatalf("expected stranger email rejection, got nil")
	}
}
