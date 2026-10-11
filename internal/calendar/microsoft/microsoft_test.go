package microsoft

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestCalendarClient_Name(t *testing.T) {
	client := NewCalendarClient("", nil)
	if client.Name() != "microsoft" {
		t.Errorf("expected client name 'microsoft', got %q", client.Name())
	}
}

func TestCalendarClient_ListCalendars_Pagination(t *testing.T) {
	var reqCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if reqCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"@odata.nextLink": server.URL + "/calendars?page=2",
				"value": []map[string]any{
					{
						"id":                "cal-ms-primary",
						"name":              "Primary Calendar",
						"hexColor":          "#0078d4",
						"isDefaultCalendar": true,
						"canEdit":           true,
					},
				},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{
				{
					"id":                "cal-ms-shared-home",
					"name":              "HOME Family Calendar",
					"hexColor":          "#10b981",
					"isDefaultCalendar": false,
					"canEdit":           true,
				},
			},
		})
	}))
	defer server.Close()

	client := NewCalendarClient(server.URL, server.Client())
	cals, err := client.ListCalendars(context.Background(), "test-access-token")
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}

	if len(cals) != 2 {
		t.Fatalf("expected 2 calendars across pages, got %d", len(cals))
	}
	if cals[0].Name != "Primary Calendar" || !cals[0].DefaultWriteTarget {
		t.Errorf("unexpected primary calendar: %+v", cals[0])
	}
	if cals[1].Name != "HOME Family Calendar" || !cals[1].Writable {
		t.Errorf("unexpected home calendar: %+v", cals[1])
	}
}

func TestCalendarClient_SyncEvents_DeltaLinkAndMeetings(t *testing.T) {
	deltaURL := "https://graph.microsoft.com/v1.0/me/calendars/cal-1/events/delta?$deltatoken=token-xyz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"@odata.deltaLink": deltaURL,
			"value": []map[string]any{
				{
					"id":              "evt-ms-1",
					"iCalUId":         "040000-uid-1",
					"subject":         "Parent Teacher Conference",
					"body":            map[string]any{"contentType": "text", "content": "Review report card"},
					"start":           map[string]any{"dateTime": "2026-10-20T14:00:00", "timeZone": "UTC"},
					"end":             map[string]any{"dateTime": "2026-10-20T14:30:00", "timeZone": "UTC"},
					"isOnlineMeeting": true,
					"onlineMeeting":   map[string]any{"joinUrl": "https://teams.microsoft.com/l/meetup-join/123"},
					"changeKey":       "ck-100",
					"isCancelled":     false,
				},
				{
					"id":          "evt-ms-2",
					"iCalUId":     "040000-uid-2",
					"subject":     "Cancelled Practice",
					"start":       map[string]any{"dateTime": "2026-10-21T18:00:00", "timeZone": "UTC"},
					"end":         map[string]any{"dateTime": "2026-10-21T19:00:00", "timeZone": "UTC"},
					"changeKey":   "ck-101",
					"isCancelled": true,
				},
			},
		})
	}))
	defer server.Close()

	client := NewCalendarClient(server.URL, server.Client())
	cal := models.Calendar{ID: "cal-local-1", ProviderCalendarID: "cal-ms-remote-1"}

	res, err := client.SyncEvents(context.Background(), "test-token", cal, "")
	if err != nil {
		t.Fatalf("SyncEvents failed: %v", err)
	}

	if len(res.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(res.Events))
	}
	if res.SyncCursor != deltaURL {
		t.Errorf("expected deltaToken %q, got %q", deltaURL, res.SyncCursor)
	}

	ev1 := res.Events[0]
	if ev1.Status != "confirmed" {
		t.Errorf("expected status confirmed, got %q", ev1.Status)
	}
	if ev1.Location != "Microsoft Teams Meeting" {
		t.Errorf("expected Location 'Microsoft Teams Meeting', got %q", ev1.Location)
	}
	if !strings.Contains(ev1.Description, "https://teams.microsoft.com/l/meetup-join/123") {
		t.Errorf("expected Teams link in description, got %q", ev1.Description)
	}

	ev2 := res.Events[1]
	if ev2.Status != "cancelled" {
		t.Errorf("expected cancelled event status, got %q", ev2.Status)
	}
}

func TestCalendarClient_CRUD(t *testing.T) {
	var createPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			if r.Header.Get("Client-Request-Id") != "idemp-crud-1" {
				t.Errorf("missing or incorrect Client-Request-Id header")
			}
			if err := json.NewDecoder(r.Body).Decode(&createPayload); err != nil {
				t.Errorf("decode create payload: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "evt-created-1",
				"iCalUId":   "ical-created-1",
				"changeKey": "ck-initial",
			})
		case "PATCH":
			if r.Header.Get("If-Match") != "ck-initial" {
				t.Errorf("expected If-Match header 'ck-initial', got %q", r.Header.Get("If-Match"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "evt-created-1",
				"changeKey": "ck-updated",
			})
		case "DELETE":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewCalendarClient(server.URL, server.Client())
	ctx := context.Background()

	// 1. CreateEvent
	newEvent := &models.Event{
		Title:       "Dentist Appointment",
		Description: "Checkup and cleaning",
		Location:    "Dental Suite",
		StartAt:     time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC),
		EndAt:       time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC),
	}
	created, err := client.CreateEvent(ctx, "test-token", "primary", newEvent, "idemp-crud-1")
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}
	if created.ProviderEventID != "evt-created-1" {
		t.Errorf("expected ProviderEventID 'evt-created-1', got %q", created.ProviderEventID)
	}
	if created.ETag != "ck-initial" {
		t.Errorf("expected ETag 'ck-initial', got %q", created.ETag)
	}
	if createPayload["transactionId"] != "idemp-crud-1" {
		t.Fatalf("create payload transactionId = %v, want idemp-crud-1", createPayload["transactionId"])
	}

	// 2. UpdateEvent
	created.Title = "Rescheduled Dentist Appointment"
	updated, err := client.UpdateEvent(ctx, "test-token", "primary", created)
	if err != nil {
		t.Fatalf("UpdateEvent failed: %v", err)
	}
	if updated.ETag != "ck-updated" {
		t.Errorf("expected ETag 'ck-updated', got %q", updated.ETag)
	}

	// 3. DeleteEvent
	if err := client.DeleteEvent(ctx, "test-token", "primary", "evt-created-1"); err != nil {
		t.Fatalf("DeleteEvent failed: %v", err)
	}
}

func TestCalendarClient_ErrorsAndRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{},
		})
	}))
	defer server.Close()

	client := NewCalendarClient(server.URL, server.Client())
	ctx := context.Background()

	// Should retry on 429 and succeed
	cals, err := client.ListCalendars(ctx, "test-token")
	if err != nil {
		t.Fatalf("expected success after retry on 429, got err: %v", err)
	}
	if len(cals) != 0 {
		t.Errorf("expected 0 calendars, got %d", len(cals))
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}

	// Test 404 on DeleteEvent is handled cleanly as no-op
	notFoundServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFoundServer.Close()

	notFoundClient := NewCalendarClient(notFoundServer.URL, notFoundServer.Client())
	err = notFoundClient.DeleteEvent(ctx, "test-token", "cal-1", "non-existent")
	if err != nil {
		t.Errorf("expected nil error on idempotent 404 delete, got %v", err)
	}
}
