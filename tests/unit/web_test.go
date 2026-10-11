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
	cfg.Database.Path = filepath.Join(tmpDir, "test.db")
	cfg.Auth.AdminPasswordHash, _ = auth.HashPassword("correct-horse-battery")
	if err := cfg.Save(filepath.Join(tmpDir, "config.toml")); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("save test config: %v", err)
	}

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

func csrfForTest(t *testing.T, srv *web.Server, path string) (*http.Cookie, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected %s to render for CSRF setup, got %d", path, w.Code)
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "fb_csrf" && cookie.Value != "" {
			return cookie, cookie.Value
		}
	}
	t.Fatalf("expected CSRF cookie from %s", path)
	return nil, ""
}

func TestWeb_CSRFRedirectAndSecurityHeaders(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	missingTokenForm := url.Values{"username": {"admin"}, "password": {"correct-horse-battery"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(missingTokenForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected login without CSRF token to be rejected, got %d", w.Code)
	}

	csrfCookie, csrfToken := csrfForTest(t, srv, "/login?next=//evil.example/steal")
	loginForm := url.Values{
		"username":   {"admin"},
		"password":   {"correct-horse-battery"},
		"csrf_token": {csrfToken},
		"next":       {"//evil.example/steal"},
	}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin" {
		t.Fatalf("expected unsafe redirect to fall back to /admin, got %d %q", w.Code, w.Header().Get("Location"))
	}

	crossSiteReq := httptest.NewRequest(http.MethodPost, "/api/kiosk/parse-natural", strings.NewReader(`{"input":"Dinner tomorrow at 6"}`))
	crossSiteReq.RemoteAddr = "127.0.0.1:1234"
	crossSiteReq.Header.Set("Content-Type", "application/json")
	crossSiteReq.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSiteW := httptest.NewRecorder()
	srv.ServeHTTP(crossSiteW, crossSiteReq)
	if crossSiteW.Code != http.StatusForbidden {
		t.Fatalf("expected cross-site mutation to be rejected, got %d", crossSiteW.Code)
	}

	headerReq := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	headerW := httptest.NewRecorder()
	srv.ServeHTTP(headerW, headerReq)
	for name, want := range map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Cross-Origin-Opener-Policy": "same-origin",
	} {
		if got := headerW.Header().Get(name); got != want {
			t.Errorf("expected %s=%q, got %q", name, want, got)
		}
	}
}

func TestWeb_HealthEndpoints(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
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

	// A database behind the binary's embedded migrations must not be ready.
	if _, err := store.DB().Exec("DELETE FROM schema_migrations WHERE version = (SELECT MAX(version) FROM schema_migrations)"); err != nil {
		t.Fatalf("simulate stale schema: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected stale schema readiness to return 503, got %d", w.Code)
	}
}

func TestWeb_SetupCannotResetInitializedAppliance(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	initializedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	if err := store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Kitchen Board",
		Timezone:      "America/Toronto",
		InitializedAt: initializedAt,
		Version:       "v1.0.0",
		KioskSettings: "{}",
	}); err != nil {
		t.Fatalf("initialize appliance: %v", err)
	}

	csrfCookie, csrfToken := csrfForTest(t, srv, "/login")
	setupForm := url.Values{
		"device_name":            {"Hijacked Board"},
		"timezone":               {"UTC"},
		"admin_password":         {"replacement-password"},
		"admin_password_confirm": {"replacement-password"},
		"csrf_token":             {csrfToken},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(setupForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected initialized setup POST to return 409, got %d", w.Code)
	}

	meta, err := store.GetDeviceMetadata(ctx)
	if err != nil {
		t.Fatalf("read appliance metadata: %v", err)
	}
	if meta == nil || meta.DeviceName != "Kitchen Board" || meta.Timezone != "America/Toronto" {
		t.Fatalf("initialized metadata changed after rejected setup: %+v", meta)
	}
}

func TestWeb_FirstRunSetupAppliesGoogleCredentialsImmediately(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	csrfCookie, csrfToken := csrfForTest(t, srv, "/setup")
	setupForm := url.Values{
		"device_name":            {"Kitchen Board"},
		"timezone":               {"America/Toronto"},
		"admin_password":         {"new-secure-password"},
		"admin_password_confirm": {"new-secure-password"},
		"google_client_id":       {"portable-client-id"},
		"google_client_secret":   {"portable-client-secret"},
		"member1_name":           {"Alex"},
		"member1_email":          {"alex@example.com"},
		"csrf_token":             {csrfToken},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(setupForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin?tab=accounts" {
		t.Fatalf("setup failed: status=%d location=%q body=%q", w.Code, w.Header().Get("Location"), w.Body.String())
	}

	meta, err := store.GetDeviceMetadata(context.Background())
	if err != nil || meta == nil || meta.DeviceName != "Kitchen Board" {
		t.Fatalf("setup metadata was not persisted: meta=%+v err=%v", meta, err)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "fb_session" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("setup did not create an admin session")
	}

	oauthReq := httptest.NewRequest(http.MethodGet, "/admin/oauth/start?person_id=person-1&provider=google&flow=calendar", nil)
	oauthReq.AddCookie(sessionCookie)
	oauthW := httptest.NewRecorder()
	srv.ServeHTTP(oauthW, oauthReq)
	if oauthW.Code != http.StatusSeeOther {
		t.Fatalf("OAuth start failed after setup: %d %q", oauthW.Code, oauthW.Body.String())
	}
	location, err := url.Parse(oauthW.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse OAuth redirect: %v", err)
	}
	if got := location.Query().Get("client_id"); got != "portable-client-id" {
		t.Fatalf("OAuth client was not refreshed after setup: client_id=%q", got)
	}
}

func TestWeb_StaticAssetsUseContentFingerprinting(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	pageReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	page := httptest.NewRecorder()
	srv.ServeHTTP(page, pageReq)
	body := page.Body.String()
	marker := `/static/css/app.css?v=`
	start := strings.Index(body, marker)
	if start == -1 {
		t.Fatalf("expected fingerprinted stylesheet URL in rendered page")
	}
	end := strings.Index(body[start:], `"`)
	if end == -1 {
		t.Fatalf("expected closing quote after fingerprinted stylesheet URL")
	}
	assetPath := body[start : start+end]

	assetReq := httptest.NewRequest(http.MethodGet, assetPath, nil)
	asset := httptest.NewRecorder()
	srv.ServeHTTP(asset, assetReq)
	if asset.Code != http.StatusOK {
		t.Fatalf("expected asset response 200, got %d", asset.Code)
	}
	if got := asset.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected fingerprinted asset to be immutable, got %q", got)
	}
	etag := asset.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected fingerprinted asset ETag")
	}

	revalidateReq := httptest.NewRequest(http.MethodGet, assetPath, nil)
	revalidateReq.Header.Set("If-None-Match", etag)
	revalidateReq.Header.Set("Accept-Encoding", "gzip")
	revalidate := httptest.NewRecorder()
	srv.ServeHTTP(revalidate, revalidateReq)
	if revalidate.Code != http.StatusNotModified {
		t.Fatalf("expected matching ETag to return 304, got %d", revalidate.Code)
	}
	if encoding := revalidate.Header().Get("Content-Encoding"); encoding != "" {
		t.Fatalf("expected 304 response without content encoding, got %q", encoding)
	}

	unversionedReq := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
	unversioned := httptest.NewRecorder()
	srv.ServeHTTP(unversioned, unversionedReq)
	if got := unversioned.Header().Get("Cache-Control"); strings.Contains(got, "immutable") {
		t.Fatalf("expected unversioned asset to require revalidation, got %q", got)
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

	// Verify event was durably queued. The background worker may already have
	// advanced it to retry/dead-letter when this test has no provider client.
	queued, _ := store.ListAllMutations(ctx, 10, 0)
	if len(queued) != 1 {
		t.Errorf("expected 1 durable mutation, got %d", len(queued))
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
	csrfCookie, csrfToken := csrfForTest(t, srv, "/login")
	form := url.Values{
		"username":   {"admin"},
		"password":   {"wrong-password"},
		"csrf_token": {csrfToken},
	}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "Invalid username or password") {
		t.Errorf("expected error message on failed login")
	}

	// 3. Successful login with correct password
	form = url.Values{
		"username":   {"admin"},
		"password":   {"correct-horse-battery"},
		"csrf_token": {csrfToken},
	}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
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
	csrfCookie, csrfToken := csrfForTest(t, srv, "/login")
	loginForm := url.Values{
		"username":   {"admin"},
		"password":   {"correct-horse-battery"},
		"csrf_token": {csrfToken},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.AddCookie(csrfCookie)
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
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfToken)
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
		"csrf_token":          {csrfToken},
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/calendars/cal-2/preferences", strings.NewReader(prefForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
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
		"csrf_token":   {csrfToken},
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/people/person-1/update", strings.NewReader(personForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
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
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfToken)
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

func TestWeb_ManifestAndServiceWorker(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Verify Web App Manifest
	reqManifest := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	wManifest := httptest.NewRecorder()
	srv.ServeHTTP(wManifest, reqManifest)
	if wManifest.Code != http.StatusOK {
		t.Fatalf("expected 200 for manifest.webmanifest, got %d", wManifest.Code)
	}
	if ct := wManifest.Header().Get("Content-Type"); !strings.Contains(ct, "application/manifest+json") {
		t.Errorf("expected application/manifest+json, got %q", ct)
	}
	var manifestData map[string]interface{}
	if err := json.Unmarshal(wManifest.Body.Bytes(), &manifestData); err != nil {
		t.Fatalf("failed to parse manifest JSON: %v", err)
	}
	if manifestData["name"] != "FamilyBoard" || manifestData["display"] != "standalone" {
		t.Errorf("unexpected manifest content: %+v", manifestData)
	}

	// 2. Verify Service Worker
	reqSW := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	wSW := httptest.NewRecorder()
	srv.ServeHTTP(wSW, reqSW)
	if wSW.Code != http.StatusOK {
		t.Fatalf("expected 200 for sw.js, got %d", wSW.Code)
	}
	if swAllowed := wSW.Header().Get("Service-Worker-Allowed"); swAllowed != "/" {
		t.Errorf("expected Service-Worker-Allowed: /, got %q", swAllowed)
	}
	if !strings.Contains(wSW.Body.String(), "familyboard-shell") {
		t.Errorf("sw.js content missing cache shell identifier")
	}
}

func TestWeb_MobilePairingAndCapabilityIssuance(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Kitchen Appliance",
		Timezone:      "America/Toronto",
		InitializedAt: time.Now(),
		Version:       "v1.2.0",
	})
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
	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 "cal-primary",
		OAuthAccountID:     "acc-test",
		Provider:           "google",
		ProviderCalendarID: "alex@example.com",
		Name:               "Family Calendar",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
	})

	// 1. Physical kiosk requests pairing payload (localhost)
	reqPair := httptest.NewRequest(http.MethodGet, "/api/system/mobile-pair", nil)
	reqPair.RemoteAddr = "127.0.0.1:8080"
	reqPair.Host = "attacker.example"
	reqPair.Header.Set("X-Forwarded-Proto", "https")
	wPair := httptest.NewRecorder()
	srv.ServeHTTP(wPair, reqPair)
	if wPair.Code != http.StatusOK {
		t.Fatalf("expected 200 for mobile-pair endpoint, got %d", wPair.Code)
	}

	var pairData map[string]interface{}
	if err := json.Unmarshal(wPair.Body.Bytes(), &pairData); err != nil {
		t.Fatalf("failed to decode pairing data: %v", err)
	}
	pairURL, ok := pairData["pair_url"].(string)
	token, tokOk := pairData["token"].(string)
	if !ok || pairURL == "" || !tokOk || token == "" {
		t.Fatalf("expected valid pair_url and token, got %+v", pairData)
	}
	if strings.Contains(pairURL, "attacker.example") {
		t.Fatalf("pairing URL trusted an unverified Host header: %s", pairURL)
	}

	// 2. Verify QR Code generation
	reqQR := httptest.NewRequest(http.MethodGet, "/api/system/mobile-pair/qrcode", nil)
	reqQR.RemoteAddr = "127.0.0.1:8080"
	wQR := httptest.NewRecorder()
	srv.ServeHTTP(wQR, reqQR)
	if wQR.Code != http.StatusOK {
		t.Fatalf("expected 200 for mobile-pair/qrcode endpoint, got %d", wQR.Code)
	}
	if ct := wQR.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected image/png for QR code, got %q", ct)
	}

	// 3. Remote smartphone (iPhone / Poco F7) scans QR code and hits /?pair=<token>
	reqPhone := httptest.NewRequest(http.MethodGet, "/?pair="+token, nil)
	reqPhone.RemoteAddr = "192.168.1.150:54321" // Remote LAN IP
	wPhone := httptest.NewRecorder()
	srv.ServeHTTP(wPhone, reqPhone)

	if wPhone.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect after valid pairing token, got %d", wPhone.Code)
	}

	var kioskCookie *http.Cookie
	for _, c := range wPhone.Result().Cookies() {
		if c.Name == "fb_kiosk_cap" {
			kioskCookie = c
			break
		}
	}
	if kioskCookie == nil || kioskCookie.Value == "" {
		t.Fatalf("expected fb_kiosk_cap cookie to be issued to paired phone")
	}

	// Pairing codes are single-use and cannot be replayed.
	replayReq := httptest.NewRequest(http.MethodGet, "/?pair="+token, nil)
	replayReq.RemoteAddr = "192.168.1.151:54321"
	replayRec := httptest.NewRecorder()
	srv.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusSeeOther || !strings.Contains(replayRec.Header().Get("Location"), "pairing=invalid") {
		t.Fatalf("expected replayed pairing code to be rejected, got %d %q", replayRec.Code, replayRec.Header().Get("Location"))
	}

	// 4. Remote smartphone now uses the issued cookie to perform a write mutation (create event)
	eventPayload := map[string]interface{}{
		"calendar_id": "cal-primary",
		"title":       "Soccer Practice from Mobile Phone",
		"start_at":    time.Now().Add(2 * time.Hour).Format(time.RFC3339),
		"end_at":      time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		"all_day":     false,
	}
	pBytes, _ := json.Marshal(eventPayload)
	reqEvent := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(pBytes))
	reqEvent.RemoteAddr = "192.168.1.150:54321" // Remote LAN client
	reqEvent.Header.Set("Content-Type", "application/json")
	reqEvent.AddCookie(kioskCookie)

	wEvent := httptest.NewRecorder()
	srv.ServeHTTP(wEvent, reqEvent)
	if wEvent.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created from paired mobile phone, got %d (body: %s)", wEvent.Code, wEvent.Body.String())
	}

	devices, err := store.ListPairedDevices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("expected one paired device, got %d err=%v", len(devices), err)
	}
	if err := store.RevokePairedDevice(ctx, devices[0].ID); err != nil {
		t.Fatalf("revoke paired device: %v", err)
	}
	revokedReq := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(pBytes))
	revokedReq.RemoteAddr = "192.168.1.150:54321"
	revokedReq.Header.Set("Content-Type", "application/json")
	revokedReq.AddCookie(kioskCookie)
	revokedRec := httptest.NewRecorder()
	srv.ServeHTTP(revokedRec, revokedReq)
	if revokedRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked device to lose access, got %d", revokedRec.Code)
	}
}

func TestWeb_MobilePWAAndLayoutElements(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Kitchen Appliance",
		Timezone:      "America/Toronto",
		InitializedAt: time.Now(),
		Version:       "v1.2.0",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for kiosk, got %d", w.Code)
	}

	body := w.Body.String()
	expectedMobileTokens := []string{
		"apple-mobile-web-app-capable",
		"apple-mobile-web-app-status-bar-style",
		"viewport-fit=cover",
		"/manifest.webmanifest",
		"/static/icons/apple-touch-icon.png",
		"mobile-bottom-nav",
		"btn-mobile-fab-add",
		"modal-mobile-pair",
		"mobile-pwa-banner",
		"btn-pair-phone",
	}

	for _, token := range expectedMobileTokens {
		if !strings.Contains(body, token) {
			t.Errorf("kiosk HTML missing mobile token: %q", token)
		}
	}
}
