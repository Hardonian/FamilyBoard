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
	"github.com/Hardonian/FamilyBoard/internal/clock"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/errdefs"
	"github.com/Hardonian/FamilyBoard/internal/metrics"
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
	clock         clock.Clock
	status        models.WorkerStatus
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
		clock:         clock.NewRealClock(),
		status: models.WorkerStatus{
			Name:   "mutation_queue",
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
		return fmt.Errorf("queue engine shutdown timed out after %v", timeout)
	}
}

// ProcessBatch runs one cycle of processing pending queue items.
func (e *Engine) ProcessBatch(ctx context.Context, batchSize int) error {
	e.processMu.Lock()
	defer e.processMu.Unlock()

	e.mu.Lock()
	e.status.LastRunAt = e.clock.Now()
	e.mu.Unlock()

	if batchSize <= 0 {
		batchSize = 10
	}
	items, err := e.store.GetPendingMutationsBefore(ctx, e.clock.Now(), batchSize)
	if err != nil {
		e.mu.Lock()
		e.status.LastError = err.Error()
		e.mu.Unlock()
		return fmt.Errorf("get pending mutations: %w", err)
	}

	var processErrors []error
	for _, item := range items {
		if err := e.ProcessItem(ctx, &item); err != nil {
			slog.Warn("Failed to process mutation queue item", "id", item.ID, "operation", item.Operation, "err", err)
			processErrors = append(processErrors, fmt.Errorf("mutation %s: %w", item.ID, err))
		}
	}

	pendingCount, _ := e.store.CountPendingMutations(ctx)
	deadCount, _ := e.store.CountDeadLetterMutations(ctx)
	metrics.Default().RecordQueue(pendingCount, 0, deadCount)

	if len(processErrors) > 0 {
		joined := errors.Join(processErrors...)
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

// ProcessItem executes an individual mutation against the appropriate calendar provider.
func (e *Engine) ProcessItem(ctx context.Context, item *models.MutationQueueItem) error {
	now := e.clock.Now()
	if item.Provider == "local" {
		err := e.store.UpdateMutationState(ctx, item.ID, models.QueueCompleted, item.AttemptCount+1, now, "")
		if err == nil {
			e.mu.Lock()
			e.status.ItemsProcessed++
			e.mu.Unlock()
		}
		return err
	}

	e.mu.Lock()
	prov, ok := e.providers[item.Provider]
	tokenProvider := e.tokenProvider
	e.mu.Unlock()
	if !ok {
		return e.failItem(ctx, item, errdefs.Permanent(fmt.Sprintf("provider %s not registered", item.Provider)))
	}

	// Retrieve calendar to identify person/account
	cal, err := e.store.GetCalendar(ctx, item.CalendarID)
	if err != nil {
		return e.failItem(ctx, item, errdefs.Retryable(fmt.Sprintf("get calendar %s for mutation", item.CalendarID), err))
	}
	if cal == nil {
		return e.failItem(ctx, item, errdefs.Permanent(fmt.Sprintf("calendar %s not found for mutation", item.CalendarID)))
	}
	if cal.Provider != item.Provider {
		return e.failItem(ctx, item, errdefs.Permanent(fmt.Sprintf("mutation provider %q does not match calendar provider %q", item.Provider, cal.Provider)))
	}

	targetAccount, err := e.store.GetOAuthAccountByID(ctx, cal.OAuthAccountID)
	if err != nil {
		return e.failItem(ctx, item, errdefs.Retryable(fmt.Sprintf("get oauth account %s", cal.OAuthAccountID), err))
	}
	if targetAccount == nil {
		return e.failItem(ctx, item, errdefs.Permanent(fmt.Sprintf("oauth account %s not found for calendar", cal.OAuthAccountID)))
	}
	if targetAccount.Provider != item.Provider {
		return e.failItem(ctx, item, errdefs.Permanent(fmt.Sprintf("oauth account provider %q does not match mutation provider %q", targetAccount.Provider, item.Provider)))
	}
	if targetAccount.Status != "active" {
		return e.failItem(ctx, item, errdefs.Authentication(fmt.Sprintf("oauth account %s is %s", targetAccount.ID, targetAccount.Status)))
	}

	if tokenProvider == nil {
		return e.failItem(ctx, item, errdefs.Unavailable("token provider not configured"))
	}

	token, err := tokenProvider.GetValidAccessToken(ctx, targetAccount.PersonID, item.Provider)
	if err != nil {
		return e.failItem(ctx, item, errdefs.Authentication("get access token", err))
	}

	// Mark processing
	if err := e.store.UpdateMutationState(ctx, item.ID, models.QueueProcessing, item.AttemptCount, now, ""); err != nil {
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
		processErr = errdefs.Validation(fmt.Sprintf("unknown operation %q", item.Operation))
	}

	if processErr != nil {
		return e.failItem(ctx, item, processErr)
	}

	// Success: mark completed
	if err := e.store.UpdateMutationState(ctx, item.ID, models.QueueCompleted, item.AttemptCount+1, e.clock.Now(), ""); err != nil {
		return err
	}
	e.mu.Lock()
	e.status.ItemsProcessed++
	e.mu.Unlock()
	return nil
}

func (e *Engine) handleCreate(ctx context.Context, prov calendar.Provider, token string, cal *models.Calendar, item *models.MutationQueueItem) error {
	var ev models.Event
	if err := json.Unmarshal([]byte(item.Payload), &ev); err != nil {
		return errdefs.Validation("unmarshal event payload", err)
	}

	created, err := prov.CreateEvent(ctx, token, cal.ProviderCalendarID, &ev, item.IdempotencyKey)
	if err != nil {
		return err
	}

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
		return errdefs.Validation("unmarshal event payload", err)
	}

	updated, err := prov.UpdateEvent(ctx, token, cal.ProviderCalendarID, &ev)
	if err != nil {
		if errors.Is(err, oauth.ErrAccountRevoked) {
			return errdefs.Authentication("account revoked", err)
		}
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
		return nil
	}

	if localEv.ProviderEventID == "" {
		return nil
	}

	return prov.DeleteEvent(ctx, token, cal.ProviderCalendarID, localEv.ProviderEventID)
}

func (e *Engine) failItem(ctx context.Context, item *models.MutationQueueItem, cause error) error {
	if stateErr := e.handleFailure(ctx, item, cause); stateErr != nil {
		return errors.Join(cause, fmt.Errorf("persist mutation failure: %w", stateErr))
	}
	return cause
}

func (e *Engine) handleFailure(ctx context.Context, item *models.MutationQueueItem, cause error) error {
	errMsg := cause.Error()
	attempts := item.AttemptCount + 1

	if errdefs.IsPermanent(cause) || errdefs.IsValidation(cause) || attempts >= e.maxAttempts {
		if err := e.store.UpdateMutationState(ctx, item.ID, models.QueueDeadLetter, attempts, e.clock.Now(), errMsg); err != nil {
			return err
		}
		slog.Error("Mutation item moved to dead-letter", "id", item.ID, "attempts", attempts, "err", errMsg, "permanent", errdefs.IsPermanent(cause))
		return nil
	}

	backoff := e.maxInterval
	if attempts < 62 {
		factor := time.Duration(1) << uint(attempts)
		if e.baseInterval <= e.maxInterval/factor {
			backoff = factor * e.baseInterval
		}
	}
	jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
	nextAttempt := e.clock.Now().Add(backoff + jitter)

	return e.store.UpdateMutationState(ctx, item.ID, models.QueueRetry, attempts, nextAttempt, errMsg)
}
