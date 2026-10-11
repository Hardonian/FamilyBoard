package tests_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func setupTestServer(t *testing.T) (*web.Server, *db.Store, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "api-contract-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "contract.db")
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
	_ = store.EnsureDefaultHouseholdCalendar(ctx)

	cfg := config.DefaultConfig()
	cfg.Database.Path = dbPath
	_ = cfg.EnsureSessionSecret()

	tokenStorage, _ := oauth.NewTokenStorage(filepath.Join(tmpDir, "tokens"))
	stateMgr := oauth.NewStateManager(cfg.Security.SessionSecret)
	qEngine := queue.NewEngine(store, nil, 3, 10*time.Second)
	sEngine := calsync.NewEngine(store, nil, 3*time.Minute)

	server, err := web.NewServer(cfg, store, tokenStorage, stateMgr, nil, nil, sEngine, qEngine)
	if err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("new server: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}
	return server, store, cleanup
}

func TestAPIContract_HealthLive(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// GET /health/live
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal live response: %v", err)
	}
	if res["status"] != "alive" {
		t.Errorf("expected status 'alive', got %q", res["status"])
	}

	// POST /health/live should be rejected (405)
	postReq := httptest.NewRequest(http.MethodPost, "/health/live", nil)
	postRec := httptest.NewRecorder()
	server.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on POST /health/live, got %d", postRec.Code)
	}
}

func TestAPIContract_EventsValidationAndMethods(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// GET /api/events with invalid date format -> 400 Bad Request
	req := httptest.NewRequest(http.MethodGet, "/api/events?start=invalid&end=invalid", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid range, got %d", rec.Code)
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("expected JSON error response shape: %v", err)
	}
	if errResp["error"] == "" {
		t.Errorf("expected error field in JSON response, got %+v", errResp)
	}

	// GET /api/events with valid range -> 200 OK and JSON array
	start := time.Now().Format(time.RFC3339)
	end := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	validReq := httptest.NewRequest(http.MethodGet, "/api/events?start="+start+"&end="+end, nil)
	validRec := httptest.NewRecorder()
	server.ServeHTTP(validRec, validReq)

	if validRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", validRec.Code, validRec.Body.String())
	}
	var events []models.Event
	if err := json.Unmarshal(validRec.Body.Bytes(), &events); err != nil {
		t.Fatalf("expected array of events: %v", err)
	}

	// POST /api/events from remote IP without capability -> 401 Unauthorized
	badBody := []byte(`{"title":"","start_at":"2026-10-11T10:00:00Z","end_at":"2026-10-11T11:00:00Z"}`)
	badPostRemote := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(badBody))
	badPostRemote.RemoteAddr = "192.168.1.100:54321"
	badRecRemote := httptest.NewRecorder()
	server.ServeHTTP(badRecRemote, badPostRemote)
	if badRecRemote.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on remote write without capability, got %d", badRecRemote.Code)
	}

	// POST /api/events from loopback kiosk with missing title -> 400 Bad Request
	badPost := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(badBody))
	badPost.Header.Set("Content-Type", "application/json")
	badPost.RemoteAddr = "127.0.0.1:12345"
	badRec := httptest.NewRecorder()
	server.ServeHTTP(badRec, badPost)

	if badRec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on empty title, got %d", badRec.Code)
	}
}

func TestAPIContract_AdminAuthEnforcement(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// GET /admin without session -> Redirect to /login
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Errorf("expected redirect (302/303) to login, got %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Errorf("expected redirect location starting with /login, got %q", rec.Header().Get("Location"))
	}
}

func TestAPIContract_OpenAPISpecValidation(t *testing.T) {
	specPath := filepath.Join("..", "docs", "openapi.yaml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "openapi: 3.0.3") {
		t.Errorf("expected openapi: 3.0.3")
	}
	if !strings.Contains(content, "version: 1.5.0") {
		t.Errorf("expected version: 1.5.0")
	}
	expectedEndpoints := []string{
		"/health/live:",
		"/health/ready:",
		"/api/calendars:",
		"/api/events:",
		"/api/events/{id}:",
		"/api/kiosk/templates:",
		"/api/kiosk/parse-natural:",
	}
	for _, ep := range expectedEndpoints {
		if !strings.Contains(content, ep) {
			t.Errorf("expected openapi spec to document %s", ep)
		}
	}
}
