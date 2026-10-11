package tests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar"
	"github.com/Hardonian/FamilyBoard/internal/clock"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/errdefs"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
)

type faultMockTokenProvider struct {
	token string
	err   error
}

func (f *faultMockTokenProvider) GetValidAccessToken(ctx context.Context, personID, provider string) (string, error) {
	return f.token, f.err
}

type faultMockProvider struct {
	syncFunc   func(ctx context.Context, token string, cal models.Calendar, cursor string) (*calendar.SyncResult, error)
	createFunc func(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error)
}

func (f *faultMockProvider) Name() string { return "google" }
func (f *faultMockProvider) ListCalendars(ctx context.Context, token string) ([]models.Calendar, error) {
	return nil, nil
}
func (f *faultMockProvider) SyncEvents(ctx context.Context, token string, cal models.Calendar, cursor string) (*calendar.SyncResult, error) {
	if f.syncFunc != nil {
		return f.syncFunc(ctx, token, cal, cursor)
	}
	return &calendar.SyncResult{}, nil
}
func (f *faultMockProvider) CreateEvent(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error) {
	if f.createFunc != nil {
		return f.createFunc(ctx, token, calID, ev, idemKey)
	}
	return ev, nil
}
func (f *faultMockProvider) UpdateEvent(ctx context.Context, token, calID string, ev *models.Event) (*models.Event, error) {
	return ev, nil
}
func (f *faultMockProvider) DeleteEvent(ctx context.Context, token, calID, eventID string) error {
	return nil
}

func setupFaultDB(t *testing.T) (*db.DB, *db.Store, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "fault-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "fault.db")
	database, err := db.Open(dbPath, 1000, 2, 1)
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

func TestFault_ProviderTimeout(t *testing.T) {
	_, store, cleanup := setupFaultDB(t)
	defer cleanup()

	ctx := context.Background()
	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC))
	engine := queue.NewEngine(store, &faultMockTokenProvider{token: "tok"}, 3, 5*time.Second)
	engine.SetClock(mockClk)

	engine.RegisterProvider(&faultMockProvider{
		createFunc: func(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error) {
			return nil, errdefs.Retryable("upstream timeout", context.DeadlineExceeded)
		},
	})

	oa := &models.OAuthAccount{ID: "oa-to", PersonID: "person-1", Provider: "google", ProviderAccountID: "acc-to", VerifiedEmail: "alex@example.com", Status: "active"}
	if err := store.UpsertOAuthAccount(ctx, oa); err != nil {
		t.Fatalf("upsert oauth: %v", err)
	}
	cal := &models.Calendar{ID: "cal-to", OAuthAccountID: "oa-to", Provider: "google", ProviderCalendarID: "pri-to", Name: "Cal"}
	if err := store.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("upsert cal: %v", err)
	}
	ev := &models.Event{ID: "ev-to", CalendarID: "cal-to", Title: "Timeout Test"}
	if err := store.UpsertEvent(ctx, ev); err != nil {
		t.Fatalf("upsert ev: %v", err)
	}

	b, _ := json.Marshal(ev)
	mut := &models.MutationQueueItem{
		ID:             "mut-to",
		CalendarID:     "cal-to",
		Provider:       "google",
		LocalEventID:   "ev-to",
		Operation:      "create",
		Payload:        string(b),
		IdempotencyKey: "idem-to",
		State:          models.QueuePending,
		CreatedAt:      mockClk.Now(),
		NextAttemptAt:  mockClk.Now(),
	}
	if err := store.EnqueueMutation(ctx, mut); err != nil {
		t.Fatalf("enqueue mutation: %v", err)
	}

	_ = engine.ProcessBatch(ctx, 10)

	updated, err := store.GetMutation(ctx, "mut-to")
	if err != nil || updated == nil {
		t.Fatalf("failed to get mutation: %v", err)
	}
	if updated.State != models.QueueRetry {
		t.Errorf("expected state retry on timeout, got %s", updated.State)
	}
}

func TestFault_HTTP429RateLimit(t *testing.T) {
	_, store, cleanup := setupFaultDB(t)
	defer cleanup()

	ctx := context.Background()
	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC))
	engine := queue.NewEngine(store, &faultMockTokenProvider{token: "tok"}, 3, 10*time.Second)
	engine.SetClock(mockClk)

	engine.RegisterProvider(&faultMockProvider{
		createFunc: func(ctx context.Context, token, calID string, ev *models.Event, idemKey string) (*models.Event, error) {
			return nil, errdefs.Retryable("HTTP 429 Too Many Requests")
		},
	})

	oa := &models.OAuthAccount{ID: "oa-429", PersonID: "person-1", Provider: "google", ProviderAccountID: "acc-429", VerifiedEmail: "alex@example.com", Status: "active"}
	if err := store.UpsertOAuthAccount(ctx, oa); err != nil {
		t.Fatalf("upsert oauth: %v", err)
	}
	cal := &models.Calendar{ID: "cal-429", OAuthAccountID: "oa-429", Provider: "google", ProviderCalendarID: "pri-429", Name: "Cal"}
	if err := store.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("upsert cal: %v", err)
	}
	ev := &models.Event{ID: "ev-429", CalendarID: "cal-429", Title: "Rate Limit"}
	if err := store.UpsertEvent(ctx, ev); err != nil {
		t.Fatalf("upsert ev: %v", err)
	}

	b, _ := json.Marshal(ev)
	mut := &models.MutationQueueItem{
		ID:             "mut-429",
		CalendarID:     "cal-429",
		Provider:       "google",
		LocalEventID:   "ev-429",
		Operation:      "create",
		Payload:        string(b),
		IdempotencyKey: "idem-429",
		State:          models.QueuePending,
		CreatedAt:      mockClk.Now(),
		NextAttemptAt:  mockClk.Now(),
	}
	if err := store.EnqueueMutation(ctx, mut); err != nil {
		t.Fatalf("enqueue mut: %v", err)
	}

	_ = engine.ProcessBatch(ctx, 10)

	updated, err := store.GetMutation(ctx, "mut-429")
	if err != nil || updated == nil {
		t.Fatalf("failed to get mutation: %v", err)
	}
	if updated.State != models.QueueRetry {
		t.Errorf("expected state retry on 429, got %s", updated.State)
	}
	if updated.AttemptCount != 1 {
		t.Errorf("expected attempt 1, got %d", updated.AttemptCount)
	}
}

func TestFault_TokenRevoked(t *testing.T) {
	_, store, cleanup := setupFaultDB(t)
	defer cleanup()

	ctx := context.Background()
	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC))
	engine := queue.NewEngine(store, &faultMockTokenProvider{err: errors.New("refresh token revoked by user")}, 3, 5*time.Second)
	engine.SetClock(mockClk)

	oa := &models.OAuthAccount{ID: "oa-rev", PersonID: "person-1", Provider: "google", ProviderAccountID: "acc-rev", VerifiedEmail: "alex@example.com", Status: "active"}
	if err := store.UpsertOAuthAccount(ctx, oa); err != nil {
		t.Fatalf("upsert oauth: %v", err)
	}
	cal := &models.Calendar{ID: "cal-rev", OAuthAccountID: "oa-rev", Provider: "google", ProviderCalendarID: "pri-rev", Name: "Cal"}
	if err := store.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("upsert cal: %v", err)
	}
	ev := &models.Event{ID: "ev-rev", CalendarID: "cal-rev", Title: "Revoked Test"}
	if err := store.UpsertEvent(ctx, ev); err != nil {
		t.Fatalf("upsert ev: %v", err)
	}

	b, _ := json.Marshal(ev)
	mut := &models.MutationQueueItem{
		ID:             "mut-rev",
		CalendarID:     "cal-rev",
		Provider:       "google",
		LocalEventID:   "ev-rev",
		Operation:      "create",
		Payload:        string(b),
		IdempotencyKey: "idem-rev",
		State:          models.QueuePending,
		CreatedAt:      mockClk.Now(),
		NextAttemptAt:  mockClk.Now(),
	}
	if err := store.EnqueueMutation(ctx, mut); err != nil {
		t.Fatalf("enqueue mut: %v", err)
	}
	engine.RegisterProvider(&faultMockProvider{})

	_ = engine.ProcessBatch(ctx, 10)

	updated, err := store.GetMutation(ctx, "mut-rev")
	if err != nil || updated == nil {
		t.Fatalf("failed to get mutation: %v", err)
	}
	if updated.State != models.QueueRetry {
		t.Errorf("expected retry state waiting for re-auth, got %s", updated.State)
	}
}

func TestFault_MalformedProviderResponseDoesNotAdvanceCursor(t *testing.T) {
	_, store, cleanup := setupFaultDB(t)
	defer cleanup()

	ctx := context.Background()
	mockClk := clock.NewMockClock(time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC))
	syncEngine := calsync.NewEngine(store, &faultMockTokenProvider{token: "tok"}, 1*time.Minute)
	syncEngine.SetClock(mockClk)

	syncEngine.RegisterProvider(&faultMockProvider{
		syncFunc: func(ctx context.Context, token string, cal models.Calendar, cursor string) (*calendar.SyncResult, error) {
			return nil, errors.New("malformed upstream JSON: unexpected EOF")
		},
	})

	oa := &models.OAuthAccount{ID: "oa-mal", PersonID: "person-1", Provider: "google", ProviderAccountID: "acc-mal", VerifiedEmail: "alex@example.com", Status: "active"}
	if err := store.UpsertOAuthAccount(ctx, oa); err != nil {
		t.Fatalf("upsert oauth: %v", err)
	}
	cal := &models.Calendar{
		ID:                 "cal-mal",
		OAuthAccountID:     "oa-mal",
		Provider:           "google",
		ProviderCalendarID: "pri-mal",
		Name:               "Cal",
		SyncCursor:         "initial-cursor",
		EnabledForDisplay:  true,
		Writable:           true,
	}
	if err := store.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("upsert cal: %v", err)
	}

	err := syncEngine.SyncCalendar(ctx, cal)
	if err == nil {
		t.Fatalf("expected error from malformed sync payload")
	}

	fetchedCal, err := store.GetCalendar(ctx, "cal-mal")
	if err != nil || fetchedCal == nil {
		t.Fatalf("failed to get calendar: %v", err)
	}
	if fetchedCal.SyncCursor != "initial-cursor" {
		t.Errorf("cursor must NOT advance on malformed response, got %q", fetchedCal.SyncCursor)
	}
}

func TestFault_PartialWriteTransactionRollback(t *testing.T) {
	database, store, cleanup := setupFaultDB(t)
	defer cleanup()

	ctx := context.Background()

	err := database.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO people (id, display_name, email, enabled, created_at, updated_at)
			VALUES ('p-fail', 'Ghost', 'ghost@example.com', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);`)
		if err != nil {
			return err
		}
		return errors.New("simulated crash mid-transaction")
	})

	if err == nil {
		t.Fatalf("expected simulated error")
	}

	p, _ := store.GetPersonByEmail(ctx, "ghost@example.com")
	if p != nil {
		t.Errorf("expected rolled back transaction to leave no ghost records")
	}
}
