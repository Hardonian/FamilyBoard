package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/queue"
)

type mockTokenProvider struct{}

func (m *mockTokenProvider) GetValidAccessToken(ctx context.Context, personID, provider string) (string, error) {
	return "mock-access-token", nil
}

func TestOffline_LifecycleAndReconciliationE2E(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fb-offline-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "familyboard.db")
	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := db.NewStore(database)

	// Setup account and calendar
	accID := "acc-member-1"
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                accID,
		PersonID:          "person-1",
		Provider:          "google",
		ProviderAccountID: "g-member-1",
		VerifiedEmail:     "alex@example.com",
		Scopes:            "cal",
		ExpiresAt:         time.Now().Add(time.Hour),
		Status:            "active",
	})
	calID := "cal-primary"
	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 calID,
		OAuthAccountID:     accID,
		Provider:           "google",
		ProviderCalendarID: "alex@example.com",
		Name:               "Primary Calendar",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
	})

	// Setup mock remote Google Calendar server with an online/offline toggle switch
	var isOnline int32 = 1
	var remoteCreates int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&isOnline) == 0 {
			// Simulate network drop / timeout / 503
			http.Error(w, "Network unreachable (Offline)", http.StatusServiceUnavailable)
			return
		}

		if r.Method == http.MethodPost {
			atomic.AddInt32(&remoteCreates, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			resp := map[string]interface{}{
				"id":      "remote-evt-999",
				"iCalUID": "uid-999@google.com",
				"summary": "Soccer Game",
				"etag":    "\"etag-remote-1\"",
				"start":   map[string]string{"dateTime": "2026-09-18T18:00:00Z"},
				"end":     map[string]string{"dateTime": "2026-09-18T19:00:00Z"},
				"created": "2026-09-05T12:00:00Z",
				"updated": "2026-09-05T12:00:00Z",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	googleClient := google.NewGoogleCalendarClient(server.URL, server.Client())
	queueEngine := queue.NewEngine(store, &mockTokenProvider{}, 5, 100*time.Millisecond)
	queueEngine.RegisterProvider(googleClient)

	// Step 1: Simulate Internet Disconnection
	atomic.StoreInt32(&isOnline, 0)

	// Step 2: Create event offline
	offlineStart := time.Date(2026, 9, 18, 18, 0, 0, 0, time.UTC)
	offlineEvent := &models.Event{
		ID:             "evt-local-100",
		Provider:       "google",
		CalendarID:     calID,
		Title:          "Soccer Game",
		StartAt:        offlineStart,
		EndAt:          offlineStart.Add(time.Hour),
		Timezone:       "America/Toronto",
		Status:         "confirmed",
		Source:         "local_ui",
		LocalCreatedAt: time.Now(),
		LocalUpdatedAt: time.Now(),
	}

	if err := store.UpsertEvent(ctx, offlineEvent); err != nil {
		t.Fatalf("failed to create offline event: %v", err)
	}

	payload, _ := json.Marshal(offlineEvent)
	mutation := &models.MutationQueueItem{
		ID:             "mut-100",
		Operation:      "create",
		Provider:       "google",
		CalendarID:     calID,
		LocalEventID:   offlineEvent.ID,
		Payload:        string(payload),
		IdempotencyKey: "idemp-soccer-100",
		AttemptCount:   0,
		NextAttemptAt:  time.Now(),
		State:          models.QueuePending,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := store.EnqueueMutation(ctx, mutation); err != nil {
		t.Fatalf("failed to enqueue mutation: %v", err)
	}

	// Attempt processing while offline -> must fail safely and transition to retry
	_ = queueEngine.ProcessBatch(ctx, 10)

	// Step 3: Simulate Device Reboot while Offline
	database.Close()
	databaseRebooted, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		t.Fatalf("failed to open database after reboot: %v", err)
	}
	defer databaseRebooted.Close()

	storeAfterReboot := db.NewStore(databaseRebooted)
	queueAfterReboot := queue.NewEngine(storeAfterReboot, &mockTokenProvider{}, 5, 100*time.Millisecond)
	queueAfterReboot.RegisterProvider(googleClient)

	// Verify local event persists and is immediately queryable after reboot
	events, err := storeAfterReboot.ListEventsRange(ctx, offlineStart.Add(-time.Hour), offlineStart.Add(2*time.Hour))
	if err != nil || len(events) != 1 || events[0].Title != "Soccer Game" {
		t.Fatalf("expected offline event to persist after reboot: %+v, err=%v", events, err)
	}

	// Step 4: Restore Connectivity
	atomic.StoreInt32(&isOnline, 1)

	// Step 5: Queue engine drains batch
	// Force next attempt timestamp to now so it's picked up immediately
	_ = storeAfterReboot.UpdateMutationState(ctx, mutation.ID, models.QueuePending, 0, time.Now(), "")
	err = queueAfterReboot.ProcessBatch(ctx, 10)
	if err != nil {
		t.Fatalf("queue process batch failed: %v", err)
	}

	// Step 6: Verify remote received exactly 1 create call
	if atomic.LoadInt32(&remoteCreates) != 1 {
		t.Errorf("expected exactly 1 remote create call, got %d", remoteCreates)
	}

	// Step 7: Verify local event is reconciled with remote IDs
	reconciled, err := storeAfterReboot.GetEvent(ctx, offlineEvent.ID)
	if err != nil || reconciled == nil {
		t.Fatalf("failed to get reconciled event: %v", err)
	}
	if reconciled.ProviderEventID != "remote-evt-999" {
		t.Errorf("expected ProviderEventID 'remote-evt-999', got '%s'", reconciled.ProviderEventID)
	}
	if reconciled.ETag != "\"etag-remote-1\"" {
		t.Errorf("expected ETag '\"etag-remote-1\"', got '%s'", reconciled.ETag)
	}

	// Step 8: Verify mutation queue is marked completed
	pendingCount, _ := storeAfterReboot.CountPendingMutations(ctx)
	if pendingCount != 0 {
		t.Errorf("expected 0 pending mutations after sync drain, got %d", pendingCount)
	}
}
