package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar"
	"github.com/Hardonian/FamilyBoard/internal/clock"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/errdefs"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/queue"
)

type mockTokenProvider struct {
	token string
	err   error
}

func (m *mockTokenProvider) GetValidAccessToken(ctx context.Context, personID, provider string) (string, error) {
	return m.token, m.err
}

type mockCalendarProvider struct {
	createFunc func(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error)
	updateFunc func(ctx context.Context, token, calID string, ev *models.Event) (*models.Event, error)
	deleteFunc func(ctx context.Context, token, calID, eventID string) error
}

func (m *mockCalendarProvider) Name() string {
	return "google"
}

func (m *mockCalendarProvider) ListCalendars(ctx context.Context, token string) ([]models.Calendar, error) {
	return nil, nil
}

func (m *mockCalendarProvider) SyncEvents(ctx context.Context, token string, cal models.Calendar, syncCursor string) (*calendar.SyncResult, error) {
	return nil, nil
}

func (m *mockCalendarProvider) CreateEvent(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, token, calID, ev, idemKey)
	}
	ev.ProviderEventID = "remote-123"
	ev.ETag = "etag-1"
	return ev, nil
}

func (m *mockCalendarProvider) UpdateEvent(ctx context.Context, token, calID string, ev *models.Event) (*models.Event, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, token, calID, ev)
	}
	ev.ETag = "etag-2"
	return ev, nil
}

func (m *mockCalendarProvider) DeleteEvent(ctx context.Context, token, calID, eventID string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, token, calID, eventID)
	}
	return nil
}

func setupQueueTest(t *testing.T) (*db.DB, *db.Store, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "queue-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "queue.db")
	database, err := db.Open(dbPath, 5000, 2, 1)
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
	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}
	return database, store, cleanup
}

func TestQueue_LifecycleAndStatus(t *testing.T) {
	_, store, cleanup := setupQueueTest(t)
	defer cleanup()

	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC))
	engine := queue.NewEngine(store, nil, 3, 10*time.Second)
	engine.SetClock(mockClk)

	status := engine.Status()
	if status.Running || status.Health != "stopped" {
		t.Errorf("expected stopped initial status, got %+v", status)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine.Start(ctx)
	status = engine.Status()
	if !status.Running || status.Health != "healthy" {
		t.Errorf("expected healthy running status, got %+v", status)
	}

	if err := engine.StopWithTimeout(2 * time.Second); err != nil {
		t.Fatalf("stop with timeout failed: %v", err)
	}

	status = engine.Status()
	if status.Running || status.Health != "stopped" {
		t.Errorf("expected stopped status after shutdown, got %+v", status)
	}
}

func TestQueue_RecoversInterruptedProcessingWork(t *testing.T) {
	_, store, cleanup := setupQueueTest(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	item := &models.MutationQueueItem{
		ID: "mut-interrupted", Operation: "create", Provider: "local",
		CalendarID: "cal-local", LocalEventID: "ev-local", Payload: `{}`,
		IdempotencyKey: "idem-interrupted", State: models.QueuePending,
		NextAttemptAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.EnqueueMutation(ctx, item); err != nil {
		t.Fatalf("enqueue mutation: %v", err)
	}
	if err := store.UpdateMutationState(ctx, item.ID, models.QueueProcessing, 1, now, "connection dropped"); err != nil {
		t.Fatalf("mark processing: %v", err)
	}

	recovered, err := store.RecoverProcessingMutations(ctx)
	if err != nil {
		t.Fatalf("recover processing mutations: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered %d mutations, want 1", recovered)
	}
	got, err := store.GetMutation(ctx, item.ID)
	if err != nil {
		t.Fatalf("get recovered mutation: %v", err)
	}
	if got.State != models.QueueRetry || got.NextAttemptAt.After(time.Now()) {
		t.Fatalf("recovered mutation is not immediately retryable: %+v", got)
	}
	if !strings.Contains(got.LastError, "recovered after interrupted processing") {
		t.Fatalf("recovery reason missing from last_error: %q", got.LastError)
	}
}

func TestQueue_PermanentError_ImmediateDeadLetter(t *testing.T) {
	_, store, cleanup := setupQueueTest(t)
	defer cleanup()

	ctx := context.Background()
	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC))
	engine := queue.NewEngine(store, &mockTokenProvider{token: "valid-tok"}, 3, 5*time.Second)
	engine.SetClock(mockClk)

	prov := &mockCalendarProvider{
		createFunc: func(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error) {
			return nil, errdefs.Permanent("calendar deleted or unparseable event")
		},
	}
	engine.RegisterProvider(prov)

	p := &models.Person{ID: "p-1", DisplayName: "Alex", Email: "alex@example.com"}
	_ = store.UpsertPerson(ctx, p)
	oa := &models.OAuthAccount{ID: "oa-1", PersonID: "p-1", Provider: "google", Status: "active"}
	_ = store.UpsertOAuthAccount(ctx, oa)
	cal := &models.Calendar{ID: "cal-1", OAuthAccountID: "oa-1", Provider: "google", ProviderCalendarID: "primary", Name: "Google Cal"}
	_ = store.UpsertCalendar(ctx, cal)

	ev := &models.Event{ID: "ev-1", CalendarID: "cal-1", Title: "Soccer"}
	_ = store.UpsertEvent(ctx, ev)

	payloadBytes, _ := json.Marshal(ev)
	mut := &models.MutationQueueItem{
		ID:             "mut-1",
		CalendarID:     "cal-1",
		Provider:       "google",
		LocalEventID:   "ev-1",
		Operation:      "create",
		Payload:        string(payloadBytes),
		IdempotencyKey: "idem-1",
		State:          models.QueuePending,
		AttemptCount:   0,
		CreatedAt:      mockClk.Now(),
		NextAttemptAt:  mockClk.Now(),
	}
	_ = store.EnqueueMutation(ctx, mut)

	err := engine.ProcessBatch(ctx, 10)
	if err == nil {
		t.Fatalf("expected error from permanent failure")
	}

	updated, err := store.GetMutation(ctx, "mut-1")
	if err != nil {
		t.Fatalf("failed to retrieve mutation: %v", err)
	}

	if updated.State != models.QueueDeadLetter {
		t.Errorf("expected immediate dead_letter on permanent error, got %s", updated.State)
	}
	if updated.AttemptCount != 1 {
		t.Errorf("expected attempt count 1, got %d", updated.AttemptCount)
	}
}

func TestQueue_TransientError_RetryWithBackoff(t *testing.T) {
	_, store, cleanup := setupQueueTest(t)
	defer cleanup()

	ctx := context.Background()
	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC))
	engine := queue.NewEngine(store, &mockTokenProvider{token: "valid-tok"}, 5, 10*time.Second)
	engine.SetClock(mockClk)

	prov := &mockCalendarProvider{
		createFunc: func(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error) {
			return nil, errdefs.Retryable("rate limited by provider", errors.New("HTTP 429"))
		},
	}
	engine.RegisterProvider(prov)

	p := &models.Person{ID: "p-2", DisplayName: "Sam", Email: "sam@example.com"}
	_ = store.UpsertPerson(ctx, p)
	oa := &models.OAuthAccount{ID: "oa-2", PersonID: "p-2", Provider: "google", Status: "active"}
	_ = store.UpsertOAuthAccount(ctx, oa)
	cal := &models.Calendar{ID: "cal-2", OAuthAccountID: "oa-2", Provider: "google", ProviderCalendarID: "primary", Name: "Google Cal"}
	_ = store.UpsertCalendar(ctx, cal)

	ev := &models.Event{ID: "ev-2", CalendarID: "cal-2", Title: "Dentist"}
	_ = store.UpsertEvent(ctx, ev)

	payloadBytes, _ := json.Marshal(ev)
	mut := &models.MutationQueueItem{
		ID:             "mut-2",
		CalendarID:     "cal-2",
		Provider:       "google",
		LocalEventID:   "ev-2",
		Operation:      "create",
		Payload:        string(payloadBytes),
		IdempotencyKey: "idem-2",
		State:          models.QueuePending,
		AttemptCount:   0,
		CreatedAt:      mockClk.Now(),
		NextAttemptAt:  mockClk.Now(),
	}
	_ = store.EnqueueMutation(ctx, mut)

	_ = engine.ProcessBatch(ctx, 10)

	updated, err := store.GetMutation(ctx, "mut-2")
	if err != nil {
		t.Fatalf("failed to retrieve mutation: %v", err)
	}

	if updated.State != models.QueueRetry {
		t.Errorf("expected state retry, got %s", updated.State)
	}
	if updated.AttemptCount != 1 {
		t.Errorf("expected attempt count 1, got %d", updated.AttemptCount)
	}
	if !updated.NextAttemptAt.After(mockClk.Now()) {
		t.Errorf("expected NextAttemptAt to be in the future, got %v <= %v", updated.NextAttemptAt, mockClk.Now())
	}
}

func TestQueue_LocalMutationSucceedsImmediately(t *testing.T) {
	_, store, cleanup := setupQueueTest(t)
	defer cleanup()

	ctx := context.Background()
	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC))
	engine := queue.NewEngine(store, nil, 3, 5*time.Second)
	engine.SetClock(mockClk)

	cal := &models.Calendar{ID: "cal-local", Provider: "local", Name: "Local Cal"}
	_ = store.UpsertCalendar(ctx, cal)
	ev := &models.Event{ID: "ev-loc", CalendarID: "cal-local", Title: "Homework"}
	_ = store.UpsertEvent(ctx, ev)

	payloadBytes, _ := json.Marshal(ev)
	mut := &models.MutationQueueItem{
		ID:             "mut-loc",
		CalendarID:     "cal-local",
		Provider:       "local",
		LocalEventID:   "ev-loc",
		Operation:      "create",
		Payload:        string(payloadBytes),
		IdempotencyKey: "idem-loc",
		State:          models.QueuePending,
		AttemptCount:   0,
		CreatedAt:      mockClk.Now(),
		NextAttemptAt:  mockClk.Now(),
	}
	_ = store.EnqueueMutation(ctx, mut)

	if err := engine.ProcessBatch(ctx, 10); err != nil {
		t.Fatalf("expected local mutation to succeed without error, got %v", err)
	}

	updated, _ := store.GetMutation(ctx, "mut-loc")
	if updated.State != models.QueueCompleted {
		t.Errorf("expected state completed, got %s", updated.State)
	}
	if engine.Status().ItemsProcessed != 1 {
		t.Errorf("expected 1 item processed, got %d", engine.Status().ItemsProcessed)
	}
}
