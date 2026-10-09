package sync

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar"
	"github.com/Hardonian/FamilyBoard/internal/calendar/google"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/queue"
)

type Engine struct {
	store           *db.Store
	tokenProvider   queue.TokenProvider
	providers       map[string]calendar.Provider
	interval        time.Duration
	stopCh          chan struct{}
	wg              sync.WaitGroup
	mu              sync.Mutex
	running         bool
	onSyncCompleted func()
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

func (e *Engine) SetOnSyncCompleted(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onSyncCompleted = fn
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

// DiscoverCalendars queries all registered providers for active OAuth accounts and auto-registers calendars.
func (e *Engine) DiscoverCalendars(ctx context.Context) error {
	accounts, err := e.store.ListOAuthAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list oauth accounts for discovery: %w", err)
	}

	if e.tokenProvider == nil {
		return nil
	}

	for _, acc := range accounts {
		if acc.Status != "active" {
			continue
		}

		e.mu.Lock()
		prov, ok := e.providers[acc.Provider]
		e.mu.Unlock()
		if !ok {
			continue
		}

		token, err := e.tokenProvider.GetValidAccessToken(ctx, acc.PersonID, acc.Provider)
		if err != nil {
			slog.Warn("Could not get access token for discovery", "account", acc.VerifiedEmail, "err", err)
			continue
		}

		remoteCals, err := prov.ListCalendars(ctx, token)
		if err != nil {
			slog.Warn("Failed to list calendars from provider", "provider", acc.Provider, "account", acc.VerifiedEmail, "err", err)
			continue
		}

		defaultCal, _ := e.store.GetDefaultWriteCalendar(ctx)
		hasDefault := (defaultCal != nil)

		for _, rc := range remoteCals {
			existing, err := e.store.GetCalendarByProviderID(ctx, rc.Provider, rc.ProviderCalendarID)
			if err != nil {
				slog.Warn("Failed to check existing calendar", "id", rc.ProviderCalendarID, "err", err)
				continue
			}

			calID := rc.ID
			if existing != nil {
				calID = existing.ID
			} else if calID == "" {
				h := sha256.Sum256([]byte(rc.ProviderCalendarID))
				calID = fmt.Sprintf("cal-%s-%x", rc.Provider[:1], h[:6])
			}

			nameLower := strings.ToLower(rc.Name)
			isHomeCal := strings.Contains(nameLower, "home") || strings.Contains(nameLower, "family")

			displayColor := rc.DisplayColor
			if existing != nil && existing.DisplayColor != "" {
				displayColor = existing.DisplayColor
			} else if isHomeCal {
				displayColor = "#10b981" // Distinctive emerald home color
			}

			enabled := true
			if existing != nil {
				enabled = existing.EnabledForDisplay
			}

			isDefault := false
			if isHomeCal {
				isDefault = true
			} else if existing != nil {
				isDefault = existing.DefaultWriteTarget
			} else if !hasDefault && rc.DefaultWriteTarget {
				isDefault = true
				hasDefault = true
			}

			calToSave := &models.Calendar{
				ID:                 calID,
				OAuthAccountID:     acc.ID,
				Provider:           rc.Provider,
				ProviderCalendarID: rc.ProviderCalendarID,
				Name:               rc.Name,
				Timezone:           rc.Timezone,
				DisplayColor:       displayColor,
				EnabledForDisplay:  enabled,
				Writable:           rc.Writable,
				DefaultWriteTarget: isDefault,
			}

			if err := e.store.UpsertCalendar(ctx, calToSave); err != nil {
				slog.Warn("Failed to upsert discovered calendar", "name", rc.Name, "err", err)
			} else {
				if isHomeCal {
					_ = e.store.SetDefaultWriteCalendar(ctx, calID)
				}
				slog.Info("Discovered calendar", "name", rc.Name, "id", calID, "provider_id", rc.ProviderCalendarID, "is_home", isHomeCal)
			}
		}
	}

	return nil
}

// SyncAll syncs all active calendars across all accounts.
func (e *Engine) SyncAll(ctx context.Context) error {
	// 1. Auto-discover any new/shared calendars from Google/Microsoft
	if err := e.DiscoverCalendars(ctx); err != nil {
		slog.Warn("Calendar discovery encountered an issue", "err", err)
	}

	// 2. Fetch and synchronize events
	calendars, err := e.store.ListCalendars(ctx)
	if err != nil {
		return fmt.Errorf("list calendars for sync: %w", err)
	}

	syncedAny := false
	for _, cal := range calendars {
		if !cal.EnabledForDisplay && !cal.Writable {
			continue
		}
		if err := e.SyncCalendar(ctx, &cal); err != nil {
			slog.Warn("Calendar sync failed", "calendar", cal.Name, "id", cal.ID, "err", err)
		} else {
			syncedAny = true
		}
	}

	if syncedAny {
		e.mu.Lock()
		onComplete := e.onSyncCompleted
		e.mu.Unlock()
		if onComplete != nil {
			onComplete()
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

	// Fallback healing: if OAuthAccountID unlinked or empty, match active provider account
	if targetAccount == nil {
		for _, acc := range accounts {
			if acc.Provider == cal.Provider && acc.Status == "active" {
				targetAccount = &acc
				cal.OAuthAccountID = acc.ID
				_ = e.store.UpsertCalendar(ctx, cal)
				break
			}
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
