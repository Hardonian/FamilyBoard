package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

type Store struct {
	db *DB
}

func NewStore(db *DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *DB {
	return s.db
}

// --- People ---

func (s *Store) ListPeople(ctx context.Context) ([]models.Person, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, display_name, email, enabled, created_at, updated_at FROM people ORDER BY display_name ASC;")
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	defer rows.Close()

	var people []models.Person
	for rows.Next() {
		var p models.Person
		var enabled int
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.Email, &enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		p.Enabled = (enabled == 1)
		people = append(people, p)
	}
	return people, rows.Err()
}

func (s *Store) GetPersonByEmail(ctx context.Context, email string) (*models.Person, error) {
	var p models.Person
	var enabled int
	row := s.db.QueryRowContext(ctx, "SELECT id, display_name, email, enabled, created_at, updated_at FROM people WHERE LOWER(email) = LOWER(?);", email)
	if err := row.Scan(&p.ID, &p.DisplayName, &p.Email, &enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get person by email: %w", err)
	}
	p.Enabled = (enabled == 1)
	return &p, nil
}

// --- OAuth Accounts ---

func (s *Store) GetOAuthAccount(ctx context.Context, personID, provider string) (*models.OAuthAccount, error) {
	var acc models.OAuthAccount
	row := s.db.QueryRowContext(ctx, `SELECT id, person_id, provider, provider_account_id, verified_email, scopes, credential_reference, expires_at, status, created_at, updated_at
		FROM oauth_accounts WHERE person_id = ? AND provider = ?;`, personID, provider)
	if err := row.Scan(&acc.ID, &acc.PersonID, &acc.Provider, &acc.ProviderAccountID, &acc.VerifiedEmail, &acc.Scopes, &acc.CredentialReference, &acc.ExpiresAt, &acc.Status, &acc.CreatedAt, &acc.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get oauth account: %w", err)
	}
	return &acc, nil
}

func (s *Store) ListOAuthAccounts(ctx context.Context) ([]models.OAuthAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, person_id, provider, provider_account_id, verified_email, scopes, credential_reference, expires_at, status, created_at, updated_at
		FROM oauth_accounts ORDER BY created_at ASC;`)
	if err != nil {
		return nil, fmt.Errorf("list oauth accounts: %w", err)
	}
	defer rows.Close()

	var accounts []models.OAuthAccount
	for rows.Next() {
		var acc models.OAuthAccount
		if err := rows.Scan(&acc.ID, &acc.PersonID, &acc.Provider, &acc.ProviderAccountID, &acc.VerifiedEmail, &acc.Scopes, &acc.CredentialReference, &acc.ExpiresAt, &acc.Status, &acc.CreatedAt, &acc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan oauth account: %w", err)
		}
		accounts = append(accounts, acc)
	}
	return accounts, rows.Err()
}

func (s *Store) UpsertOAuthAccount(ctx context.Context, acc *models.OAuthAccount) error {
	query := `INSERT INTO oauth_accounts (id, person_id, provider, provider_account_id, verified_email, scopes, credential_reference, expires_at, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(person_id, provider) DO UPDATE SET
			provider_account_id = excluded.provider_account_id,
			verified_email = excluded.verified_email,
			scopes = excluded.scopes,
			credential_reference = excluded.credential_reference,
			expires_at = excluded.expires_at,
			status = excluded.status,
			updated_at = excluded.updated_at;`
	now := time.Now()
	if acc.CreatedAt.IsZero() {
		acc.CreatedAt = now
	}
	acc.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, query, acc.ID, acc.PersonID, acc.Provider, acc.ProviderAccountID, acc.VerifiedEmail, acc.Scopes, acc.CredentialReference, acc.ExpiresAt, acc.Status, acc.CreatedAt, acc.UpdatedAt)
	return err
}

func (s *Store) DeleteOAuthAccount(ctx context.Context, personID, provider string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM oauth_accounts WHERE person_id = ? AND provider = ?;", personID, provider)
	return err
}

// --- Calendars ---

func (s *Store) ListCalendars(ctx context.Context) ([]models.Calendar, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, oauth_account_id, provider, provider_calendar_id, name, timezone, display_color, enabled_for_display, writable, default_write_target, sync_cursor, last_synced_at, created_at, updated_at
		FROM calendars ORDER BY name ASC;`)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	defer rows.Close()

	var calendars []models.Calendar
	for rows.Next() {
		var c models.Calendar
		var enabled, writable, defaultTarget int
		if err := rows.Scan(&c.ID, &c.OAuthAccountID, &c.Provider, &c.ProviderCalendarID, &c.Name, &c.Timezone, &c.DisplayColor, &enabled, &writable, &defaultTarget, &c.SyncCursor, &c.LastSyncedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan calendar: %w", err)
		}
		c.EnabledForDisplay = (enabled == 1)
		c.Writable = (writable == 1)
		c.DefaultWriteTarget = (defaultTarget == 1)
		calendars = append(calendars, c)
	}
	return calendars, rows.Err()
}

func (s *Store) ListDisplayCalendars(ctx context.Context) ([]models.Calendar, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, oauth_account_id, provider, provider_calendar_id, name, timezone, display_color, enabled_for_display, writable, default_write_target, sync_cursor, last_synced_at, created_at, updated_at
		FROM calendars WHERE enabled_for_display = 1 ORDER BY name ASC;`)
	if err != nil {
		return nil, fmt.Errorf("list display calendars: %w", err)
	}
	defer rows.Close()

	var calendars []models.Calendar
	for rows.Next() {
		var c models.Calendar
		var enabled, writable, defaultTarget int
		if err := rows.Scan(&c.ID, &c.OAuthAccountID, &c.Provider, &c.ProviderCalendarID, &c.Name, &c.Timezone, &c.DisplayColor, &enabled, &writable, &defaultTarget, &c.SyncCursor, &c.LastSyncedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan calendar: %w", err)
		}
		c.EnabledForDisplay = (enabled == 1)
		c.Writable = (writable == 1)
		c.DefaultWriteTarget = (defaultTarget == 1)
		calendars = append(calendars, c)
	}
	return calendars, rows.Err()
}

func (s *Store) GetCalendar(ctx context.Context, id string) (*models.Calendar, error) {
	var c models.Calendar
	var enabled, writable, defaultTarget int
	row := s.db.QueryRowContext(ctx, `SELECT id, oauth_account_id, provider, provider_calendar_id, name, timezone, display_color, enabled_for_display, writable, default_write_target, sync_cursor, last_synced_at, created_at, updated_at
		FROM calendars WHERE id = ?;`, id)
	if err := row.Scan(&c.ID, &c.OAuthAccountID, &c.Provider, &c.ProviderCalendarID, &c.Name, &c.Timezone, &c.DisplayColor, &enabled, &writable, &defaultTarget, &c.SyncCursor, &c.LastSyncedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get calendar: %w", err)
	}
	c.EnabledForDisplay = (enabled == 1)
	c.Writable = (writable == 1)
	c.DefaultWriteTarget = (defaultTarget == 1)
	return &c, nil
}

func (s *Store) GetDefaultWriteCalendar(ctx context.Context) (*models.Calendar, error) {
	var c models.Calendar
	var enabled, writable, defaultTarget int
	row := s.db.QueryRowContext(ctx, `SELECT id, oauth_account_id, provider, provider_calendar_id, name, timezone, display_color, enabled_for_display, writable, default_write_target, sync_cursor, last_synced_at, created_at, updated_at
		FROM calendars WHERE default_write_target = 1 AND writable = 1 LIMIT 1;`)
	if err := row.Scan(&c.ID, &c.OAuthAccountID, &c.Provider, &c.ProviderCalendarID, &c.Name, &c.Timezone, &c.DisplayColor, &enabled, &writable, &defaultTarget, &c.SyncCursor, &c.LastSyncedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			// Fallback to first writable calendar
			fallbackRow := s.db.QueryRowContext(ctx, `SELECT id, oauth_account_id, provider, provider_calendar_id, name, timezone, display_color, enabled_for_display, writable, default_write_target, sync_cursor, last_synced_at, created_at, updated_at
				FROM calendars WHERE writable = 1 LIMIT 1;`)
			if fErr := fallbackRow.Scan(&c.ID, &c.OAuthAccountID, &c.Provider, &c.ProviderCalendarID, &c.Name, &c.Timezone, &c.DisplayColor, &enabled, &writable, &defaultTarget, &c.SyncCursor, &c.LastSyncedAt, &c.CreatedAt, &c.UpdatedAt); fErr != nil {
				if fErr == sql.ErrNoRows {
					return nil, nil
				}
				return nil, fmt.Errorf("fallback default write calendar: %w", fErr)
			}
		} else {
			return nil, fmt.Errorf("get default write calendar: %w", err)
		}
	}
	c.EnabledForDisplay = (enabled == 1)
	c.Writable = (writable == 1)
	c.DefaultWriteTarget = (defaultTarget == 1)
	return &c, nil
}

func (s *Store) UpsertCalendar(ctx context.Context, c *models.Calendar) error {
	query := `INSERT INTO calendars (id, oauth_account_id, provider, provider_calendar_id, name, timezone, display_color, enabled_for_display, writable, default_write_target, sync_cursor, last_synced_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, provider_calendar_id) DO UPDATE SET
			name = excluded.name,
			timezone = excluded.timezone,
			updated_at = excluded.updated_at;`
	now := time.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now

	enabled := 0
	if c.EnabledForDisplay {
		enabled = 1
	}
	writable := 0
	if c.Writable {
		writable = 1
	}
	defaultTarget := 0
	if c.DefaultWriteTarget {
		defaultTarget = 1
	}

	_, err := s.db.ExecContext(ctx, query, c.ID, c.OAuthAccountID, c.Provider, c.ProviderCalendarID, c.Name, c.Timezone, c.DisplayColor, enabled, writable, defaultTarget, c.SyncCursor, c.LastSyncedAt, c.CreatedAt, c.UpdatedAt)
	return err
}

func (s *Store) SetDefaultWriteCalendar(ctx context.Context, calendarID string) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE calendars SET default_write_target = 0;"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE calendars SET default_write_target = 1, writable = 1 WHERE id = ?;", calendarID)
		return err
	})
}

func (s *Store) UpdateCalendarPreferences(ctx context.Context, calendarID string, enabledForDisplay, writable bool, displayColor string) error {
	en := 0
	if enabledForDisplay {
		en = 1
	}
	wr := 0
	if writable {
		wr = 1
	}
	_, err := s.db.ExecContext(ctx, "UPDATE calendars SET enabled_for_display = ?, writable = ?, display_color = ?, updated_at = ? WHERE id = ?;",
		en, wr, displayColor, time.Now(), calendarID)
	return err
}

func (s *Store) UpdateCalendarSyncCursor(ctx context.Context, calendarID, syncCursor string, syncedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE calendars SET sync_cursor = ?, last_synced_at = ?, updated_at = ? WHERE id = ?;",
		syncCursor, syncedAt, time.Now(), calendarID)
	return err
}

// --- Events ---

func (s *Store) ListEventsRange(ctx context.Context, start, end time.Time) ([]models.Event, error) {
	query := `SELECT e.id, e.provider, e.provider_event_id, e.provider_uid, e.calendar_id, e.etag, e.version, e.title, e.description, e.location,
		e.start_at, e.end_at, e.timezone, e.all_day, e.status, e.recurrence_rule, e.recurrence_parent, e.organizer, e.source,
		e.remote_created_at, e.remote_updated_at, e.local_created_at, e.local_updated_at, e.deleted_at
		FROM events e
		JOIN calendars c ON e.calendar_id = c.id
		WHERE c.enabled_for_display = 1
		  AND e.deleted_at IS NULL
		  AND e.start_at < ? AND e.end_at > ?
		ORDER BY e.start_at ASC;`

	rows, err := s.db.QueryContext(ctx, query, end, start)
	if err != nil {
		return nil, fmt.Errorf("list events range: %w", err)
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var e models.Event
		var allDay int
		if err := rows.Scan(&e.ID, &e.Provider, &e.ProviderEventID, &e.ProviderUID, &e.CalendarID, &e.ETag, &e.Version, &e.Title, &e.Description, &e.Location,
			&e.StartAt, &e.EndAt, &e.Timezone, &allDay, &e.Status, &e.RecurrenceRule, &e.RecurrenceParent, &e.Organizer, &e.Source,
			&e.RemoteCreatedAt, &e.RemoteUpdatedAt, &e.LocalCreatedAt, &e.LocalUpdatedAt, &e.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.AllDay = (allDay == 1)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) GetEvent(ctx context.Context, id string) (*models.Event, error) {
	query := `SELECT id, provider, provider_event_id, provider_uid, calendar_id, etag, version, title, description, location,
		start_at, end_at, timezone, all_day, status, recurrence_rule, recurrence_parent, organizer, source,
		remote_created_at, remote_updated_at, local_created_at, local_updated_at, deleted_at
		FROM events WHERE id = ?;`

	var e models.Event
	var allDay int
	row := s.db.QueryRowContext(ctx, query, id)
	if err := row.Scan(&e.ID, &e.Provider, &e.ProviderEventID, &e.ProviderUID, &e.CalendarID, &e.ETag, &e.Version, &e.Title, &e.Description, &e.Location,
		&e.StartAt, &e.EndAt, &e.Timezone, &allDay, &e.Status, &e.RecurrenceRule, &e.RecurrenceParent, &e.Organizer, &e.Source,
		&e.RemoteCreatedAt, &e.RemoteUpdatedAt, &e.LocalCreatedAt, &e.LocalUpdatedAt, &e.DeletedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get event: %w", err)
	}
	e.AllDay = (allDay == 1)
	return &e, nil
}

func (s *Store) GetEventByProviderID(ctx context.Context, calendarID, providerEventID string) (*models.Event, error) {
	query := `SELECT id, provider, provider_event_id, provider_uid, calendar_id, etag, version, title, description, location,
		start_at, end_at, timezone, all_day, status, recurrence_rule, recurrence_parent, organizer, source,
		remote_created_at, remote_updated_at, local_created_at, local_updated_at, deleted_at
		FROM events WHERE calendar_id = ? AND provider_event_id = ?;`

	var e models.Event
	var allDay int
	row := s.db.QueryRowContext(ctx, query, calendarID, providerEventID)
	if err := row.Scan(&e.ID, &e.Provider, &e.ProviderEventID, &e.ProviderUID, &e.CalendarID, &e.ETag, &e.Version, &e.Title, &e.Description, &e.Location,
		&e.StartAt, &e.EndAt, &e.Timezone, &allDay, &e.Status, &e.RecurrenceRule, &e.RecurrenceParent, &e.Organizer, &e.Source,
		&e.RemoteCreatedAt, &e.RemoteUpdatedAt, &e.LocalCreatedAt, &e.LocalUpdatedAt, &e.DeletedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get event by provider id: %w", err)
	}
	e.AllDay = (allDay == 1)
	return &e, nil
}

func (s *Store) UpsertEvent(ctx context.Context, e *models.Event) error {
	allDay := 0
	if e.AllDay {
		allDay = 1
	}

	query := `INSERT INTO events (id, provider, provider_event_id, provider_uid, calendar_id, etag, version, title, description, location,
		start_at, end_at, timezone, all_day, status, recurrence_rule, recurrence_parent, organizer, source,
		remote_created_at, remote_updated_at, local_created_at, local_updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider_event_id = excluded.provider_event_id,
			provider_uid = excluded.provider_uid,
			calendar_id = excluded.calendar_id,
			etag = excluded.etag,
			version = excluded.version,
			title = excluded.title,
			description = excluded.description,
			location = excluded.location,
			start_at = excluded.start_at,
			end_at = excluded.end_at,
			timezone = excluded.timezone,
			all_day = excluded.all_day,
			status = excluded.status,
			recurrence_rule = excluded.recurrence_rule,
			recurrence_parent = excluded.recurrence_parent,
			organizer = excluded.organizer,
			source = excluded.source,
			remote_created_at = excluded.remote_created_at,
			remote_updated_at = excluded.remote_updated_at,
			local_updated_at = excluded.local_updated_at,
			deleted_at = excluded.deleted_at;`

	_, err := s.db.ExecContext(ctx, query, e.ID, e.Provider, e.ProviderEventID, e.ProviderUID, e.CalendarID, e.ETag, e.Version, e.Title, e.Description, e.Location,
		e.StartAt, e.EndAt, e.Timezone, allDay, e.Status, e.RecurrenceRule, e.RecurrenceParent, e.Organizer, e.Source,
		e.RemoteCreatedAt, e.RemoteUpdatedAt, e.LocalCreatedAt, e.LocalUpdatedAt, e.DeletedAt)
	return err
}

func (s *Store) SoftDeleteEvent(ctx context.Context, id string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, "UPDATE events SET deleted_at = ?, local_updated_at = ?, status = 'cancelled' WHERE id = ?;", now, now, id)
	return err
}

func (s *Store) RestoreEvent(ctx context.Context, id string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, "UPDATE events SET deleted_at = NULL, local_updated_at = ?, status = 'confirmed' WHERE id = ?;", now, id)
	return err
}

func (s *Store) CountActiveEvents(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE deleted_at IS NULL;").Scan(&count)
	return count, err
}

// --- Mutation Queue ---

func (s *Store) EnqueueMutation(ctx context.Context, item *models.MutationQueueItem) error {
	query := `INSERT INTO mutation_queue (id, operation, provider, calendar_id, local_event_id, payload, idempotency_key, attempt_count, next_attempt_at, last_error, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(idempotency_key) DO UPDATE SET
			payload = excluded.payload,
			updated_at = excluded.updated_at;`
	now := time.Now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, query, item.ID, item.Operation, item.Provider, item.CalendarID, item.LocalEventID, item.Payload, item.IdempotencyKey,
		item.AttemptCount, item.NextAttemptAt, item.LastError, string(item.State), item.CreatedAt, item.UpdatedAt)
	return err
}

func (s *Store) GetPendingMutations(ctx context.Context, limit int) ([]models.MutationQueueItem, error) {
	query := `SELECT id, operation, provider, calendar_id, local_event_id, payload, idempotency_key, attempt_count, next_attempt_at, last_error, state, created_at, updated_at
		FROM mutation_queue
		WHERE state IN ('pending', 'retry') AND next_attempt_at <= ?
		ORDER BY created_at ASC LIMIT ?;`

	rows, err := s.db.QueryContext(ctx, query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("get pending mutations: %w", err)
	}
	defer rows.Close()

	var items []models.MutationQueueItem
	for rows.Next() {
		var item models.MutationQueueItem
		var stateStr string
		if err := rows.Scan(&item.ID, &item.Operation, &item.Provider, &item.CalendarID, &item.LocalEventID, &item.Payload, &item.IdempotencyKey,
			&item.AttemptCount, &item.NextAttemptAt, &item.LastError, &stateStr, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan mutation: %w", err)
		}
		item.State = models.QueueState(stateStr)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateMutationState(ctx context.Context, id string, state models.QueueState, attemptCount int, nextAttempt time.Time, lastError string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE mutation_queue SET state = ?, attempt_count = ?, next_attempt_at = ?, last_error = ?, updated_at = ? WHERE id = ?;",
		string(state), attemptCount, nextAttempt, lastError, time.Now(), id)
	return err
}

func (s *Store) CountPendingMutations(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mutation_queue WHERE state IN ('pending', 'retry', 'processing');").Scan(&count)
	return count, err
}

func (s *Store) ListAllMutations(ctx context.Context, limit, offset int) ([]models.MutationQueueItem, error) {
	query := `SELECT id, operation, provider, calendar_id, local_event_id, payload, idempotency_key, attempt_count, next_attempt_at, last_error, state, created_at, updated_at
		FROM mutation_queue
		ORDER BY created_at DESC LIMIT ? OFFSET ?;`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list all mutations: %w", err)
	}
	defer rows.Close()

	var items []models.MutationQueueItem
	for rows.Next() {
		var item models.MutationQueueItem
		var stateStr string
		if err := rows.Scan(&item.ID, &item.Operation, &item.Provider, &item.CalendarID, &item.LocalEventID, &item.Payload, &item.IdempotencyKey,
			&item.AttemptCount, &item.NextAttemptAt, &item.LastError, &stateStr, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan mutation: %w", err)
		}
		item.State = models.QueueState(stateStr)
		items = append(items, item)
	}
	return items, rows.Err()
}

// --- Mail Sources ---

func (s *Store) ListMailSources(ctx context.Context) ([]models.MailSource, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, person_id, provider, provider_account_id, enabled, filter_mode, filter_config, last_cursor, last_checked_at, status FROM mail_sources;")
	if err != nil {
		return nil, fmt.Errorf("list mail sources: %w", err)
	}
	defer rows.Close()

	var sources []models.MailSource
	for rows.Next() {
		var src models.MailSource
		var enabled int
		if err := rows.Scan(&src.ID, &src.PersonID, &src.Provider, &src.ProviderAccountID, &enabled, &src.FilterMode, &src.FilterConfig, &src.LastCursor, &src.LastCheckedAt, &src.Status); err != nil {
			return nil, fmt.Errorf("scan mail source: %w", err)
		}
		src.Enabled = (enabled == 1)
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *Store) UpsertMailSource(ctx context.Context, src *models.MailSource) error {
	en := 0
	if src.Enabled {
		en = 1
	}
	query := `INSERT INTO mail_sources (id, person_id, provider, provider_account_id, enabled, filter_mode, filter_config, last_cursor, last_checked_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			filter_mode = excluded.filter_mode,
			filter_config = excluded.filter_config,
			status = excluded.status;`
	_, err := s.db.ExecContext(ctx, query, src.ID, src.PersonID, src.Provider, src.ProviderAccountID, en, src.FilterMode, src.FilterConfig, src.LastCursor, src.LastCheckedAt, src.Status)
	return err
}

func (s *Store) UpdateMailSourceCursor(ctx context.Context, id, cursor string, checkedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE mail_sources SET last_cursor = ?, last_checked_at = ? WHERE id = ?;", cursor, checkedAt, id)
	return err
}

// --- Processed Messages ---

func (s *Store) IsMessageProcessed(ctx context.Context, provider, providerMessageID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM processed_messages WHERE provider = ? AND provider_message_id = ?;", provider, providerMessageID).Scan(&count)
	return count > 0, err
}

func (s *Store) RecordProcessedMessage(ctx context.Context, msg *models.ProcessedMessage) error {
	query := `INSERT INTO processed_messages (id, provider, provider_message_id, mail_source_id, received_at, content_hash, processing_state, parser_version, candidate_id, event_id, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, provider_message_id) DO UPDATE SET
			processing_state = excluded.processing_state,
			candidate_id = excluded.candidate_id,
			event_id = excluded.event_id,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at;`
	now := time.Now()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	msg.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, query, msg.ID, msg.Provider, msg.ProviderMessageID, msg.MailSourceID, msg.ReceivedAt, msg.ContentHash, msg.ProcessingState,
		msg.ParserVersion, msg.CandidateID, msg.EventID, msg.LastError, msg.CreatedAt, msg.UpdatedAt)
	return err
}

// --- Event Candidates ---

func (s *Store) CreateEventCandidate(ctx context.Context, c *models.EventCandidate) error {
	allDay := 0
	if c.AllDay {
		allDay = 1
	}
	query := `INSERT INTO event_candidates (id, source_provider, source_account, source_message_id, source_sender, source_subject, title, description, location,
		start_at, end_at, timezone, all_day, confidence, parser_method, ambiguity_reason, suggested_calendar_id, state, created_at, reviewed_at, reviewed_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`

	now := time.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}

	_, err := s.db.ExecContext(ctx, query, c.ID, c.SourceProvider, c.SourceAccount, c.SourceMessageID, c.SourceSender, c.SourceSubject, c.Title, c.Description, c.Location,
		c.StartAt, c.EndAt, c.Timezone, allDay, c.Confidence, c.ParserMethod, c.AmbiguityReason, c.SuggestedCalendarID, string(c.State), c.CreatedAt, c.ReviewedAt, c.ReviewedBy)
	return err
}

func (s *Store) ListCandidates(ctx context.Context, state models.CandidateState, limit, offset int) ([]models.EventCandidate, error) {
	var query string
	var args []interface{}
	if state != "" {
		query = `SELECT id, source_provider, source_account, source_message_id, source_sender, source_subject, title, description, location,
			start_at, end_at, timezone, all_day, confidence, parser_method, ambiguity_reason, suggested_calendar_id, state, created_at, reviewed_at, reviewed_by
			FROM event_candidates WHERE state = ? ORDER BY created_at DESC LIMIT ? OFFSET ?;`
		args = []interface{}{string(state), limit, offset}
	} else {
		query = `SELECT id, source_provider, source_account, source_message_id, source_sender, source_subject, title, description, location,
			start_at, end_at, timezone, all_day, confidence, parser_method, ambiguity_reason, suggested_calendar_id, state, created_at, reviewed_at, reviewed_by
			FROM event_candidates ORDER BY created_at DESC LIMIT ? OFFSET ?;`
		args = []interface{}{limit, offset}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()

	var list []models.EventCandidate
	for rows.Next() {
		var c models.EventCandidate
		var allDay int
		var stateStr string
		if err := rows.Scan(&c.ID, &c.SourceProvider, &c.SourceAccount, &c.SourceMessageID, &c.SourceSender, &c.SourceSubject, &c.Title, &c.Description, &c.Location,
			&c.StartAt, &c.EndAt, &c.Timezone, &allDay, &c.Confidence, &c.ParserMethod, &c.AmbiguityReason, &c.SuggestedCalendarID, &stateStr, &c.CreatedAt, &c.ReviewedAt, &c.ReviewedBy); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		c.AllDay = (allDay == 1)
		c.State = models.CandidateState(stateStr)
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) GetCandidate(ctx context.Context, id string) (*models.EventCandidate, error) {
	query := `SELECT id, source_provider, source_account, source_message_id, source_sender, source_subject, title, description, location,
		start_at, end_at, timezone, all_day, confidence, parser_method, ambiguity_reason, suggested_calendar_id, state, created_at, reviewed_at, reviewed_by
		FROM event_candidates WHERE id = ?;`

	var c models.EventCandidate
	var allDay int
	var stateStr string
	row := s.db.QueryRowContext(ctx, query, id)
	if err := row.Scan(&c.ID, &c.SourceProvider, &c.SourceAccount, &c.SourceMessageID, &c.SourceSender, &c.SourceSubject, &c.Title, &c.Description, &c.Location,
		&c.StartAt, &c.EndAt, &c.Timezone, &allDay, &c.Confidence, &c.ParserMethod, &c.AmbiguityReason, &c.SuggestedCalendarID, &stateStr, &c.CreatedAt, &c.ReviewedAt, &c.ReviewedBy); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get candidate: %w", err)
	}
	c.AllDay = (allDay == 1)
	c.State = models.CandidateState(stateStr)
	return &c, nil
}

func (s *Store) UpdateCandidateState(ctx context.Context, id string, state models.CandidateState, reviewedBy string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, "UPDATE event_candidates SET state = ?, reviewed_at = ?, reviewed_by = ? WHERE id = ?;",
		string(state), now, reviewedBy, id)
	return err
}

func (s *Store) CountPendingCandidates(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM event_candidates WHERE state = 'pending';").Scan(&count)
	return count, err
}

// --- Conflicts ---

func (s *Store) CreateConflict(ctx context.Context, c *models.Conflict) error {
	query := `INSERT INTO conflicts (id, local_event_id, remote_event_id, conflict_type, local_payload, remote_payload, state, resolved_at, resolution, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
	now := time.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, query, c.ID, c.LocalEventID, c.RemoteEventID, c.ConflictType, c.LocalPayload, c.RemotePayload, c.State, c.ResolvedAt, c.Resolution, c.CreatedAt)
	return err
}

func (s *Store) ListPendingConflicts(ctx context.Context) ([]models.Conflict, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, local_event_id, remote_event_id, conflict_type, local_payload, remote_payload, state, resolved_at, resolution, created_at
		FROM conflicts WHERE state = 'pending' ORDER BY created_at DESC;`)
	if err != nil {
		return nil, fmt.Errorf("list conflicts: %w", err)
	}
	defer rows.Close()

	var list []models.Conflict
	for rows.Next() {
		var c models.Conflict
		if err := rows.Scan(&c.ID, &c.LocalEventID, &c.RemoteEventID, &c.ConflictType, &c.LocalPayload, &c.RemotePayload, &c.State, &c.ResolvedAt, &c.Resolution, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conflict: %w", err)
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) ResolveConflict(ctx context.Context, id, resolutionState, resolutionDetails string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, "UPDATE conflicts SET state = ?, resolved_at = ?, resolution = ? WHERE id = ?;",
		resolutionState, now, resolutionDetails, id)
	return err
}

// --- Trusted Senders ---

func (s *Store) ListTrustedSenders(ctx context.Context) ([]models.TrustedSender, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, email, domain, description, enabled, created_at FROM trusted_senders WHERE enabled = 1 ORDER BY created_at DESC;")
	if err != nil {
		return nil, fmt.Errorf("list trusted senders: %w", err)
	}
	defer rows.Close()

	var list []models.TrustedSender
	for rows.Next() {
		var t models.TrustedSender
		var en int
		if err := rows.Scan(&t.ID, &t.Email, &t.Domain, &t.Description, &en, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan trusted sender: %w", err)
		}
		t.Enabled = (en == 1)
		list = append(list, t)
	}
	return list, rows.Err()
}

func (s *Store) AddTrustedSender(ctx context.Context, t *models.TrustedSender) error {
	en := 1
	if !t.Enabled {
		en = 0
	}
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO trusted_senders (id, email, domain, description, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?);",
		t.ID, t.Email, t.Domain, t.Description, en, t.CreatedAt)
	return err
}

func (s *Store) DeleteTrustedSender(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM trusted_senders WHERE id = ?;", id)
	return err
}

// --- Audit Log ---

func (s *Store) RecordAudit(ctx context.Context, entry *models.AuditEntry) error {
	query := `INSERT INTO audit_log (id, timestamp, actor, action, resource_type, resource_id, details, ip_address)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);`
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	_, err := s.db.ExecContext(ctx, query, entry.ID, entry.Timestamp, entry.Actor, entry.Action, entry.ResourceType, entry.ResourceID, entry.Details, entry.IPAddress)
	return err
}

func (s *Store) ListAuditLogs(ctx context.Context, limit, offset int) ([]models.AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, timestamp, actor, action, resource_type, resource_id, details, ip_address FROM audit_log ORDER BY timestamp DESC LIMIT ? OFFSET ?;", limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	defer rows.Close()

	var list []models.AuditEntry
	for rows.Next() {
		var a models.AuditEntry
		if err := rows.Scan(&a.ID, &a.Timestamp, &a.Actor, &a.Action, &a.ResourceType, &a.ResourceID, &a.Details, &a.IPAddress); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// --- Settings ---

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	row := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?;", key)
	if err := row.Scan(&val); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("get setting: %w", err)
	}
	return val, nil
}

func (s *Store) SetSetting(ctx context.Context, key, val string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;`, key, val, time.Now())
	return err
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *models.Session) error {
	query := `INSERT INTO sessions (token_hash, user_id, expires_at, created_at, last_active_at)
		VALUES (?, ?, ?, ?, ?);`
	_, err := s.db.ExecContext(ctx, query, sess.TokenHash, sess.UserID, sess.ExpiresAt, sess.CreatedAt, sess.LastActiveAt)
	return err
}

func (s *Store) GetSession(ctx context.Context, tokenHash string) (*models.Session, error) {
	var sess models.Session
	row := s.db.QueryRowContext(ctx, "SELECT token_hash, user_id, expires_at, created_at, last_active_at FROM sessions WHERE token_hash = ? AND expires_at > ?;", tokenHash, time.Now())
	if err := row.Scan(&sess.TokenHash, &sess.UserID, &sess.ExpiresAt, &sess.CreatedAt, &sess.LastActiveAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_active_at = ? WHERE token_hash = ?;", time.Now(), tokenHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?;", tokenHash)
	return err
}

func (s *Store) CleanExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?;", time.Now())
	return err
}

// --- Device Metadata ---

func (s *Store) GetDeviceMetadata(ctx context.Context) (*models.DeviceMetadata, error) {
	var m models.DeviceMetadata
	row := s.db.QueryRowContext(ctx, "SELECT device_name, timezone, initialized_at, version, kiosk_settings FROM device_metadata LIMIT 1;")
	if err := row.Scan(&m.DeviceName, &m.Timezone, &m.InitializedAt, &m.Version, &m.KioskSettings); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get device metadata: %w", err)
	}
	return &m, nil
}

func (s *Store) SetDeviceMetadata(ctx context.Context, m *models.DeviceMetadata) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, "DELETE FROM device_metadata;")
		_, err := tx.ExecContext(ctx, `INSERT INTO device_metadata (device_name, timezone, initialized_at, version, kiosk_settings)
			VALUES (?, ?, ?, ?, ?);`, m.DeviceName, m.Timezone, m.InitializedAt, m.Version, m.KioskSettings)
		return err
	})
}

// --- Quick Templates ---

func (s *Store) ListQuickTemplates(ctx context.Context) ([]models.QuickTemplate, error) {
	query := `SELECT id, title, COALESCE(calendar_id, ''), default_duration_minutes, icon, category, location, all_day, display_order, created_at
		FROM quick_templates ORDER BY display_order ASC, title ASC;`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list quick templates: %w", err)
	}
	defer rows.Close()

	var list []models.QuickTemplate
	for rows.Next() {
		var t models.QuickTemplate
		var allDayInt int
		if err := rows.Scan(&t.ID, &t.Title, &t.CalendarID, &t.DefaultDurationMinutes, &t.Icon, &t.Category, &t.Location, &allDayInt, &t.DisplayOrder, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan quick template: %w", err)
		}
		t.AllDay = (allDayInt == 1)
		list = append(list, t)
	}
	return list, rows.Err()
}

func (s *Store) GetQuickTemplate(ctx context.Context, id string) (*models.QuickTemplate, error) {
	query := `SELECT id, title, COALESCE(calendar_id, ''), default_duration_minutes, icon, category, location, all_day, display_order, created_at
		FROM quick_templates WHERE id = ?;`
	row := s.db.QueryRowContext(ctx, query, id)
	var t models.QuickTemplate
	var allDayInt int
	if err := row.Scan(&t.ID, &t.Title, &t.CalendarID, &t.DefaultDurationMinutes, &t.Icon, &t.Category, &t.Location, &allDayInt, &t.DisplayOrder, &t.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get quick template: %w", err)
	}
	t.AllDay = (allDayInt == 1)
	return &t, nil
}

func (s *Store) UpsertQuickTemplate(ctx context.Context, t *models.QuickTemplate) error {
	query := `INSERT INTO quick_templates (id, title, calendar_id, default_duration_minutes, icon, category, location, all_day, display_order, created_at)
		VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			calendar_id = excluded.calendar_id,
			default_duration_minutes = excluded.default_duration_minutes,
			icon = excluded.icon,
			category = excluded.category,
			location = excluded.location,
			all_day = excluded.all_day,
			display_order = excluded.display_order;`
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	allDay := 0
	if t.AllDay {
		allDay = 1
	}
	_, err := s.db.ExecContext(ctx, query, t.ID, t.Title, t.CalendarID, t.DefaultDurationMinutes, t.Icon, t.Category, t.Location, allDay, t.DisplayOrder, t.CreatedAt)
	return err
}

func (s *Store) DeleteQuickTemplate(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM quick_templates WHERE id = ?;", id)
	return err
}
