package integration_test

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

	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
)

type mockTokenProvider struct {
	token string
}

func (m *mockTokenProvider) GetValidAccessToken(ctx context.Context, personID, provider string) (string, error) {
	return m.token, nil
}

func setupTestDB(t *testing.T) (*db.Store, func()) {
	tmpDir, err := os.MkdirTemp("", "fb-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	store := db.NewStore(database)
	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}
	return store, cleanup
}

func TestGoogleWorkspace_ListCalendars_SharedHomeCalendar(t *testing.T) {
	// Mock Google Workspace Calendar API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/users/me/calendarList" {
			resp := map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":              "admin@familyworkspace.org",
						"summary":         "Scott (Work & Personal)",
						"timeZone":        "America/Toronto",
						"backgroundColor": "#3b82f6",
						"primary":         true,
						"accessRole":      "owner",
					},
					{
						"id":              "c_home_calendar_777@group.calendar.google.com",
						"summary":         "HOME",
						"timeZone":        "America/Toronto",
						"backgroundColor": "#10b981",
						"primary":         false,
						"accessRole":      "writer",
					},
					{
						"id":              "c_kids_school_888@group.calendar.google.com",
						"summary":         "School & Sports",
						"timeZone":        "America/Toronto",
						"backgroundColor": "#fbbf24",
						"primary":         false,
						"accessRole":      "reader",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := google.NewGoogleCalendarClient(server.URL, server.Client())
	ctx := context.Background()

	calendars, err := client.ListCalendars(ctx, "mock-gsuite-token")
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}

	if len(calendars) != 3 {
		t.Fatalf("expected 3 calendars, got %d", len(calendars))
	}

	// Verify primary calendar
	primary := calendars[0]
	if primary.Name != "Scott (Work & Personal)" || !primary.DefaultWriteTarget || !primary.Writable {
		t.Errorf("unexpected primary calendar: %+v", primary)
	}

	// Verify shared HOME calendar
	homeCal := calendars[1]
	if homeCal.Name != "HOME" || homeCal.ProviderCalendarID != "c_home_calendar_777@group.calendar.google.com" {
		t.Errorf("unexpected HOME calendar: %+v", homeCal)
	}
	if !homeCal.Writable {
		t.Errorf("expected HOME calendar to be writable for family members")
	}

	// Verify secondary read-only calendar
	schoolCal := calendars[2]
	if schoolCal.Writable {
		t.Errorf("expected read-only calendar to have writable=false")
	}
}

func TestGoogleWorkspace_AutoDiscovery_And_Sync(t *testing.T) {
	var capturedCalPaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/users/me/calendarList" {
			resp := map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"id":              "parent@familyworkspace.org",
						"summary":         "Parent Work Calendar",
						"timeZone":        "America/Toronto",
						"backgroundColor": "#3b82f6",
						"primary":         true,
						"accessRole":      "owner",
					},
					{
						"id":              "c_home_999@group.calendar.google.com",
						"summary":         "HOME",
						"timeZone":        "America/Toronto",
						"backgroundColor": "#10b981",
						"primary":         false,
						"accessRole":      "writer",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Handle events sync for any calendar
		if strings.Contains(r.URL.Path, "/events") {
			capturedCalPaths = append(capturedCalPaths, r.URL.Path)

			var items []map[string]interface{}
			if strings.Contains(r.URL.Path, "c_home_999") {
				// Phone added an event to the shared HOME calendar!
				items = append(items, map[string]interface{}{
					"id":          "phone-event-101",
					"iCalUID":     "phone-uid-101@google.com",
					"summary":     "Family Dinner with Grandparents",
					"description": "Added from iPhone Calendar",
					"location":    "123 Maple St",
					"status":      "confirmed",
					"start":       map[string]string{"dateTime": "2026-10-15T18:00:00Z"},
					"end":         map[string]string{"dateTime": "2026-10-15T20:00:00Z"},
					"updated":     "2026-10-09T14:00:00Z",
				})
			}

			resp := map[string]interface{}{
				"nextSyncToken": "sync-token-done",
				"items":         items,
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Setup Person and connected Google Workspace OAuth account
	now := time.Now()
	_ = store.UpsertPerson(ctx, &models.Person{
		ID:          "person-1",
		DisplayName: "Scott",
		Email:       "parent@familyworkspace.org",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-g-101",
		PersonID:            "person-1",
		Provider:            "google",
		ProviderAccountID:   "sub-g-101",
		VerifiedEmail:       "parent@familyworkspace.org",
		Scopes:              "https://www.googleapis.com/auth/calendar",
		CredentialReference: "acc-g-101",
		ExpiresAt:           now.Add(24 * time.Hour),
		Status:              "active",
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	googleClient := google.NewGoogleCalendarClient(server.URL, server.Client())
	tokenProvider := &mockTokenProvider{token: "valid-token"}

	syncEngine := calsync.NewEngine(store, tokenProvider, 1*time.Minute)
	syncEngine.RegisterProvider(googleClient)

	// 2. Run Auto-Discovery and Full Sync
	if err := syncEngine.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	// 3. Verify that HOME calendar was automatically registered and selected as default write target
	calendars, err := store.ListCalendars(ctx)
	if err != nil {
		t.Fatalf("ListCalendars from store failed: %v", err)
	}

	if len(calendars) != 2 {
		t.Fatalf("expected 2 discovered calendars in store, got %d", len(calendars))
	}

	var homeCalendar *models.Calendar
	for _, c := range calendars {
		if strings.EqualFold(c.Name, "HOME") {
			homeCalendar = &c
			break
		}
	}

	if homeCalendar == nil {
		t.Fatalf("HOME calendar was not discovered!")
	}

	if !homeCalendar.DefaultWriteTarget {
		t.Errorf("expected HOME calendar to be marked DefaultWriteTarget for family kiosk, got false")
	}
	if homeCalendar.DisplayColor != "#10b981" {
		t.Errorf("expected HOME calendar to receive emerald color, got %s", homeCalendar.DisplayColor)
	}

	// 4. Verify that the event added from the phone was synchronized into SQLite
	events, err := store.ListEventsRange(ctx, now.AddDate(0, -1, 0), now.AddDate(0, 1, 0))
	if err != nil {
		t.Fatalf("ListEventsRange failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 synced event from phone on HOME calendar, got %d", len(events))
	}

	ev := events[0]
	if ev.Title != "Family Dinner with Grandparents" {
		t.Errorf("unexpected event title: %s", ev.Title)
	}
	if ev.CalendarID != homeCalendar.ID {
		t.Errorf("expected event calendar ID %s, got %s", homeCalendar.ID, ev.CalendarID)
	}
}

func TestGoogleWorkspace_KioskToPhone_OutboundMutation(t *testing.T) {
	var capturedCreateBody map[string]interface{}
	var capturedCalID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/events") {
			capturedCalID = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&capturedCreateBody)

			resp := map[string]interface{}{
				"id":      "created-from-kiosk-999",
				"iCalUID": "kiosk-uid-999@google.com",
				"summary": capturedCreateBody["summary"],
				"status":  "confirmed",
				"etag":    "\"etag-kiosk-999\"",
				"start":   capturedCreateBody["start"],
				"end":     capturedCreateBody["end"],
				"updated": "2026-10-09T16:00:00Z",
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Insert person and OAuth account
	_ = store.UpsertPerson(ctx, &models.Person{
		ID:          "person-1",
		DisplayName: "Scott",
		Email:       "scott@familyworkspace.org",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-g-1",
		PersonID:            "person-1",
		Provider:            "google",
		ProviderAccountID:   "sub-1",
		VerifiedEmail:       "scott@familyworkspace.org",
		Scopes:              "https://www.googleapis.com/auth/calendar",
		CredentialReference: "acc-g-1",
		ExpiresAt:           now.Add(24 * time.Hour),
		Status:              "active",
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	// Create HOME calendar in SQLite (shared Google Workspace secondary calendar)
	homeCal := &models.Calendar{
		ID:                 "cal-home-1",
		OAuthAccountID:     "acc-g-1",
		Provider:           "google",
		ProviderCalendarID: "c_shared_home_456@group.calendar.google.com",
		Name:               "HOME",
		Timezone:           "America/Toronto",
		DisplayColor:       "#10b981",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = store.UpsertCalendar(ctx, homeCal)

	// Create event locally on kiosk display destined for HOME calendar
	localEvent := &models.Event{
		ID:             "evt-local-101",
		Provider:       "google",
		CalendarID:     homeCal.ID,
		Title:          "Kids Soccer Tournament",
		StartAt:        now.Add(24 * time.Hour),
		EndAt:          now.Add(26 * time.Hour),
		Timezone:       "America/Toronto",
		Status:         "confirmed",
		Source:         "kiosk_ui",
		LocalCreatedAt: now,
		LocalUpdatedAt: now,
	}
	_ = store.UpsertEvent(ctx, localEvent)

	// Enqueue mutation
	payload, _ := json.Marshal(localEvent)
	mutation := &models.MutationQueueItem{
		ID:             "mut-1",
		Operation:      "create",
		Provider:       "google",
		CalendarID:     homeCal.ID,
		LocalEventID:   localEvent.ID,
		Payload:        string(payload),
		IdempotencyKey: "idemp-soccer-1",
		State:          models.QueuePending,
		AttemptCount:   0,
		NextAttemptAt:  now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_ = store.EnqueueMutation(ctx, mutation)

	googleClient := google.NewGoogleCalendarClient(server.URL, server.Client())
	tokenProvider := &mockTokenProvider{token: "valid-token"}

	queueEngine := queue.NewEngine(store, tokenProvider, 5, 30*time.Second)
	queueEngine.RegisterProvider(googleClient)

	// Process queue mutation
	if err := queueEngine.ProcessBatch(ctx, 10); err != nil {
		t.Fatalf("ProcessBatch failed: %v", err)
	}

	// Verify Google API received event creation for the shared HOME calendar
	if !strings.Contains(capturedCalID, url.PathEscape(homeCal.ProviderCalendarID)) {
		t.Errorf("expected POST path to contain escaped HOME calendar ID %s, got %s", homeCal.ProviderCalendarID, capturedCalID)
	}
	if capturedCreateBody["summary"] != "Kids Soccer Tournament" {
		t.Errorf("unexpected event summary sent to Google: %v", capturedCreateBody["summary"])
	}

	// Verify local event was updated with remote Google event ID
	updatedLocal, err := store.GetEvent(ctx, localEvent.ID)
	if err != nil || updatedLocal == nil {
		t.Fatalf("failed to retrieve updated local event: %v", err)
	}
	if updatedLocal.ProviderEventID != "created-from-kiosk-999" {
		t.Errorf("expected provider event id 'created-from-kiosk-999', got '%s'", updatedLocal.ProviderEventID)
	}
}

func TestGoogleWorkspace_CalendarPreferences_And_DefaultTarget(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	_ = store.UpsertPerson(ctx, &models.Person{
		ID:          "person-1",
		DisplayName: "Parent",
		Email:       "parent@domain.com",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                  "acc-1",
		PersonID:            "person-1",
		Provider:            "google",
		ProviderAccountID:   "sub-1",
		VerifiedEmail:       "parent@domain.com",
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
		ProviderCalendarID: "user@domain.com",
		Name:               "Personal",
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

	// Set cal2 ("HOME") as default write calendar
	if err := store.SetDefaultWriteCalendar(ctx, "cal-2"); err != nil {
		t.Fatalf("SetDefaultWriteCalendar failed: %v", err)
	}

	defCal, err := store.GetDefaultWriteCalendar(ctx)
	if err != nil || defCal == nil {
		t.Fatalf("GetDefaultWriteCalendar failed: %v", err)
	}
	if defCal.ID != "cal-2" || defCal.Name != "HOME" {
		t.Errorf("expected HOME (cal-2) to be default write calendar, got: %+v", defCal)
	}

	// Verify cal1 is no longer default
	c1, _ := store.GetCalendar(ctx, "cal-1")
	if c1.DefaultWriteTarget {
		t.Errorf("cal-1 should no longer be default write target")
	}

	// Update preferences for cal2
	if err := store.UpdateCalendarPreferences(ctx, "cal-2", true, true, "#059669"); err != nil {
		t.Fatalf("UpdateCalendarPreferences failed: %v", err)
	}
	c2Updated, _ := store.GetCalendar(ctx, "cal-2")
	if c2Updated.DisplayColor != "#059669" {
		t.Errorf("expected updated color #059669, got %s", c2Updated.DisplayColor)
	}
}
