package unit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	"github.com/Hardonian/FamilyBoard/internal/sync"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func setupTestServer(t *testing.T) (*web.Server, *db.Store, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "fb-web-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}

	database, err := db.Open(filepath.Join(tmpDir, "test.db"), 5000, 2, 1)
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
	tokenStorage, _ := oauth.NewTokenStorage(filepath.Join(tmpDir, "credentials"))
	stateManager := oauth.NewStateManager("test-secret-key-1234567890123456")

	cfg := config.DefaultConfig()
	cfg.Auth.AdminPasswordHash, _ = auth.HashPassword("correct-horse-battery")

	queueEngine := queue.NewEngine(store, nil, 3, 10*time.Second)
	syncEngine := sync.NewEngine(store, nil, 10*time.Minute)

	srv, err := web.NewServer(cfg, store, tokenStorage, stateManager, nil, nil, syncEngine, queueEngine)
	if err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("new server: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return srv, store, cleanup
}

func TestWeb_HealthEndpoints(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Live probe
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /health/live, got %d", w.Code)
	}

	// Ready probe
	req = httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for /health/ready, got %d", w.Code)
	}

	var status web.ReadyStatus
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if status.Status != "ready" || status.Database != "healthy" {
		t.Errorf("unexpected ready status: %+v", status)
	}
}

func TestWeb_KioskRedirectAndEventCRUD(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Uninitialized appliance -> redirects to /setup
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/setup" {
		t.Errorf("expected redirect to /setup, got code %d location %s", w.Code, w.Header().Get("Location"))
	}

	// Initialize appliance
	ctx := context.Background()
	_ = store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Kitchen Appliance",
		Timezone:      "America/Toronto",
		InitializedAt: time.Now(),
		Version:       "v0.9.0",
	})

	// Setup mock calendar
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                "acc-test",
		PersonID:          "person-1",
		Provider:          "google",
		ProviderAccountID: "g-1",
		VerifiedEmail:     "alex@example.com",
		Scopes:            "cal",
		ExpiresAt:         time.Now().Add(time.Hour),
		Status:            "active",
	})
	calID := "cal-primary"
	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 calID,
		OAuthAccountID:     "acc-test",
		Provider:           "google",
		ProviderCalendarID: "alex@example.com",
		Name:               "Family Calendar",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
	})

	// 2. Initialized appliance -> 200 OK rendering Kiosk
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for kiosk, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Kitchen Appliance") {
		t.Errorf("expected device name in kiosk HTML")
	}

	// 3. API Create Event
	now := time.Now()
	createPayload := models.Event{
		CalendarID: calID,
		Title:      "Family Movie Night",
		StartAt:    now.Add(2 * time.Hour),
		EndAt:      now.Add(4 * time.Hour),
		Location:   "Living Room",
	}
	bodyBytes, _ := json.Marshal(createPayload)

	req = httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(bodyBytes))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for create event, got %d: %s", w.Code, w.Body.String())
	}

	var created models.Event
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Title != "Family Movie Night" {
		t.Errorf("expected title 'Family Movie Night', got %s", created.Title)
	}

	// Verify event was queued in mutation_queue
	pendingCount, _ := store.CountPendingMutations(ctx)
	if pendingCount != 1 {
		t.Errorf("expected 1 pending mutation, got %d", pendingCount)
	}

	// 4. API List Events
	req = httptest.NewRequest(http.MethodGet, "/api/events", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for list events, got %d", w.Code)
	}

	var list []models.Event
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Title != "Family Movie Night" {
		t.Errorf("unexpected event list: %+v", list)
	}

	// 5. API Delete Event
	req = httptest.NewRequest(http.MethodDelete, "/api/events/"+created.ID, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for delete event, got %d", w.Code)
	}

	// List should now return 0 events
	req = httptest.NewRequest(http.MethodGet, "/api/events", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var listAfterDelete []models.Event
	_ = json.Unmarshal(w.Body.Bytes(), &listAfterDelete)
	if len(listAfterDelete) != 0 {
		t.Errorf("expected 0 events after delete, got %d", len(listAfterDelete))
	}
}

func TestWeb_AdminLoginAndSessionAuth(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Appliance",
		Timezone:      "America/Toronto",
		InitializedAt: time.Now(),
		Version:       "v0.9.0",
	})

	// 1. Unauthenticated request to /admin -> redirects to /login
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "/login") {
		t.Errorf("expected redirect to /login, got code %d location %s", w.Code, w.Header().Get("Location"))
	}

	// 2. Failed login with wrong password
	form := url.Values{
		"username": {"admin"},
		"password": {"wrong-password"},
	}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "Invalid username or password") {
		t.Errorf("expected error message on failed login")
	}

	// 3. Successful login with correct password
	form = url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery"},
	}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin" {
		t.Errorf("expected redirect to /admin, got code %d location %s", w.Code, w.Header().Get("Location"))
	}

	// Capture session cookie
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "fb_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected fb_session cookie to be set")
	}

	// 4. Authenticated request to /admin using session cookie -> 200 OK
	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for authenticated /admin, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "FamilyBoard Admin") {
		t.Errorf("expected admin dashboard HTML")
	}
}

func TestWeb_AdminCalendarAndPersonActions(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Setup account and calendars
	_ = store.UpsertPerson(ctx, &models.Person{
		ID:          "person-1",
		DisplayName: "Parent 1",
		Email:       "parent1@familydomain.org",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-1",
		PersonID:            "person-1",
		Provider:            "google",
		ProviderAccountID:   "sub-1",
		VerifiedEmail:       "parent1@familydomain.org",
		Scopes:              "https://www.googleapis.com/auth/calendar",
		CredentialReference: "acc-1",
		ExpiresAt:           now.Add(time.Hour),
		Status:              "active",
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	cal1 := &models.Calendar{
		ID:                 "cal-1",
		OAuthAccountID:     "acc-1",
		Provider:           "google",
		ProviderCalendarID: "cal1@domain.com",
		Name:               "Work",
		DisplayColor:       "#3b82f6",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	cal2 := &models.Calendar{
		ID:                 "cal-2",
		OAuthAccountID:     "acc-1",
		Provider:           "google",
		ProviderCalendarID: "c_home@group.calendar.google.com",
		Name:               "HOME",
		DisplayColor:       "#10b981",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = store.UpsertCalendar(ctx, cal1)
	_ = store.UpsertCalendar(ctx, cal2)

	// Log in to get session cookie
	loginForm := url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery"},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	srv.ServeHTTP(loginRec, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "fb_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("session cookie not found")
	}

	// 1. POST /admin/calendars/cal-2/set-default
	req := httptest.NewRequest(http.MethodPost, "/admin/calendars/cal-2/set-default", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 SeeOther, got %d", w.Code)
	}

	defCal, _ := store.GetDefaultWriteCalendar(ctx)
	if defCal == nil || defCal.ID != "cal-2" {
		t.Errorf("expected cal-2 to be default write target, got %+v", defCal)
	}

	// 2. POST /admin/calendars/cal-2/preferences (update color to #059669)
	prefForm := url.Values{
		"display_color":       {"#059669"},
		"enabled_for_display": {"1"},
		"writable":            {"1"},
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/calendars/cal-2/preferences", strings.NewReader(prefForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 SeeOther, got %d", w.Code)
	}

	c2, _ := store.GetCalendar(ctx, "cal-2")
	if c2 == nil || c2.DisplayColor != "#059669" {
		t.Errorf("expected updated color #059669, got %+v", c2)
	}

	// 3. POST /admin/people/person-1/update (update name and Google Workspace email)
	personForm := url.Values{
		"display_name": {"Scott H."},
		"email":        {"scott@familydomain.org"},
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/people/person-1/update", strings.NewReader(personForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 SeeOther, got %d", w.Code)
	}

	p1, _ := store.GetPerson(ctx, "person-1")
	if p1 == nil || p1.DisplayName != "Scott H." || p1.Email != "scott@familydomain.org" {
		t.Errorf("expected updated person profile, got %+v", p1)
	}

	// 4. POST /admin/calendars/refresh
	req = httptest.NewRequest(http.MethodPost, "/admin/calendars/refresh", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 SeeOther, got %d", w.Code)
	}
}

func TestWeb_KioskGoogleCalendarPolishAndViews(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Kitchen Appliance",
		Timezone:      "America/Toronto",
		InitializedAt: time.Now(),
		Version:       "v0.9.0",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for kiosk, got %d", w.Code)
	}

	body := w.Body.String()
	expectedTokens := []string{
		"btn-view-schedule",
		"Schedule",
		"schedule-view-container",
		"btn-create",
		"btn-today",
		"stepper-time-container",
		"stepper-time-readout",
		"time-summary-banner",
		"week-allday-row",
		"btn-clear-title",
		"detail-accent-strip",
	}

	for _, token := range expectedTokens {
		if !strings.Contains(body, token) {
			t.Errorf("kiosk HTML missing expected Google Calendar polish token: %q", token)
		}
	}
}
