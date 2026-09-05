package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar"
	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/queue"
)

type Engine struct {
	store         *db.Store
	tokenProvider queue.TokenProvider
	providers     map[string]calendar.Provider
	interval      time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	mu            sync.Mutex
	running       bool
}

func NewEngine(store *db.Store, tokenProvider queue.TokenProvider, interval time.Duration) *Engine {
	if interval <= 0 {
		interval = 3 * time.Minute
	}
	return &Engine{
		store:         store,
		tokenProvider: tokenProvider,
		providers:     make(map[string]calendar.Provider),
		interval:      interval,
		stopCh:        make(chan struct{}),
	}
}

func (e *Engine) RegisterProvider(p calendar.Provider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[p.Name()] = p
}

func (e *Engine) SetTokenProvider(tp queue.TokenProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tokenProvider = tp
}

func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.stopCh = make(chan struct{})
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		// Initial sync run on startup
		_ = e.SyncAll(ctx)

		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()

		for {
			select {
			case <-e.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = e.SyncAll(ctx)
			}
		}
	}()
}

func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stopCh)
	e.mu.Unlock()
	e.wg.Wait()
}

// SyncAll syncs all active calendars across all accounts.
func (e *Engine) SyncAll(ctx context.Context) error {
	calendars, err := e.store.ListCalendars(ctx)
	if err != nil {
		return fmt.Errorf("list calendars for sync: %w", err)
	}

	for _, cal := range calendars {
		if !cal.EnabledForDisplay && !cal.Writable {
			continue
		}
		if err := e.SyncCalendar(ctx, &cal); err != nil {
			slog.Warn("Calendar sync failed", "calendar", cal.Name, "id", cal.ID, "err", err)
		}
	}
	return nil
}

// SyncCalendar performs full or incremental sync for a single calendar.
func (e *Engine) SyncCalendar(ctx context.Context, cal *models.Calendar) error {
	e.mu.Lock()
	prov, ok := e.providers[cal.Provider]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("provider %s not registered", cal.Provider)
	}

	accounts, err := e.store.ListOAuthAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list oauth accounts: %w", err)
	}
	var targetAccount *models.OAuthAccount
	for _, acc := range accounts {
		if acc.ID == cal.OAuthAccountID {
			targetAccount = &acc
			break
		}
	}
	if targetAccount == nil {
		return fmt.Errorf("oauth account %s not found", cal.OAuthAccountID)
	}

	if e.tokenProvider == nil {
		return fmt.Errorf("token provider not configured")
	}

	token, err := e.tokenProvider.GetValidAccessToken(ctx, targetAccount.PersonID, cal.Provider)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	result, err := prov.SyncEvents(ctx, token, *cal, cal.SyncCursor)
	if err != nil {
		if errors.Is(err, google.ErrCursorInvalid) {
			slog.Info("Sync cursor invalid (410 Gone); resetting cursor and re-syncing", "calendar", cal.Name)
			_ = e.store.UpdateCalendarSyncCursor(ctx, cal.ID, "", time.Now())
			cal.SyncCursor = ""
			result, err = prov.SyncEvents(ctx, token, *cal, "")
			if err != nil {
				return fmt.Errorf("full sync after 410 failed: %w", err)
			}
		} else {
			return fmt.Errorf("sync events from provider: %w", err)
		}
	}

	// Reconcile each event into SQLite
	for _, remoteEv := range result.Events {
		if err := e.reconcileEvent(ctx, cal, &remoteEv); err != nil {
			slog.Warn("Failed to reconcile event", "title", remoteEv.Title, "err", err)
		}
	}

	// Update cursor and timestamp
	if result.SyncCursor != "" {
		if err := e.store.UpdateCalendarSyncCursor(ctx, cal.ID, result.SyncCursor, time.Now()); err != nil {
			return fmt.Errorf("update calendar sync cursor: %w", err)
		}
	}

	return nil
}

func (e *Engine) reconcileEvent(ctx context.Context, cal *models.Calendar, remoteEv *models.Event) error {
	existing, err := e.store.GetEventByProviderID(ctx, cal.ID, remoteEv.ProviderEventID)
	if err != nil {
		return err
	}

	if existing != nil {
		// Check for conflicts: is there an un-synced local edit for this event?
		pendingMutations, _ := e.store.GetPendingMutations(ctx, 50)
		for _, m := range pendingMutations {
			if m.LocalEventID == existing.ID && m.State != models.QueueCompleted {
				// Local mutation pending while remote event changed!
				if existing.ETag != remoteEv.ETag && remoteEv.ETag != "" {
					localPayload, _ := json.Marshal(existing)
					remotePayload, _ := json.Marshal(remoteEv)
					conflict := &models.Conflict{
						ID:            fmt.Sprintf("conf-%d", time.Now().UnixNano()),
						LocalEventID:  existing.ID,
						RemoteEventID: remoteEv.ProviderEventID,
						ConflictType:  "concurrent_edit",
						LocalPayload:  string(localPayload),
						RemotePayload: string(remotePayload),
						State:         "pending",
					}
					_ = e.store.CreateConflict(ctx, conflict)
					slog.Warn("Conflict detected on event", "title", existing.Title, "local_id", existing.ID)
					return nil // Do not overwrite local changes until resolved
				}
			}
		}

		// Update existing event ID so we update the local record
		remoteEv.ID = existing.ID
		remoteEv.LocalCreatedAt = existing.LocalCreatedAt
	}

	return e.store.UpsertEvent(ctx, remoteEv)
}
