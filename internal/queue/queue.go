package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
)

type TokenProvider interface {
	GetValidAccessToken(ctx context.Context, personID, provider string) (string, error)
}

type Engine struct {
	store         *db.Store
	tokenProvider TokenProvider
	providers     map[string]calendar.Provider
	maxAttempts   int
	baseInterval  time.Duration
	maxInterval   time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	mu            sync.Mutex
	processMu     sync.Mutex
	running       bool
}

func NewEngine(store *db.Store, tokenProvider TokenProvider, maxAttempts int, baseInterval time.Duration) *Engine {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if baseInterval <= 0 {
		baseInterval = 30 * time.Second
	}
	return &Engine{
		store:         store,
		tokenProvider: tokenProvider,
		providers:     make(map[string]calendar.Provider),
		maxAttempts:   maxAttempts,
		baseInterval:  baseInterval,
		maxInterval:   10 * time.Minute,
		stopCh:        make(chan struct{}),
	}
}

func (e *Engine) RegisterProvider(provider calendar.Provider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[provider.Name()] = provider
}

func (e *Engine) SetTokenProvider(tp TokenProvider) {
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
		ticker := time.NewTicker(e.baseInterval)
		defer ticker.Stop()

		for {
			select {
			case <-e.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = e.ProcessBatch(ctx, 10)
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

// ProcessBatch runs one cycle of processing pending queue items.
func (e *Engine) ProcessBatch(ctx context.Context, batchSize int) error {
	// The HTTP fast-path and the periodic worker can fire together. Serialize
	// claims within this process so the same pending create is never submitted
	// twice before its state transition is visible.
	e.processMu.Lock()
	defer e.processMu.Unlock()

	if batchSize <= 0 {
		batchSize = 10
	}
	items, err := e.store.GetPendingMutations(ctx, batchSize)
	if err != nil {
		return fmt.Errorf("get pending mutations: %w", err)
	}

	var processErrors []error
	for _, item := range items {
		if err := e.ProcessItem(ctx, &item); err != nil {
			slog.Warn("Failed to process mutation queue item", "id", item.ID, "operation", item.Operation, "err", err)
			processErrors = append(processErrors, fmt.Errorf("mutation %s: %w", item.ID, err))
		}
	}
	return errors.Join(processErrors...)
}

// ProcessItem executes an individual mutation against the appropriate calendar provider.
func (e *Engine) ProcessItem(ctx context.Context, item *models.MutationQueueItem) error {
	e.mu.Lock()
	prov, ok := e.providers[item.Provider]
	tokenProvider := e.tokenProvider
	e.mu.Unlock()
	if !ok {
		return e.failItem(ctx, item, fmt.Errorf("provider %s not registered", item.Provider))
	}

	// Retrieve calendar to identify person/account
	cal, err := e.store.GetCalendar(ctx, item.CalendarID)
	if err != nil {
		return e.failItem(ctx, item, fmt.Errorf("get calendar %s for mutation: %w", item.CalendarID, err))
	}
	if cal == nil {
		return e.failItem(ctx, item, fmt.Errorf("calendar %s not found for mutation", item.CalendarID))
	}
	if cal.Provider != item.Provider {
		return e.failItem(ctx, item, fmt.Errorf("mutation provider %q does not match calendar provider %q", item.Provider, cal.Provider))
	}

	// Resolve only the exact account referenced by this calendar. Never heal an
	// orphan by selecting the first account for a provider: on a family device
	// that can send one person's event with another person's credentials.
	targetAccount, err := e.store.GetOAuthAccountByID(ctx, cal.OAuthAccountID)
	if err != nil {
		return e.failItem(ctx, item, fmt.Errorf("get oauth account %s: %w", cal.OAuthAccountID, err))
	}
	if targetAccount == nil {
		return e.failItem(ctx, item, fmt.Errorf("oauth account %s not found for calendar", cal.OAuthAccountID))
	}
	if targetAccount.Provider != item.Provider {
		return e.failItem(ctx, item, fmt.Errorf("oauth account provider %q does not match mutation provider %q", targetAccount.Provider, item.Provider))
	}
	if targetAccount.Status != "active" {
		return e.failItem(ctx, item, fmt.Errorf("oauth account %s is %s", targetAccount.ID, targetAccount.Status))
	}

	if tokenProvider == nil {
		return e.failItem(ctx, item, fmt.Errorf("token provider not configured"))
	}

	token, err := tokenProvider.GetValidAccessToken(ctx, targetAccount.PersonID, item.Provider)
	if err != nil {
		return e.failItem(ctx, item, fmt.Errorf("get access token: %w", err))
	}

	// Mark processing
	if err := e.store.UpdateMutationState(ctx, item.ID, models.QueueProcessing, item.AttemptCount, time.Now(), ""); err != nil {
		return fmt.Errorf("mark mutation processing: %w", err)
	}

	var processErr error
	switch item.Operation {
	case "create":
		processErr = e.handleCreate(ctx, prov, token, cal, item)
	case "update":
		processErr = e.handleUpdate(ctx, prov, token, cal, item)
	case "delete":
		processErr = e.handleDelete(ctx, prov, token, cal, item)
	default:
		processErr = fmt.Errorf("unknown operation %q", item.Operation)
	}

	if processErr != nil {
		return e.failItem(ctx, item, processErr)
	}

	// Success: mark completed
	return e.store.UpdateMutationState(ctx, item.ID, models.QueueCompleted, item.AttemptCount+1, time.Now(), "")
}

func (e *Engine) handleCreate(ctx context.Context, prov calendar.Provider, token string, cal *models.Calendar, item *models.MutationQueueItem) error {
	var ev models.Event
	if err := json.Unmarshal([]byte(item.Payload), &ev); err != nil {
		return fmt.Errorf("unmarshal event payload: %w", err)
	}

	created, err := prov.CreateEvent(ctx, token, cal.ProviderCalendarID, &ev, item.IdempotencyKey)
	if err != nil {
		return err
	}

	// Update local event with remote identifiers
	localEv, err := e.store.GetEvent(ctx, item.LocalEventID)
	if err == nil && localEv != nil {
		localEv.ProviderEventID = created.ProviderEventID
		localEv.ProviderUID = created.ProviderUID
		localEv.ETag = created.ETag
		localEv.Version = created.Version
		localEv.RemoteCreatedAt = created.RemoteCreatedAt
		localEv.RemoteUpdatedAt = created.RemoteUpdatedAt
		localEv.Source = "remote_sync"
		if err := e.store.UpsertEvent(ctx, localEv); err != nil {
			return fmt.Errorf("persist remote identifiers: %w", err)
		}
	}

	return nil
}

func (e *Engine) handleUpdate(ctx context.Context, prov calendar.Provider, token string, cal *models.Calendar, item *models.MutationQueueItem) error {
	var ev models.Event
	if err := json.Unmarshal([]byte(item.Payload), &ev); err != nil {
		return fmt.Errorf("unmarshal event payload: %w", err)
	}

	updated, err := prov.UpdateEvent(ctx, token, cal.ProviderCalendarID, &ev)
	if err != nil {
		if errors.Is(err, oauth.ErrAccountRevoked) {
			return err
		}
		// Check if remote event was 404 (deleted remotely)
		return err
	}

	localEv, err := e.store.GetEvent(ctx, item.LocalEventID)
	if err == nil && localEv != nil {
		localEv.ETag = updated.ETag
		localEv.Version = updated.Version
		localEv.RemoteUpdatedAt = updated.RemoteUpdatedAt
		if err := e.store.UpsertEvent(ctx, localEv); err != nil {
			return fmt.Errorf("persist remote update metadata: %w", err)
		}
	}

	return nil
}

func (e *Engine) handleDelete(ctx context.Context, prov calendar.Provider, token string, cal *models.Calendar, item *models.MutationQueueItem) error {
	localEv, err := e.store.GetEvent(ctx, item.LocalEventID)
	if err != nil || localEv == nil {
		return nil // Already gone locally
	}

	if localEv.ProviderEventID == "" {
		// Event was created offline and never synced to remote provider
		return nil
	}

	return prov.DeleteEvent(ctx, token, cal.ProviderCalendarID, localEv.ProviderEventID)
}

func (e *Engine) failItem(ctx context.Context, item *models.MutationQueueItem, cause error) error {
	if stateErr := e.handleFailure(ctx, item, cause.Error()); stateErr != nil {
		return errors.Join(cause, fmt.Errorf("persist mutation failure: %w", stateErr))
	}
	return cause
}

func (e *Engine) handleFailure(ctx context.Context, item *models.MutationQueueItem, errMsg string) error {
	attempts := item.AttemptCount + 1
	if attempts >= e.maxAttempts {
		if err := e.store.UpdateMutationState(ctx, item.ID, models.QueueDeadLetter, attempts, time.Now(), errMsg); err != nil {
			return err
		}
		slog.Error("Mutation item exceeded max retries, moved to dead-letter", "id", item.ID, "attempts", attempts, "err", errMsg)
		return nil
	}

	// Exponential backoff + jitter
	backoff := e.maxInterval
	if attempts < 62 {
		factor := time.Duration(1) << uint(attempts)
		if e.baseInterval <= e.maxInterval/factor {
			backoff = factor * e.baseInterval
		}
	}
	jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
	nextAttempt := time.Now().Add(backoff + jitter)

	return e.store.UpdateMutationState(ctx, item.ID, models.QueueRetry, attempts, nextAttempt, errMsg)
}
