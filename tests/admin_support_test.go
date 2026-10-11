package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func setupTestServer(t *testing.T) (*web.Server, string, func()) {
	tmpDir, err := os.MkdirTemp("", "fb-support-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.Migrate(store.DB()); err != nil {
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to run migrations: %v", err)
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			DeviceName: "Test Appliance",
			Timezone:   "America/New_York",
			Port:       8080,
		},
		Database: config.DatabaseConfig{
			Path: dbPath,
		},
		Auth: config.AuthConfig{
			AdminUsername: "admin",
		},
	}

	tokenStorage := oauth.NewTokenStorage(filepath.Join(tmpDir, "tokens"))
	stateManager := oauth.NewStateManager()
	queueEngine := queue.NewEngine(store, nil, nil, nil, nil)
	syncEngine := calsync.NewEngine(store, tokenStorage, nil, nil, nil, nil, nil)

	srv, err := web.NewServer(cfg, store, tokenStorage, stateManager, nil, nil, syncEngine, queueEngine)
	if err != nil {
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create web server: %v", err)
	}

	// Create valid admin session token
	sessionToken := "admin-test-token-12345678901234567890"
	_ = store.CreateSession(t.Context(), &models.Session{
		TokenHash:    auth.HashToken(sessionToken),
		UserID:       "admin",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	})

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return srv, sessionToken, cleanup
}

func TestAdminAPIHealth(t *testing.T) {
	srv, token, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/admin/api/health", nil)
	req.AddCookie(&http.Cookie{
		Name:  "fb_session",
		Value: token,
	})
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d (%s)", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	if resp["status"] != "ready" && resp["status"] != "degraded" {
		t.Errorf("unexpected status: %v", resp["status"])
	}
	if resp["clock_health"] == nil {
		t.Errorf("missing clock_health in report")
	}
	if resp["metrics"] == nil {
		t.Errorf("missing metrics in report")
	}
}

func TestAdminSupportBundle(t *testing.T) {
	srv, token, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/admin/support/bundle", nil)
	req.AddCookie(&http.Cookie{
		Name:  "fb_session",
		Value: token,
	})
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/zip" {
		t.Errorf("expected application/zip, got: %s", contentType)
	}

	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("failed to read zip bundle: %v", err)
	}

	expectedEntries := map[string]bool{
		"manifest.json":     false,
		"diagnostics.json":  false,
		"metrics.json":      false,
		"clock_health.json": false,
		"config_shape.json": false,
		"recent_audit.json": false,
	}

	for _, f := range zr.File {
		if _, ok := expectedEntries[f.Name]; ok {
			expectedEntries[f.Name] = true
		}
	}

	for name, found := range expectedEntries {
		if !found {
			t.Errorf("missing expected bundle entry: %s", name)
		}
	}
}
