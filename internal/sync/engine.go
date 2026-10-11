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
	"github.com/Hardonian/FamilyBoard/internal/clock"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/metrics"
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
	syncRunMu       sync.Mutex
	running         bool
	onSyncCompleted func()
	clock           clock.Clock
	status          models.WorkerStatus
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
		clock:         clock.NewRealClock(),
		status: models.WorkerStatus{
			Name:   "calendar_sync",
			Health: "stopped",
		},
	}
}

func (e *Engine) SetClock(clk clock.Clock) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if clk != nil {
		e.clock = clk
	}
}

func (e *Engine) Status() models.WorkerStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.status
	st.Running = e.running
	if !e.running {
		st.Health = "stopped"
	} else if st.LastError != "" {
		st.Health = "degraded"
	} else {
		st.Health = "healthy"
	}
	return st
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
		defer func() {
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
			e.wg.Done()
		}()
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
	_ = e.StopWithTimeout(5 * time.Second)
}

func (e *Engine) StopWithTimeout(timeout time.Duration) error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	close(e.stopCh)
	e.mu.Unlock()

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("sync engine shutdown timed out after %v", timeout)
	}
}

// DiscoverCalendars queries all registered providers for active OAuth accounts and auto-registers calendars.
func (e *Engine) DiscoverCalendars(ctx context.Context) error {
	accounts, err := e.store.ListOAuthAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list oauth accounts for discovery: %w", err)
	}

	e.mu.Lock()
	tokenProvider := e.tokenProvider
	e.mu.Unlock()
	if tokenProvider == nil {
		return nil
	}

	var discoveryErrors []error
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

		token, err := tokenProvider.GetValidAccessToken(ctx, acc.PersonID, acc.Provider)
		if err != nil {
			slog.Warn("Could not get access token for discovery", "account", acc.VerifiedEmail, "err", err)
			discoveryErrors = append(discoveryErrors, fmt.Errorf("discover %s account %s: %w", acc.Provider, acc.ID, err))
			continue
		}

		remoteCals, err := prov.ListCalendars(ctx, token)
		if err != nil {
			slog.Warn("Failed to list calendars from provider", "provider", acc.Provider, "account", acc.VerifiedEmail, "err", err)
			discoveryErrors = append(discoveryErrors, fmt.Errorf("list %s calendars for account %s: %w", acc.Provider, acc.ID, err))
			continue
		}

		defaultCal, _ := e.store.GetDefaultWriteCalendar(ctx)
		hasDefault := (defaultCal != nil)

		for _, rc := range remoteCals {
			providerName := strings.TrimSpace(rc.Provider)
			if providerName == "" {
				providerName = acc.Provider
			}
			if providerName != acc.Provider {
				err := fmt.Errorf("provider returned calendar for %q while discovering %q account", providerName, acc.Provider)
				discoveryErrors = append(discoveryErrors, err)
				slog.Warn("Ignoring cross-provider calendar discovery result", "account", acc.ID, "err", err)
				continue
			}

			existing, err := e.store.GetCalendarByProviderID(ctx, providerName, rc.ProviderCalendarID)
			if err != nil {
				slog.Warn("Failed to check existing calendar", "id", rc.ProviderCalendarID, "err", err)
				discoveryErrors = append(discoveryErrors, fmt.Errorf("look up discovered calendar %s: %w", rc.ProviderCalendarID, err))
				continue
			}
			if existing != nil && existing.OAuthAccountID != acc.ID {
				slog.Info("Calendar already linked to another household account; preserving existing owner", "calendar", existing.ID, "owner_account", existing.OAuthAccountID, "discovered_account", acc.ID)
				continue
			}

			calID := rc.ID
			if existing != nil {
				calID = existing.ID
			} else if calID == "" {
				h := sha256.Sum256([]byte(rc.ProviderCalendarID))
				calID = fmt.Sprintf("cal-%s-%x", providerName[:1], h[:6])
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
			if existing != nil {
				isDefault = existing.DefaultWriteTarget
			} else if isHomeCal && !hasDefault {
				isDefault = true
				hasDefault = true
			}

			calToSave := &models.Calendar{
				ID:                 calID,
				OAuthAccountID:     acc.ID,
				Provider:           providerName,
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
				discoveryErrors = append(discoveryErrors, fmt.Errorf("save discovered calendar %s: %w", rc.ProviderCalendarID, err))
			} else {
				if isHomeCal {
					_ = e.store.SetDefaultWriteCalendar(ctx, calID)
				}
				slog.Info("Discovered calendar", "name", rc.Name, "id", calID, "provider_id", rc.ProviderCalendarID, "is_home", isHomeCal)
			}
		}
	}

	return errors.Join(discoveryErrors...)
}

// SyncAll syncs all active calendars across all accounts.
func (e *Engine) SyncAll(ctx context.Context) error {
	e.syncRunMu.Lock()
	defer e.syncRunMu.Unlock()

	e.mu.Lock()
	e.status.LastRunAt = e.clock.Now()
	e.mu.Unlock()

	var syncErrors []error
	// 1. Auto-discover any new/shared calendars from Google/Microsoft
	if err := e.DiscoverCalendars(ctx); err != nil {
		slog.Warn("Calendar discovery encountered an issue", "err", err)
		syncErrors = append(syncErrors, err)
	}

	// 2. Fetch and synchronize events
	calendars, err := e.store.ListCalendars(ctx)
	if err != nil {
		e.mu.Lock()
		e.status.LastError = err.Error()
		e.mu.Unlock()
		return fmt.Errorf("list calendars for sync: %w", err)
	}

	syncedAny := false
	for _, cal := range calendars {
		if !cal.EnabledForDisplay && !cal.Writable {
			continue
		}
		if err := e.SyncCalendar(ctx, &cal); err != nil {
			slog.Warn("Calendar sync failed", "calendar", cal.Name, "id", cal.ID, "err", err)
			syncErrors = append(syncErrors, fmt.Errorf("sync calendar %s: %w", cal.ID, err))
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

	if len(syncErrors) > 0 {
		joined := errors.Join(syncErrors...)
		e.mu.Lock()
		e.status.LastError = joined.Error()
		e.mu.Unlock()
		return joined
	}

	e.mu.Lock()
	e.status.LastError = ""
	e.status.LastSuccessAt = e.clock.Now()
	e.mu.Unlock()
	return nil
}

// SyncCalendar performs full or incremental sync for a single calendar.
func (e *Engine) SyncCalendar(ctx context.Context, cal *models.Calendar) (err error) {
	if cal.Provider == "local" {
		return nil
	}

	start := e.clock.Now()
	defer func() {
		metrics.Default().RecordSync(cal.Provider, e.clock.Now().Sub(start), err == nil)
	}()

	e.mu.Lock()
	prov, ok := e.providers[cal.Provider]
	tokenProvider := e.tokenProvider
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("provider %s not registered", cal.Provider)
	}

	targetAccount, err := e.store.GetOAuthAccountByID(ctx, cal.OAuthAccountID)
	if err != nil {
		return fmt.Errorf("get oauth account %s: %w", cal.OAuthAccountID, err)
	}
	if targetAccount == nil {
		return fmt.Errorf("oauth account %s not found", cal.OAuthAccountID)
	}
	if targetAccount.Provider != cal.Provider {
		return fmt.Errorf("oauth account provider %q does not match calendar provider %q", targetAccount.Provider, cal.Provider)
	}
	if targetAccount.Status != "active" {
		return fmt.Errorf("oauth account %s is %s", targetAccount.ID, targetAccount.Status)
	}

	if tokenProvider == nil {
		return fmt.Errorf("token provider not configured")
	}

	token, err := tokenProvider.GetValidAccessToken(ctx, targetAccount.PersonID, cal.Provider)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	result, err := prov.SyncEvents(ctx, token, *cal, cal.SyncCursor)
	if err != nil {
		if errors.Is(err, google.ErrCursorInvalid) {
			slog.Info("Sync cursor invalid (410 Gone); resetting cursor and re-syncing", "calendar", cal.Name)
			if err := e.store.UpdateCalendarSyncCursor(ctx, cal.ID, "", e.clock.Now()); err != nil {
				return fmt.Errorf("reset invalid sync cursor: %w", err)
			}
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
	var reconcileErrors []error
	for _, remoteEv := range result.Events {
		if err := e.reconcileEvent(ctx, cal, &remoteEv); err != nil {
			slog.Warn("Failed to reconcile event", "title", remoteEv.Title, "err", err)
			reconcileErrors = append(reconcileErrors, fmt.Errorf("reconcile provider event %s: %w", remoteEv.ProviderEventID, err))
		}
	}
	if err := errors.Join(reconcileErrors...); err != nil {
		return err
	}

	e.mu.Lock()
	e.status.ItemsProcessed += int64(len(result.Events))
	e.mu.Unlock()

	// Update cursor and timestamp
	nextCursor := cal.SyncCursor
	if result.SyncCursor != "" {
		nextCursor = result.SyncCursor
	}
	if err := e.store.UpdateCalendarSyncCursor(ctx, cal.ID, nextCursor, e.clock.Now()); err != nil {
		return fmt.Errorf("update calendar sync cursor: %w", err)
	}

	return nil
}

func (e *Engine) reconcileEvent(ctx context.Context, cal *models.Calendar, remoteEv *models.Event) error {
	existing, err := e.store.GetEventByProviderID(ctx, cal.ID, remoteEv.ProviderEventID)
	if err != nil {
		return err
	}

	if existing != nil {
		hasActiveMutation, err := e.store.HasActiveMutationForEvent(ctx, existing.ID)
		if err != nil {
			return err
		}
		if hasActiveMutation && existing.ETag != remoteEv.ETag && remoteEv.ETag != "" {
			localPayload, err := json.Marshal(existing)
			if err != nil {
				return fmt.Errorf("marshal local conflict payload: %w", err)
			}
			remotePayload, err := json.Marshal(remoteEv)
			if err != nil {
				return fmt.Errorf("marshal remote conflict payload: %w", err)
			}
			conflict := &models.Conflict{
				ID:            fmt.Sprintf("conf-%d", e.clock.Now().UnixNano()),
				LocalEventID:  existing.ID,
				RemoteEventID: remoteEv.ProviderEventID,
				ConflictType:  "concurrent_edit",
				LocalPayload:  string(localPayload),
				RemotePayload: string(remotePayload),
				State:         "pending",
			}
			if err := e.store.CreateConflict(ctx, conflict); err != nil {
				return fmt.Errorf("persist sync conflict: %w", err)
			}
			slog.Warn("Conflict detected on event", "title", existing.Title, "local_id", existing.ID)
			return nil
		}

		remoteEv.ID = existing.ID
		remoteEv.LocalCreatedAt = existing.LocalCreatedAt
	}

	return e.store.UpsertEvent(ctx, remoteEv)
}
