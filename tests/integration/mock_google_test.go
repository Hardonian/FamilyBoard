package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestGoogleCalendarClient_SyncAndPagination(t *testing.T) {
	// Mock Google Calendar Server with 2 pages of events and a syncToken
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		pageToken := r.URL.Query().Get("pageToken")
		syncToken := r.URL.Query().Get("syncToken")

		if syncToken == "invalid-cursor-123" {
			w.WriteHeader(http.StatusGone) // 410 Gone
			_, _ = w.Write([]byte(`{"error": {"code": 410, "message": "Sync token is no longer valid"}}`))
			return
		}

		if pageToken == "page-2" {
			// Page 2 response
			resp := map[string]interface{}{
				"nextSyncToken": "valid-sync-token-999",
				"items": []map[string]interface{}{
					{
						"id":      "event-page-2",
						"iCalUID": "uid-page-2@google.com",
						"summary": "Guitar Lesson",
						"status":  "confirmed",
						"start":   map[string]string{"dateTime": "2026-09-20T17:00:00Z"},
						"end":     map[string]string{"dateTime": "2026-09-20T18:00:00Z"},
						"created": "2026-09-01T10:00:00Z",
						"updated": "2026-09-01T10:00:00Z",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Page 1 response
		resp := map[string]interface{}{
			"nextPageToken": "page-2",
			"items": []map[string]interface{}{
				{
					"id":      "event-page-1",
					"iCalUID": "uid-page-1@google.com",
					"summary": "Soccer Practice",
					"status":  "confirmed",
					"start":   map[string]string{"dateTime": "2026-09-18T18:00:00Z"},
					"end":     map[string]string{"dateTime": "2026-09-18T19:00:00Z"},
					"created": "2026-09-01T10:00:00Z",
					"updated": "2026-09-01T10:00:00Z",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := google.NewGoogleCalendarClient(server.URL, server.Client())
	ctx := context.Background()

	cal := models.Calendar{
		ID:                 "cal-1",
		ProviderCalendarID: "primary",
	}

	// 1. Initial Sync (fetches page 1 and page 2, returns nextSyncToken)
	result, err := client.SyncEvents(ctx, "mock-token", cal, "")
	if err != nil {
		t.Fatalf("sync events failed: %v", err)
	}

	if len(result.Events) != 2 {
		t.Fatalf("expected 2 paginated events, got %d", len(result.Events))
	}
	if result.SyncCursor != "valid-sync-token-999" {
		t.Errorf("expected sync cursor 'valid-sync-token-999', got '%s'", result.SyncCursor)
	}

	// 2. Test 410 Gone Recovery
	_, err = client.SyncEvents(ctx, "mock-token", cal, "invalid-cursor-123")
	if err != google.ErrCursorInvalid {
		t.Errorf("expected ErrCursorInvalid (410), got %v", err)
	}
}

func TestGoogleCalendarClient_RetryOn429And500(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")

		if att == 1 {
			// First attempt returns 429 Too Many Requests
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limit exceeded"}`))
			return
		}
		if att == 2 {
			// Second attempt returns 503 Service Unavailable
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": "backend temporarily unavailable"}`))
			return
		}

		// Third attempt succeeds
		resp := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":      "event-retry-success",
					"summary": "Piano Recital",
					"status":  "confirmed",
					"start":   map[string]string{"dateTime": "2026-09-25T14:00:00Z"},
					"end":     map[string]string{"dateTime": "2026-09-25T15:00:00Z"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := google.NewGoogleCalendarClient(server.URL, server.Client())
	ctx := context.Background()

	cal := models.Calendar{ID: "cal-1", ProviderCalendarID: "primary"}
	result, err := client.SyncEvents(ctx, "mock-token", cal, "")
	if err != nil {
		t.Fatalf("expected retry to succeed on 3rd attempt, got: %v", err)
	}

	if len(result.Events) != 1 || result.Events[0].Title != "Piano Recital" {
		t.Errorf("unexpected event result: %+v", result.Events)
	}

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected exactly 3 attempts, got %d", attempts)
	}
}

func TestGoogleCalendarClient_CRUD(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost {
			// Create event
			resp := map[string]interface{}{
				"id":      "created-123",
				"iCalUID": "uid-created-123@google.com",
				"summary": "Dental Checkup",
				"status":  "confirmed",
				"etag":    "\"etag-123\"",
				"start":   map[string]string{"dateTime": "2026-09-22T15:30:00Z"},
				"end":     map[string]string{"dateTime": "2026-09-22T16:30:00Z"},
				"updated": "2026-09-05T12:00:00Z",
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodPut {
			// Update event
			resp := map[string]interface{}{
				"id":      "created-123",
				"iCalUID": "uid-created-123@google.com",
				"summary": "Dental Checkup (Updated)",
				"status":  "confirmed",
				"etag":    "\"etag-updated\"",
				"start":   map[string]string{"dateTime": "2026-09-22T15:30:00Z"},
				"end":     map[string]string{"dateTime": "2026-09-22T16:30:00Z"},
				"updated": "2026-09-05T12:05:00Z",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := google.NewGoogleCalendarClient(server.URL, server.Client())
	ctx := context.Background()

	ev := &models.Event{
		ID:       "local-1",
		Title:    "Dental Checkup",
		StartAt:  time.Now(),
		EndAt:    time.Now().Add(time.Hour),
		Timezone: "America/Toronto",
	}

	// Create
	created, err := client.CreateEvent(ctx, "mock-token", "primary", ev, "idemp-key-1")
	if err != nil {
		t.Fatalf("create event failed: %v", err)
	}
	if created.ProviderEventID != "created-123" {
		t.Errorf("expected provider event id 'created-123', got '%s'", created.ProviderEventID)
	}

	// Update
	created.Title = "Dental Checkup (Updated)"
	updated, err := client.UpdateEvent(ctx, "mock-token", "primary", created)
	if err != nil {
		t.Fatalf("update event failed: %v", err)
	}
	if updated.ETag != "\"etag-updated\"" {
		t.Errorf("expected updated etag, got %s", updated.ETag)
	}

	// Delete
	err = client.DeleteEvent(ctx, "mock-token", "primary", "created-123")
	if err != nil {
		t.Fatalf("delete event failed: %v", err)
	}
	_ = fmt.Sprint()
}
