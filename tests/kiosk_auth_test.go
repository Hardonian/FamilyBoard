package tests

import (
	"bytes"
	"context"
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

func setupTestServer(t *testing.T) (*web.Server, *db.Store, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "fb-kiosk-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := db.NewStore(database)
	tokenStorage, err := oauth.NewTokenStorage(filepath.Join(tempDir, "tokens"))
	if err != nil {
		t.Fatalf("token storage: %v", err)
	}
	stateManager := oauth.NewStateManager("test-secret-key-1234567890123456")

	cfg := config.DefaultConfig()
	cfg.Security.SessionSecret = "test-secret-kiosk-123456789012"
	cfg.Auth.AdminUsername = "admin"
	hash, _ := auth.HashPassword("secretpass123")
	cfg.Auth.AdminPasswordHash = hash

	// Pre-seed writable and read-only calendars
	now := time.Now()
	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 "cal-writable-family",
		OAuthAccountID:     "acc-dummy",
		Provider:           "google",
		ProviderCalendarID: "fam@gmail.com",
		Name:               "Family Calendar",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	})

	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 "cal-readonly-holidays",
		OAuthAccountID:     "acc-dummy",
		Provider:           "google",
		ProviderCalendarID: "holidays@gmail.com",
		Name:               "US Holidays",
		EnabledForDisplay:  true,
		Writable:           false,
		DefaultWriteTarget: false,
		CreatedAt:          now,
		UpdatedAt:          now,
	})

	// Metadata
	_ = store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Kitchen Kiosk Test",
		Timezone:      "America/Toronto",
		InitializedAt: now,
		Version:       "v1.0.0",
	})

	queueEngine := queue.NewEngine(store, nil, 3, 10*time.Second)
	syncEngine := calsync.NewEngine(store, nil, 10*time.Minute)

	srv, err := web.NewServer(cfg, store, tokenStorage, stateManager, nil, nil, syncEngine, queueEngine)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	return srv, store, tempDir
}

func TestKioskAuthorization_LocalhostPasswordlessAccess(t *testing.T) {
	srv, store, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	// 1. Localhost kiosk accesses GET / -> succeeds and receives fb_kiosk_cap cookie
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for localhost kiosk GET /, got %d", rec.Code)
	}

	var kioskCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "fb_kiosk_cap" {
			kioskCookie = c
			break
		}
	}
	if kioskCookie == nil {
		t.Fatalf("expected fb_kiosk_cap cookie to be set for localhost kiosk")
	}

	// 2. Physical localhost kiosk adds event without password -> succeeds
	eventPayload := map[string]interface{}{
		"calendar_id": "cal-writable-family",
		"title":       "Soccer Practice",
		"start_at":    time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"end_at":      time.Now().Add(25 * time.Hour).Format(time.RFC3339),
		"all_day":     false,
	}
	body, _ := json.Marshal(eventPayload)

	postReq := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(body))
	postReq.RemoteAddr = "127.0.0.1:54321" // Localhost physical display
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()

	srv.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for localhost event creation, got %d (body: %s)", postRec.Code, postRec.Body.String())
	}

	var created models.Event
	if err := json.NewDecoder(postRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created event: %v", err)
	}
	if created.Title != "Soccer Practice" || created.Source != "kiosk" {
		t.Errorf("unexpected created event: title=%s source=%s", created.Title, created.Source)
	}

	// 3. Localhost kiosk edits event -> succeeds
	editPayload := map[string]interface{}{
		"title": "Soccer Practice (Updated)",
	}
	editBody, _ := json.Marshal(editPayload)
	putReq := httptest.NewRequest(http.MethodPut, "/api/events/"+created.ID, bytes.NewReader(editBody))
	putReq.RemoteAddr = "127.0.0.1:54321"
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()

	srv.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for localhost event edit, got %d", putRec.Code)
	}

	// 4. Localhost kiosk deletes event -> succeeds
	delReq := httptest.NewRequest(http.MethodDelete, "/api/events/"+created.ID, nil)
	delReq.RemoteAddr = "127.0.0.1:54321"
	delRec := httptest.NewRecorder()

	srv.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for localhost event delete, got %d", delRec.Code)
	}

	// Verify event is soft-deleted
	ev, _ := store.GetEvent(context.Background(), created.ID)
	if ev.Status != "cancelled" || ev.DeletedAt == nil {
		t.Errorf("expected event to be cancelled/deleted, got status=%s deletedAt=%v", ev.Status, ev.DeletedAt)
	}

	// 5. Restore event via Undo -> succeeds
	restoreReq := httptest.NewRequest(http.MethodPost, "/api/events/"+created.ID+"/restore", nil)
	restoreReq.RemoteAddr = "127.0.0.1:54321"
	restoreRec := httptest.NewRecorder()

	srv.ServeHTTP(restoreRec, restoreReq)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for localhost event restore, got %d", restoreRec.Code)
	}
	restored, _ := store.GetEvent(context.Background(), created.ID)
	if restored.Status != "confirmed" || restored.DeletedAt != nil {
		t.Errorf("expected event to be restored, got status=%s", restored.Status)
	}
}

func TestKioskAuthorization_RemoteLANBoundary(t *testing.T) {
	srv, _, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	// 1. Remote LAN device (e.g. 192.168.1.100) attempts unauthenticated write -> rejected with 401 Unauthorized
	eventPayload := map[string]interface{}{
		"calendar_id": "cal-writable-family",
		"title":       "Unauthorized LAN Event",
		"start_at":    time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"end_at":      time.Now().Add(25 * time.Hour).Format(time.RFC3339),
	}
	body, _ := json.Marshal(eventPayload)

	postReq := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(body))
	postReq.RemoteAddr = "192.168.1.100:54321" // Remote LAN IP
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()

	srv.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unauthenticated remote LAN event creation, got %d (body: %s)", postRec.Code, postRec.Body.String())
	}

	// 2. Remote LAN device can still read display calendars without password
	getReq := httptest.NewRequest(http.MethodGet, "/api/calendars", nil)
	getReq.RemoteAddr = "192.168.1.100:54321"
	getRec := httptest.NewRecorder()

	srv.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for remote LAN read calendars, got %d", getRec.Code)
	}

	// 3. Attempting write to read-only calendar -> rejected with 400 Bad Request
	roPayload := map[string]interface{}{
		"calendar_id": "cal-readonly-holidays",
		"title":       "Write to read-only",
		"start_at":    time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"end_at":      time.Now().Add(25 * time.Hour).Format(time.RFC3339),
	}
	roBody, _ := json.Marshal(roPayload)

	roReq := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(roBody))
	roReq.RemoteAddr = "127.0.0.1:12345" // Localhost
	roReq.Header.Set("Content-Type", "application/json")
	roRec := httptest.NewRecorder()

	srv.ServeHTTP(roRec, roReq)
	if roRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request when creating event on read-only calendar, got %d (body: %s)", roRec.Code, roRec.Body.String())
	}
}

func TestKioskAuthorization_AdminPrivilegeAndLock(t *testing.T) {
	srv, _, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	// 1. Unauthenticated request to /admin -> redirected to /login
	adminReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	adminReq.RemoteAddr = "127.0.0.1:12345"
	adminRec := httptest.NewRecorder()

	srv.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect to /login for unauthenticated admin access, got %d", adminRec.Code)
	}

	// 2. Successful login establishes admin session
	loginForm := "username=admin&password=secretpass123"
	loginReq := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(loginForm))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.RemoteAddr = "127.0.0.1:12345"
	loginRec := httptest.NewRecorder()

	srv.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on successful login, got %d", loginRec.Code)
	}

	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "fb_session" && c.Value != "" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected session cookie fb_session to be set on successful login")
	}

	// 3. Requesting /admin with session cookie succeeds
	authedReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	authedReq.AddCookie(sessionCookie)
	authedRec := httptest.NewRecorder()

	srv.ServeHTTP(authedRec, authedReq)
	if authedRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for authenticated /admin, got %d", authedRec.Code)
	}

	// 4. Calling /admin/lock clears session and redirects back to /
	lockReq := httptest.NewRequest(http.MethodGet, "/admin/lock", nil)
	lockReq.AddCookie(sessionCookie)
	lockRec := httptest.NewRecorder()

	srv.ServeHTTP(lockRec, lockReq)
	if lockRec.Code != http.StatusSeeOther || lockRec.Header().Get("Location") != "/" {
		t.Fatalf("expected 303 redirect to / on /admin/lock, got %d (loc: %s)", lockRec.Code, lockRec.Header().Get("Location"))
	}
}
