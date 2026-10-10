package google

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestGoogleCalendarClient_Name(t *testing.T) {
	client := NewGoogleCalendarClient("", nil)
	if client.Name() != "google" {
		t.Errorf("expected client name 'google', got %q", client.Name())
	}
}

func TestGoogleCalendarClient_ListCalendars_Pagination(t *testing.T) {
	var reqCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-google-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if reqCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"nextPageToken": "page-2-token",
				"items": []map[string]any{
					{
						"id":              "primary-cal@gmail.com",
						"summary":         "Personal",
						"backgroundColor": "#3b82f6",
						"primary":         true,
						"accessRole":      "owner",
					},
				},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":              "shared-home@group.calendar.google.com",
					"summary":         "HOME Family Calendar",
					"backgroundColor": "#10b981",
					"primary":         false,
					"accessRole":      "writer",
				},
			},
		})
	}))
	defer server.Close()

	client := NewGoogleCalendarClient(server.URL, server.Client())
	cals, err := client.ListCalendars(context.Background(), "test-google-token")
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}

	if len(cals) != 2 {
		t.Fatalf("expected 2 calendars, got %d", len(cals))
	}
	if cals[0].Name != "Personal" || !cals[0].DefaultWriteTarget {
		t.Errorf("unexpected primary calendar: %+v", cals[0])
	}
	if cals[1].Name != "HOME Family Calendar" || !cals[1].Writable {
		t.Errorf("unexpected home calendar: %+v", cals[1])
	}
}

func TestGoogleCalendarClient_SyncEvents_CursorAndMeet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"nextSyncToken": "new-sync-token-999",
			"items": []map[string]any{
				{
					"id":          "g-event-1",
					"summary":     "School Play",
					"description": "Auditorium seats reserved",
					"start":       map[string]any{"dateTime": "2026-10-22T19:00:00Z"},
					"end":         map[string]any{"dateTime": "2026-10-22T21:00:00Z"},
					"hangoutLink": "https://meet.google.com/abc-defg-hij",
					"status":      "confirmed",
					"etag":        "etag-123",
				},
			},
		})
	}))
	defer server.Close()

	client := NewGoogleCalendarClient(server.URL, server.Client())
	cal := models.Calendar{ID: "cal-g-1", ProviderCalendarID: "primary"}

	res, err := client.SyncEvents(context.Background(), "test-token", cal, "old-token")
	if err != nil {
		t.Fatalf("SyncEvents failed: %v", err)
	}

	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	if res.SyncCursor != "new-sync-token-999" {
		t.Errorf("expected nextSyncToken 'new-sync-token-999', got %q", res.SyncCursor)
	}
	ev := res.Events[0]
	if ev.Location != "Google Meet" {
		t.Errorf("expected location 'Google Meet', got %q", ev.Location)
	}
	if !strings.Contains(ev.Description, "https://meet.google.com/abc-defg-hij") {
		t.Errorf("expected meet link in description, got %q", ev.Description)
	}
}

func TestGoogleCalendarClient_SyncEvents_410Gone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()

	client := NewGoogleCalendarClient(server.URL, server.Client())
	cal := models.Calendar{ID: "cal-g-1", ProviderCalendarID: "primary"}

	_, err := client.SyncEvents(context.Background(), "test-token", cal, "expired-sync-token")
	if !errors.Is(err, ErrCursorInvalid) {
		t.Errorf("expected ErrCursorInvalid on 410 Gone, got %v", err)
	}
}

func TestGoogleCalendarClient_CRUD(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "created-g-id",
				"etag":  "g-etag-new",
				"start": map[string]any{"dateTime": "2026-10-24T16:00:00Z"},
				"end":   map[string]any{"dateTime": "2026-10-24T17:00:00Z"},
			})
		case "PUT":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "created-g-id",
				"etag":  "g-etag-updated",
				"start": map[string]any{"dateTime": "2026-10-24T16:00:00Z"},
				"end":   map[string]any{"dateTime": "2026-10-24T17:00:00Z"},
			})
		case "DELETE":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGoogleCalendarClient(server.URL, server.Client())
	ctx := context.Background()

	// 1. CreateEvent
	newEvent := &models.Event{
		Title:       "Piano Lesson",
		Description: "Bring lesson book",
		StartAt:     time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC),
		EndAt:       time.Date(2026, 10, 24, 17, 0, 0, 0, time.UTC),
	}
	created, err := client.CreateEvent(ctx, "test-token", "primary", newEvent, "idemp-g-1")
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}
	if created.ProviderEventID != "created-g-id" {
		t.Errorf("expected ProviderEventID 'created-g-id', got %q", created.ProviderEventID)
	}

	// 2. UpdateEvent
	created.Title = "Updated Piano Lesson"
	updated, err := client.UpdateEvent(ctx, "test-token", "primary", created)
	if err != nil {
		t.Fatalf("UpdateEvent failed: %v", err)
	}
	if updated.ETag != "g-etag-updated" {
		t.Errorf("expected ETag 'g-etag-updated', got %q", updated.ETag)
	}

	// 3. DeleteEvent
	if err := client.DeleteEvent(ctx, "test-token", "primary", "created-g-id"); err != nil {
		t.Fatalf("DeleteEvent failed: %v", err)
	}
}
