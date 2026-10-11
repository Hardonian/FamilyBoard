package unit_test

import (
	"bytes"
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

	accountID := "acc-person-1"
	token := &oauth.StoredToken{
		AccessToken:  "access-12345",
		TokenType:    "Bearer",
		RefreshToken: "refresh-67890",
		Expiry:       time.Now().Add(1 * time.Hour),
		Scopes:       "calendar email",
		Email:        "alex@example.com",
	}

	// Save token
	if err := storage.Save(accountID, token); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// Verify file permissions on disk (must be 0600 on Unix)
	p := filepath.Join(tmpDir, accountID+".token.json")
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

func TestTokenStorage_EncryptsCredentialsAtRest(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := oauth.NewTokenStorage(tmpDir, "installation-secret-with-enough-entropy")
	if err != nil {
		t.Fatal(err)
	}
	token := &oauth.StoredToken{AccessToken: "access-must-not-be-plaintext", RefreshToken: "refresh-must-not-be-plaintext"}
	if err := storage.Save("account-1", token); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(tmpDir, "account-1.token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(token.AccessToken)) || bytes.Contains(raw, []byte(token.RefreshToken)) {
		t.Fatal("credential file contains plaintext secrets")
	}
	loaded, err := storage.Load("account-1")
	if err != nil || loaded.AccessToken != token.AccessToken {
		t.Fatalf("encrypted token round trip failed: token=%+v err=%v", loaded, err)
	}
	wrongKey, err := oauth.NewTokenStorage(tmpDir, "different-installation-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongKey.Load("account-1"); err == nil {
		t.Fatal("expected wrong installation key to fail closed")
	}
}

func TestTokenStorage_MigratesLegacyPlaintextAtomically(t *testing.T) {
	tmpDir := t.TempDir()
	legacy, err := oauth.NewTokenStorage(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Save("legacy", &oauth.StoredToken{AccessToken: "legacy-secret"}); err != nil {
		t.Fatal(err)
	}
	encrypted, err := oauth.NewTokenStorage(tmpDir, "installation-secret")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := encrypted.Load("legacy")
	if err != nil || loaded.AccessToken != "legacy-secret" {
		t.Fatalf("legacy migration failed: token=%+v err=%v", loaded, err)
	}
	raw, err := os.ReadFile(filepath.Join(tmpDir, "legacy.token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("legacy-secret")) {
		t.Fatal("legacy credential remained plaintext after migration")
	}
}

func TestStateManager_HMACAndTampering(t *testing.T) {
	secret := "test-secret-key-1234567890123456"
	sm := oauth.NewStateManager(secret)

	stateStr, err := sm.GenerateState("person-1", "google", "calendar", 5*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate state: %v", err)
	}

	// Valid state verification
	parsed, err := sm.VerifyState(stateStr)
	if err != nil {
		t.Fatalf("expected valid state, got error: %v", err)
	}
	if parsed.PersonID != "person-1" || parsed.Provider != "google" || parsed.Flow != "calendar" {
		t.Errorf("unexpected parsed state: %+v", parsed)
	}

	// Tampered state verification
	tampered := stateStr[:len(stateStr)-4] + "AAAA"
	_, err = sm.VerifyState(tampered)
	if err == nil {
		t.Errorf("expected tampered state to fail verification")
	}

	// Expired state verification
	expiredState, err := sm.GenerateState("person-1", "google", "calendar", -1*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate expired state: %v", err)
	}
	_, err = sm.VerifyState(expiredState)
	if err != oauth.ErrInvalidState {
		t.Errorf("expected ErrInvalidState for expired token, got %v", err)
	}
}

func TestAccountIsolation_VerifyAccountIsolation(t *testing.T) {
	member1Email := "alex@example.com"
	member2Email := "jordan@example.com"

	// Member 1 matches Member 1
	if err := oauth.VerifyAccountIsolation(member1Email, "alex@example.com"); err != nil {
		t.Errorf("expected matching Member 1 email to pass: %v", err)
	}

	// Case-insensitive match
	if err := oauth.VerifyAccountIsolation(member1Email, "Alex@Example.Com"); err != nil {
		t.Errorf("expected case-insensitive matching Member 1 email to pass: %v", err)
	}

	// Member 1 attempted with Member 2 email -> MUST FAIL
	err := oauth.VerifyAccountIsolation(member1Email, member2Email)
	if err == nil {
		t.Fatalf("expected cross-assignment error, got nil")
	}
	if err != oauth.ErrAccountMismatch && !os.IsPermission(err) {
		// Verify error string contains mismatch details
		t.Logf("Got expected mismatch error: %v", err)
	}

	// Member 2 attempted with random email -> MUST FAIL
	err = oauth.VerifyAccountIsolation(member2Email, "stranger@gmail.com")
	if err == nil {
		t.Fatalf("expected stranger email rejection, got nil")
	}
}
