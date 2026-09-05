package unit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestKiosk_QuickTemplatesAPI(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/kiosk/templates", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for templates, got %d", w.Code)
	}

	var templates []*models.QuickTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &templates); err != nil {
		t.Fatalf("failed to decode templates: %v", err)
	}

	if len(templates) < 5 {
		t.Errorf("expected at least 5 seeded templates, got %d", len(templates))
	}

	foundSoccer := false
	for _, tmpl := range templates {
		if tmpl.Title == "Soccer Practice" && tmpl.Icon == "⚽" {
			foundSoccer = true
			break
		}
	}
	if !foundSoccer {
		t.Errorf("expected Soccer Practice template in templates list")
	}
}

func TestKiosk_NaturalLanguageParseAPI(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	payload := map[string]string{
		"text": "Swimming Wednesday 5pm at Community Centre",
	}
	bodyBytes, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/kiosk/parse-natural", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for parse-natural, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode parse-natural response: %v", err)
	}

	titleStr, _ := resp["title"].(string)
	if !strings.Contains(titleStr, "Swimming") {
		t.Errorf("expected title to contain 'Swimming', got '%v'", resp["title"])
	}

	if resp["location"] != "Community Centre" {
		t.Errorf("expected location 'Community Centre', got '%v'", resp["location"])
	}
}

func TestKiosk_EventRestoreAPI(t *testing.T) {
	srv, store, cleanup := setupTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Seed OAuth account and calendar
	now := time.Now()
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                "acc-restore-test",
		PersonID:          "person-1",
		Provider:          "google",
		ProviderAccountID: "g-restore-test",
		VerifiedEmail:     "alex@example.com",
		Scopes:            "cal",
		ExpiresAt:         now.Add(time.Hour),
		Status:            "active",
	})
	calID := "cal-restore-target"
	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 calID,
		OAuthAccountID:     "acc-restore-test",
		Provider:           "google",
		ProviderCalendarID: "alex@example.com",
		Name:               "Family Calendar",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
	})

	// Create event
	createPayload := models.Event{
		CalendarID: calID,
		Title:      "Piano Lesson",
		StartAt:    now.Add(2 * time.Hour),
		EndAt:      now.Add(3 * time.Hour),
	}
	bodyBytes, _ := json.Marshal(createPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(bodyBytes))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create event: %d %s", w.Code, w.Body.String())
	}

	var created models.Event
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// Delete event
	delReq := httptest.NewRequest(http.MethodDelete, "/api/events/"+created.ID, nil)
	delReq.RemoteAddr = "127.0.0.1:1234"
	delW := httptest.NewRecorder()
	srv.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("delete event failed: %d", delW.Code)
	}

	// Verify soft-deleted
	ev, _ := store.GetEvent(ctx, created.ID)
	if ev.Status != "cancelled" || ev.DeletedAt == nil {
		t.Fatalf("expected soft-deleted event, got status=%s deletedAt=%v", ev.Status, ev.DeletedAt)
	}

	// Restore event
	restoreReq := httptest.NewRequest(http.MethodPost, "/api/events/"+created.ID+"/restore", nil)
	restoreReq.RemoteAddr = "127.0.0.1:1234"
	restoreW := httptest.NewRecorder()
	srv.ServeHTTP(restoreW, restoreReq)

	if restoreW.Code != http.StatusOK {
		t.Fatalf("restore event failed: %d %s", restoreW.Code, restoreW.Body.String())
	}

	restored, _ := store.GetEvent(ctx, created.ID)
	if restored.Status != "confirmed" || restored.DeletedAt != nil {
		t.Errorf("expected restored event, got status=%s deletedAt=%v", restored.Status, restored.DeletedAt)
	}
}
