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
	items, err := e.store.GetPendingMutations(ctx, batchSize)
	if err != nil {
		return fmt.Errorf("get pending mutations: %w", err)
	}

	for _, item := range items {
		if err := e.ProcessItem(ctx, &item); err != nil {
			slog.Warn("Failed to process mutation queue item", "id", item.ID, "operation", item.Operation, "err", err)
		}
	}
	return nil
}

// ProcessItem executes an individual mutation against the appropriate calendar provider.
func (e *Engine) ProcessItem(ctx context.Context, item *models.MutationQueueItem) error {
	e.mu.Lock()
	prov, ok := e.providers[item.Provider]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("provider %s not registered", item.Provider)
	}

	// Retrieve calendar to identify person/account
	cal, err := e.store.GetCalendar(ctx, item.CalendarID)
	if err != nil || cal == nil {
		return fmt.Errorf("calendar %s not found for mutation: %w", item.CalendarID, err)
	}

	// Fetch OAuth account for person
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
		return fmt.Errorf("oauth account %s not found for calendar", cal.OAuthAccountID)
	}

	if e.tokenProvider == nil {
		return fmt.Errorf("token provider not configured")
	}

	token, err := e.tokenProvider.GetValidAccessToken(ctx, targetAccount.PersonID, item.Provider)
	if err != nil {
		e.handleFailure(ctx, item, fmt.Sprintf("get access token: %v", err))
		return err
	}

	// Mark processing
	_ = e.store.UpdateMutationState(ctx, item.ID, models.QueueProcessing, item.AttemptCount, time.Now(), "")

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
		e.handleFailure(ctx, item, processErr.Error())
		return processErr
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
		_ = e.store.UpsertEvent(ctx, localEv)
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
		_ = e.store.UpsertEvent(ctx, localEv)
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

func (e *Engine) handleFailure(ctx context.Context, item *models.MutationQueueItem, errMsg string) {
	attempts := item.AttemptCount + 1
	if attempts >= e.maxAttempts {
		_ = e.store.UpdateMutationState(ctx, item.ID, models.QueueDeadLetter, attempts, time.Now(), errMsg)
		slog.Error("Mutation item exceeded max retries, moved to dead-letter", "id", item.ID, "attempts", attempts, "err", errMsg)
		return
	}

	// Exponential backoff + jitter
	factor := 1 << uint(attempts)
	backoff := time.Duration(factor) * e.baseInterval
	if backoff > e.maxInterval {
		backoff = e.maxInterval
	}
	jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
	nextAttempt := time.Now().Add(backoff + jitter)

	_ = e.store.UpdateMutationState(ctx, item.ID, models.QueueRetry, attempts, nextAttempt, errMsg)
}
