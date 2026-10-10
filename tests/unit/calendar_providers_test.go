package unit_test

import (
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

	appleCal "github.com/Hardonian/FamilyBoard/internal/calendar/apple"
	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	msCal "github.com/Hardonian/FamilyBoard/internal/calendar/microsoft"
	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	"github.com/Hardonian/FamilyBoard/internal/sync"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func TestDetectEmailProvider_AllEcosystems(t *testing.T) {
	cases := []struct {
		email    string
		expected string
	}{
		{"alex@icloud.com", "apple"},
		{"jordan@me.com", "apple"},
		{"steve@mac.com", "apple"},
		{"alex@outlook.com", "microsoft"},
		{"work@office365.com", "microsoft"},
		{"corp@onmicrosoft.com", "microsoft"},
		{"employee@exchange.company.com", "microsoft"},
		{"personal@gmail.com", "google"},
		{"family@ourcustomdomain.org", "google"},
	}

	for _, tc := range cases {
		got := web.DetectEmailProvider(tc.email)
		if got != tc.expected {
			t.Errorf("DetectEmailProvider(%q) = %q, expected %q", tc.email, got, tc.expected)
		}
	}
}

func TestGoogleWorkspace_PaginationAndHostedDomain(t *testing.T) {
	// 1. Test Hosted Domain (hd) & prompt in AuthURL
	gc := oauth.NewGoogleClient("test-client", "test-secret", "http://localhost/callback")
	authURL := gc.AuthURL("test-state", "calendar", "alex@ourfamily.org")
	parsedURL, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse google auth url: %v", err)
	}
	q := parsedURL.Query()
	if q.Get("hd") != "ourfamily.org" {
		t.Errorf("expected hd=ourfamily.org for custom domain, got %q", q.Get("hd"))
	}
	if !strings.Contains(q.Get("prompt"), "select_account") {
		t.Errorf("expected prompt to contain select_account for multi-tenant Google Workspace, got %q", q.Get("prompt"))
	}

	// 2. Test Multi-page Calendar Discovery (Google Workspace secondary calendars)
	page := 1
	mockGoogleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			page = 2
			_ = json.NewEncoder(w).Encode(map[string]any{
				"nextPageToken": "token-page-2",
				"items": []map[string]any{
					{
						"id":              "primary@ourfamily.org",
						"summary":         "Alex (Primary)",
						"timeZone":        "America/Toronto",
						"backgroundColor": "#3b82f6",
						"primary":         true,
						"accessRole":      "owner",
					},
				},
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":              "home@group.calendar.google.com",
						"summary":         "HOME Household Calendar",
						"timeZone":        "America/Toronto",
						"backgroundColor": "#10b981",
						"primary":         false,
						"accessRole":      "writer",
					},
				},
			})
		}
	}))
	defer mockGoogleServer.Close()

	client := google.NewGoogleCalendarClient(mockGoogleServer.URL, mockGoogleServer.Client())
	cals, err := client.ListCalendars(context.Background(), "mock-token")
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}

	if len(cals) != 2 {
		t.Fatalf("expected 2 calendars across pages, got %d", len(cals))
	}
	if cals[0].Name != "Alex (Primary)" || cals[1].Name != "HOME Household Calendar" {
		t.Errorf("unexpected calendar names: %+v", cals)
	}
}

func TestMicrosoftExchange_Provider(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/calendars"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []map[string]any{
					{
						"id":                "AAMkAG-primary",
						"name":              "Exchange Calendar",
						"hexColor":          "#0078d4",
						"isDefaultCalendar": true,
						"canEdit":           true,
					},
				},
			})
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/events"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []map[string]any{
					{
						"id":          "evt-ms-1",
						"iCalUId":     "040000-uid-1",
						"subject":     "Dentist Appointment",
						"body":        map[string]any{"contentType": "text", "content": "Routine checkup"},
						"location":    map[string]any{"displayName": "Dental Clinic"},
						"start":       map[string]any{"dateTime": "2026-10-15T14:00:00", "timeZone": "UTC"},
						"end":         map[string]any{"dateTime": "2026-10-15T15:00:00", "timeZone": "UTC"},
						"isAllDay":    false,
						"isCancelled": false,
						"changeKey":   "etag-ms-1",
					},
				},
			})
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/events"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "evt-ms-new",
				"iCalUId":   "040000-uid-new",
				"changeKey": "etag-new",
			})
		case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/events/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "evt-ms-new",
				"changeKey": "etag-updated",
			})
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/events/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	client := msCal.NewCalendarClient(mockServer.URL, mockServer.Client())
	ctx := context.Background()

	// 1. List Calendars
	cals, err := client.ListCalendars(ctx, "mock-token")
	if err != nil {
		t.Fatalf("ListCalendars: %v", err)
	}
	if len(cals) != 1 || cals[0].Name != "Exchange Calendar" {
		t.Fatalf("unexpected calendars: %+v", cals)
	}

	// 2. Sync Events
	res, err := client.SyncEvents(ctx, "mock-token", cals[0], "")
	if err != nil {
		t.Fatalf("SyncEvents: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].Title != "Dentist Appointment" {
		t.Fatalf("unexpected events: %+v", res.Events)
	}

	// 3. Create Event
	newEvent := &models.Event{
		Title:       "Soccer Practice",
		Description: "Bring water",
		Location:    "City Park",
		StartAt:     time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC),
		EndAt:       time.Date(2026, 10, 16, 19, 0, 0, 0, time.UTC),
	}
	created, err := client.CreateEvent(ctx, "mock-token", cals[0].ProviderCalendarID, newEvent, "idemp-1")
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if created.ProviderEventID != "evt-ms-new" {
		t.Fatalf("expected new event id, got %q", created.ProviderEventID)
	}

	// 4. Update Event
	created.Title = "Updated Soccer Practice"
	updated, err := client.UpdateEvent(ctx, "mock-token", cals[0].ProviderCalendarID, created)
	if err != nil {
		t.Fatalf("UpdateEvent: %v", err)
	}
	if updated.ETag != "etag-updated" {
		t.Fatalf("expected updated changeKey, got %q", updated.ETag)
	}

	// 5. Delete Event
	if err := client.DeleteEvent(ctx, "mock-token", cals[0].ProviderCalendarID, "evt-ms-new"); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
}

func TestAppleCalendar_WebcalSubscription(t *testing.T) {
	icsContent := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Apple Inc.//macOS 15.0//EN
X-WR-CALNAME:Apple Household Shared
BEGIN:VEVENT
UID:apple-evt-12345
DTSTART:20261018T180000Z
DTEND:20261018T190000Z
SUMMARY:Family Sunday Dinner
DESCRIPTION:At grandma's house
LOCATION:123 Maple Street
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		_, _ = w.Write([]byte(icsContent))
	}))
	defer mockServer.Close()

	client := appleCal.NewAppleCalendarClient("", mockServer.Client())
	ctx := context.Background()

	// 1. List Calendars using Webcal URL
	webcalURL := "webcal://" + strings.TrimPrefix(mockServer.URL, "http://")
	cals, err := client.ListCalendars(ctx, webcalURL)
	if err != nil {
		t.Fatalf("ListCalendars: %v", err)
	}
	if len(cals) != 1 || cals[0].Name != "Apple Household Shared" {
		t.Fatalf("unexpected Apple calendar: %+v", cals)
	}

	// 2. Sync Events
	res, err := client.SyncEvents(ctx, webcalURL, cals[0], "")
	if err != nil {
		t.Fatalf("SyncEvents: %v", err)
	}
	if len(res.Events) != 1 || res.Events[0].Title != "Family Sunday Dinner" {
		t.Fatalf("unexpected Apple events: %+v", res.Events)
	}
	if res.Events[0].Provider != "apple" {
		t.Errorf("expected provider apple, got %s", res.Events[0].Provider)
	}
}

func TestAdmin_AppleConnectEndpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fb-apple-test-*")
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

	person := &models.Person{
		ID:          "person-apple-1",
		DisplayName: "Alex Apple",
		Email:       "alex@icloud.com",
		Enabled:     true,
	}
	if err := store.UpsertPerson(ctx, person); err != nil {
		t.Fatalf("upsert person: %v", err)
	}

	tokenStorage, err := oauth.NewTokenStorage(filepath.Join(tmpDir, "credentials"))
	if err != nil {
		t.Fatalf("token storage: %v", err)
	}
	stateManager := oauth.NewStateManager("test-secret-123")

	cfg := config.DefaultConfig()
	cfg.Database.Path = database.Path()
	cfg.Auth.AdminPasswordHash, _ = auth.HashPassword("correct-horse-battery")
	queueEngine := queue.NewEngine(store, nil, 3, time.Second)
	syncEngine := sync.NewEngine(store, nil, time.Minute)

	server, err := web.NewServer(
		cfg,
		store,
		tokenStorage,
		stateManager,
		nil,
		nil,
		syncEngine,
		queueEngine,
	)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	// Login to get valid authenticated session
	csrfCookie, csrfToken := csrfForTest(t, server, "/login")
	loginForm := url.Values{
		"username":   {"admin"},
		"password":   {"correct-horse-battery"},
		"csrf_token": {csrfToken},
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.AddCookie(csrfCookie)
	loginRec := httptest.NewRecorder()
	server.ServeHTTP(loginRec, loginReq)

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

	// Connect Apple iCloud Shared Calendar via POST /admin/apple/connect
	formData := url.Values{
		"person_id":  {"person-apple-1"},
		"auth_type":  {"webcal"},
		"webcal_url": {"https://p123-caldav.icloud.com/published/2/testfeed"},
		"csrf_token": {csrfToken},
	}
	reqWithAuth := httptest.NewRequest("POST", "/admin/apple/connect", strings.NewReader(formData.Encode()))
	reqWithAuth.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqWithAuth.AddCookie(sessionCookie)
	reqWithAuth.AddCookie(csrfCookie)

	recAuth := httptest.NewRecorder()
	server.ServeHTTP(recAuth, reqWithAuth)

	if recAuth.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect on apple connect, got %d: %s", recAuth.Code, recAuth.Body.String())
	}

	// Verify account in store
	account, err := store.GetOAuthAccount(ctx, "person-apple-1", "apple")
	if err != nil || account == nil {
		t.Fatalf("expected apple account in store, got err: %v, account: %+v", err, account)
	}
	if account.Provider != "apple" {
		t.Errorf("expected provider apple, got %s", account.Provider)
	}

	// Verify token load via server.GetValidAccessToken
	tok, err := server.GetValidAccessToken(ctx, "person-apple-1", "apple")
	if err != nil {
		t.Fatalf("GetValidAccessToken: %v", err)
	}
	if tok != "https://p123-caldav.icloud.com/published/2/testfeed" {
		t.Errorf("unexpected token: %s", tok)
	}
}

func TestMicrosoft_TeamsMeetingExtractionAndCustomTenant(t *testing.T) {
	// 1. Verify custom tenant ID routing
	customTenantClient := oauth.NewMicrosoftClient("test-client", "test-secret", "http://localhost/callback", "contoso-tenant-id")
	authURL := customTenantClient.AuthURL("test-state")
	if !strings.Contains(authURL, "login.microsoftonline.com/contoso-tenant-id/oauth2/v2.0/authorize") {
		t.Errorf("expected custom tenant auth url, got %s", authURL)
	}

	// 2. Verify Teams meeting link extraction in SyncEvents
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{
				{
					"id":              "evt-teams-1",
					"subject":         "Parent-Teacher Conference",
					"body":            map[string]any{"contentType": "text", "content": "Review Q1 progress"},
					"start":           map[string]any{"dateTime": "2026-10-20T15:00:00", "timeZone": "UTC"},
					"end":             map[string]any{"dateTime": "2026-10-20T15:30:00", "timeZone": "UTC"},
					"isOnlineMeeting": true,
					"onlineMeeting":   map[string]any{"joinUrl": "https://teams.microsoft.com/l/meetup-join/19%3ameeting_123"},
					"changeKey":       "etag-teams-1",
				},
			},
		})
	}))
	defer mockServer.Close()

	client := msCal.NewCalendarClient(mockServer.URL, mockServer.Client())
	res, err := client.SyncEvents(context.Background(), "mock-token", models.Calendar{ID: "cal-ms-1", ProviderCalendarID: "primary"}, "")
	if err != nil {
		t.Fatalf("SyncEvents: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	ev := res.Events[0]
	if !strings.Contains(ev.Description, "https://teams.microsoft.com/l/meetup-join/19%3ameeting_123") {
		t.Errorf("expected teams link in description, got %q", ev.Description)
	}
	if ev.Location != "Microsoft Teams Meeting" {
		t.Errorf("expected Location 'Microsoft Teams Meeting', got %q", ev.Location)
	}
}

func TestGoogleWorkspace_GoogleMeetExtraction(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":          "g-meet-evt-1",
					"summary":     "Band Rehearsal Planning",
					"description": "Discuss sheet music",
					"start":       map[string]any{"dateTime": "2026-10-21T18:00:00Z"},
					"end":         map[string]any{"dateTime": "2026-10-21T19:00:00Z"},
					"hangoutLink": "https://meet.google.com/xyz-uvwx-rst",
					"status":      "confirmed",
					"etag":        "etag-g-1",
				},
			},
		})
	}))
	defer mockServer.Close()

	client := google.NewGoogleCalendarClient(mockServer.URL, mockServer.Client())
	res, err := client.SyncEvents(context.Background(), "mock-token", models.Calendar{ID: "cal-g-1", ProviderCalendarID: "primary"}, "")
	if err != nil {
		t.Fatalf("SyncEvents: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	ev := res.Events[0]
	if !strings.Contains(ev.Description, "https://meet.google.com/xyz-uvwx-rst") {
		t.Errorf("expected Google Meet link in description, got %q", ev.Description)
	}
	if ev.Location != "Google Meet" {
		t.Errorf("expected Location 'Google Meet', got %q", ev.Location)
	}
}

func TestAppleCalendar_EventURLExtraction(t *testing.T) {
	icsContent := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Apple Inc.//macOS 15.0//EN
BEGIN:VEVENT
UID:apple-facetime-1
DTSTART:20261022T200000Z
DTEND:20261022T203000Z
SUMMARY:Grandparents FaceTime Call
DESCRIPTION:Catch up with grandma
URL:https://facetime.apple.com/join#v=1&p=987654321
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		_, _ = w.Write([]byte(icsContent))
	}))
	defer mockServer.Close()

	client := appleCal.NewAppleCalendarClient("", mockServer.Client())
	res, err := client.SyncEvents(context.Background(), mockServer.URL, models.Calendar{ID: "cal-apple-1", ProviderCalendarID: mockServer.URL}, "")
	if err != nil {
		t.Fatalf("SyncEvents: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	ev := res.Events[0]
	if !strings.Contains(ev.Description, "https://facetime.apple.com/join#v=1&p=987654321") {
		t.Errorf("expected FaceTime link in description, got %q", ev.Description)
	}
	if ev.Location != "Online Meeting" {
		t.Errorf("expected Location 'Online Meeting', got %q", ev.Location)
	}
}

