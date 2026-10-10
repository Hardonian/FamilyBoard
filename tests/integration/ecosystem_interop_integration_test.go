package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	appleCal "github.com/Hardonian/FamilyBoard/internal/calendar/apple"
	googleCal "github.com/Hardonian/FamilyBoard/internal/calendar/google"
	msCal "github.com/Hardonian/FamilyBoard/internal/calendar/microsoft"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
)

type multiAccountTokenProvider struct {
	mu     sync.Mutex
	tokens map[string]string // key: personID:provider
}

func newMultiAccountTokenProvider() *multiAccountTokenProvider {
	return &multiAccountTokenProvider{
		tokens: make(map[string]string),
	}
}

func (m *multiAccountTokenProvider) Set(personID, provider, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[personID+":"+provider] = token
}

func (m *multiAccountTokenProvider) GetValidAccessToken(ctx context.Context, personID, provider string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, ok := m.tokens[personID+":"+provider]
	if !ok {
		return "", fmt.Errorf("token not found for %s:%s", personID, provider)
	}
	return tok, nil
}

func TestEcosystemInterop_MultiProvider_DiscoveryAndSync(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Seed 3 household persons
	p1 := &models.Person{ID: "person-google", DisplayName: "Alex Google", Email: "alex@ourfamily.org", Enabled: true}
	p2 := &models.Person{ID: "person-ms", DisplayName: "Jordan Microsoft", Email: "jordan@office365.com", Enabled: true}
	p3 := &models.Person{ID: "person-apple", DisplayName: "Sam Apple", Email: "sam@icloud.com", Enabled: true}
	for _, p := range []*models.Person{p1, p2, p3} {
		if err := store.UpsertPerson(ctx, p); err != nil {
			t.Fatalf("upsert person: %v", err)
		}
	}

	// 2. Mock Google Server
	mockGoogle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/calendarList") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":              "alex-primary@ourfamily.org",
						"summary":         "Alex Schedule",
						"primary":         true,
						"accessRole":      "owner",
						"backgroundColor": "#3b82f6",
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/events") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"nextSyncToken": "g-cursor-1",
				"items": []map[string]any{
					{
						"id":          "evt-g-1",
						"summary":     "Google Meet Standup",
						"hangoutLink": "https://meet.google.com/abc-defg-hij",
						"start":       map[string]any{"dateTime": "2026-10-25T10:00:00Z"},
						"end":         map[string]any{"dateTime": "2026-10-25T10:30:00Z"},
						"status":      "confirmed",
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockGoogle.Close()

	// 3. Mock Microsoft Server
	mockMS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/calendars") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []map[string]any{
					{
						"id":                "ms-primary-cal-id",
						"name":              "Jordan Calendar",
						"hexColor":          "#0078d4",
						"isDefaultCalendar": true,
						"canEdit":           true,
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/events") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"@odata.deltaLink": "https://graph.microsoft.com/v1.0/me/delta-cursor-1",
				"value": []map[string]any{
					{
						"id":              "evt-ms-1",
						"iCalUId":         "ms-ical-1",
						"subject":         "Teams Project Review",
						"body":            map[string]any{"contentType": "text", "content": "Quarterly goals"},
						"start":           map[string]any{"dateTime": "2026-10-25T13:00:00", "timeZone": "UTC"},
						"end":             map[string]any{"dateTime": "2026-10-25T14:00:00", "timeZone": "UTC"},
						"isOnlineMeeting": true,
						"onlineMeeting":   map[string]any{"joinUrl": "https://teams.microsoft.com/l/meetup-join/456"},
						"changeKey":       "etag-ms-100",
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockMS.Close()

	// 4. Mock Apple Webcal Server
	appleICS := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Apple Inc.//macOS 15.0//EN
X-WR-CALNAME:Sam iCloud Shared
BEGIN:VEVENT
UID:evt-apple-1
DTSTART:20261025T180000Z
DTEND:20261025T190000Z
SUMMARY:FaceTime Family Call
URL:https://facetime.apple.com/join#v=1&p=789
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	mockApple := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		_, _ = w.Write([]byte(appleICS))
	}))
	defer mockApple.Close()

	// 5. Seed OAuth Accounts
	appleWebcalURL := "webcal://" + strings.TrimPrefix(mockApple.URL, "http://")
	acc1 := &models.OAuthAccount{ID: "acc-google", PersonID: "person-google", Provider: "google", Status: "active"}
	acc2 := &models.OAuthAccount{ID: "acc-ms", PersonID: "person-ms", Provider: "microsoft", Status: "active"}
	acc3 := &models.OAuthAccount{ID: "acc-apple", PersonID: "person-apple", Provider: "apple", Status: "active"}
	for _, acc := range []*models.OAuthAccount{acc1, acc2, acc3} {
		if err := store.UpsertOAuthAccount(ctx, acc); err != nil {
			t.Fatalf("upsert oauth account: %v", err)
		}
	}

	// Token Provider
	tp := newMultiAccountTokenProvider()
	tp.Set("person-google", "google", "tok-google")
	tp.Set("person-ms", "microsoft", "tok-ms")
	tp.Set("person-apple", "apple", appleWebcalURL)

	// Clients & Engine
	googleClient := googleCal.NewGoogleCalendarClient(mockGoogle.URL, mockGoogle.Client())
	microsoftClient := msCal.NewCalendarClient(mockMS.URL, mockMS.Client())
	appleClient := appleCal.NewAppleCalendarClient("", mockApple.Client())

	syncEng := calsync.NewEngine(store, tp, time.Minute)
	syncEng.RegisterProvider(googleClient)
	syncEng.RegisterProvider(microsoftClient)
	syncEng.RegisterProvider(appleClient)

	// 6. Run DiscoverCalendars
	if err := syncEng.DiscoverCalendars(ctx); err != nil {
		t.Fatalf("DiscoverCalendars failed: %v", err)
	}

	cals, err := store.ListDisplayCalendars(ctx)
	if err != nil {
		t.Fatalf("ListDisplayCalendars: %v", err)
	}
	if len(cals) != 3 {
		t.Fatalf("expected 3 discovered calendars across ecosystems, got %d", len(cals))
	}

	foundProviders := make(map[string]bool)
	for _, c := range cals {
		foundProviders[c.Provider] = true
	}
	if !foundProviders["google"] || !foundProviders["microsoft"] || !foundProviders["apple"] {
		t.Errorf("expected all 3 providers discovered, got map: %+v", foundProviders)
	}

	// 7. Run SyncAll
	if err := syncEng.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	// 8. Verify events in SQLite
	startRange := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	endRange := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	events, err := store.ListEventsRange(ctx, startRange, endRange)
	if err != nil {
		t.Fatalf("ListEventsRange: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 synchronized events, got %d", len(events))
	}

	meetTypes := make(map[string]bool)
	for _, ev := range events {
		if strings.Contains(ev.Description, "meet.google.com") {
			meetTypes["google_meet"] = true
		}
		if strings.Contains(ev.Description, "teams.microsoft.com") {
			meetTypes["ms_teams"] = true
		}
		if strings.Contains(ev.Description, "facetime.apple.com") {
			meetTypes["apple_facetime"] = true
		}
	}

	if !meetTypes["google_meet"] || !meetTypes["ms_teams"] || !meetTypes["apple_facetime"] {
		t.Errorf("expected all three video meeting types parsed correctly, got: %+v", meetTypes)
	}
}

func TestEcosystemInterop_OutboundMutationQueueReplay(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Seed Person & Accounts
	_ = store.UpsertPerson(ctx, &models.Person{ID: "p-user-1", DisplayName: "User 1", Email: "u1@family.org", Enabled: true})
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{ID: "acc-ms-1", PersonID: "p-user-1", Provider: "microsoft", Status: "active"})
	calMS := &models.Calendar{
		ID:                 "cal-ms-outbound",
		OAuthAccountID:     "acc-ms-1",
		Provider:           "microsoft",
		ProviderCalendarID: "primary",
		Name:               "MS Calendar",
		Writable:           true,
	}
	_ = store.UpsertCalendar(ctx, calMS)

	var receivedMethod string
	var receivedBody string

	mockMS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        "remote-ms-id-777",
			"changeKey": "etag-new-777",
		})
	}))
	defer mockMS.Close()

	tp := newMultiAccountTokenProvider()
	tp.Set("p-user-1", "microsoft", "token-ms-write")

	msClient := msCal.NewCalendarClient(mockMS.URL, mockMS.Client())
	queueEng := queue.NewEngine(store, tp, 3, time.Second)
	queueEng.RegisterProvider(msClient)

	// Create local event and enqueue mutation atomically
	now := time.Now()
	ev := &models.Event{
		ID:             "evt-local-kiosk-1",
		Provider:       "microsoft",
		CalendarID:     calMS.ID,
		Title:          "Soccer Practice on Radxa Dragon",
		StartAt:        now.Add(24 * time.Hour),
		EndAt:          now.Add(25 * time.Hour),
		Source:         "kiosk",
		LocalCreatedAt: now,
		LocalUpdatedAt: now,
	}
	payload, _ := json.Marshal(ev)
	mut := &models.MutationQueueItem{
		ID:             "mut-ms-1",
		Operation:      "create",
		Provider:       "microsoft",
		CalendarID:     calMS.ID,
		LocalEventID:   ev.ID,
		Payload:        string(payload),
		IdempotencyKey: "idemp-test-1",
		State:          models.QueuePending,
	}

	if err := store.UpsertEventAndEnqueueMutation(ctx, ev, mut); err != nil {
		t.Fatalf("UpsertEventAndEnqueueMutation failed: %v", err)
	}

	// Process queue
	if err := queueEng.ProcessBatch(ctx, 10); err != nil {
		t.Fatalf("ProcessBatch failed: %v", err)
	}

	if receivedMethod != "POST" {
		t.Errorf("expected HTTP POST on remote provider, got %q", receivedMethod)
	}
	if !strings.Contains(receivedBody, "Soccer Practice on Radxa Dragon") {
		t.Errorf("expected payload in body, got: %s", receivedBody)
	}

	// Verify local event has received remote provider ID and sync source
	updatedLocal, err := store.GetEvent(ctx, ev.ID)
	if err != nil || updatedLocal == nil {
		t.Fatalf("GetEvent failed: %v", err)
	}
	if updatedLocal.ProviderEventID != "remote-ms-id-777" {
		t.Errorf("expected remote ID remote-ms-id-777, got %q", updatedLocal.ProviderEventID)
	}
	if updatedLocal.Source != "remote_sync" {
		t.Errorf("expected source remote_sync after successful replay, got %q", updatedLocal.Source)
	}
}
