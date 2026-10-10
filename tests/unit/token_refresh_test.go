package unit_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	"github.com/Hardonian/FamilyBoard/internal/sync"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func TestWeb_RefreshesExpiringMicrosoftToken(t *testing.T) {
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse refresh form: %v", err)
		}
		if got := r.Form.Get("refresh_token"); got != "old-refresh" {
			t.Errorf("expected old refresh token, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "new-refresh",
		})
	}))
	defer tokenEndpoint.Close()

	tmpDir, err := os.MkdirTemp("", "fb-token-refresh-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(filepath.Join(tmpDir, "test.db"), 5000, 2, 1)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	store := db.NewStore(database)

	tokenStorage, err := oauth.NewTokenStorage(filepath.Join(tmpDir, "credentials"))
	if err != nil {
		t.Fatalf("create token storage: %v", err)
	}
	account := &models.OAuthAccount{
		ID:                  "microsoft-account",
		PersonID:            "person-1",
		Provider:            "microsoft",
		ProviderAccountID:   "ms-user-1",
		VerifiedEmail:       "alex@example.com",
		CredentialReference: "microsoft-account.token.json",
		ExpiresAt:           time.Now().Add(-time.Minute),
		Status:              "active",
	}
	if err := store.UpsertOAuthAccount(ctx, account); err != nil {
		t.Fatalf("store OAuth account: %v", err)
	}
	if err := tokenStorage.Save(account.ID, &oauth.StoredToken{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		Expiry:       time.Now().Add(-time.Minute),
		Email:        account.VerifiedEmail,
	}); err != nil {
		t.Fatalf("store expired token: %v", err)
	}

	microsoftClient := oauth.NewMicrosoftClient("client-id", "client-secret", "http://localhost/callback")
	microsoftClient.SetEndpoints("", tokenEndpoint.URL, "", tokenEndpoint.Client())
	cfg := config.DefaultConfig()
	cfg.Database.Path = database.Path()
	queueEngine := queue.NewEngine(store, nil, 3, time.Second)
	syncEngine := sync.NewEngine(store, nil, time.Minute)
	server, err := web.NewServer(
		cfg,
		store,
		tokenStorage,
		oauth.NewStateManager("test-secret"),
		nil,
		microsoftClient,
		syncEngine,
		queueEngine,
	)
	if err != nil {
		t.Fatalf("create web server: %v", err)
	}

	got, err := server.GetValidAccessToken(ctx, account.PersonID, account.Provider)
	if err != nil {
		t.Fatalf("refresh access token: %v", err)
	}
	if got != "new-access" {
		t.Fatalf("expected refreshed access token, got %q", got)
	}
	stored, err := tokenStorage.Load(account.ID)
	if err != nil {
		t.Fatalf("reload refreshed token: %v", err)
	}
	if stored.RefreshToken != "new-refresh" || stored.AccessToken != "new-access" {
		t.Fatalf("expected rotated credentials to persist, got access=%q refresh=%q", stored.AccessToken, stored.RefreshToken)
	}
	updatedAccount, err := store.GetOAuthAccount(ctx, account.PersonID, account.Provider)
	if err != nil {
		t.Fatalf("reload OAuth account: %v", err)
	}
	if !updatedAccount.ExpiresAt.After(time.Now()) {
		t.Fatalf("expected refreshed expiry metadata, got %s", updatedAccount.ExpiresAt)
	}
}
