package models

import (
	"time"
)

// Person represents a family member identity.
type Person struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// OAuthAccount links a person to an external provider account.
type OAuthAccount struct {
	ID                  string    `json:"id"`
	PersonID            string    `json:"person_id"`
	Provider            string    `json:"provider"` // "google", "microsoft"
	ProviderAccountID   string    `json:"provider_account_id"`
	VerifiedEmail       string    `json:"verified_email"`
	Scopes              string    `json:"scopes"`
	CredentialReference string    `json:"credential_reference"`
	ExpiresAt           time.Time `json:"expires_at"`
	Status              string    `json:"status"` // "active", "expired", "revoked", "error"
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Calendar represents a synced calendar target.
type Calendar struct {
	ID                 string     `json:"id"`
	OAuthAccountID     string     `json:"oauth_account_id"`
	Provider           string     `json:"provider"`
	ProviderCalendarID string     `json:"provider_calendar_id"`
	Name               string     `json:"name"`
	Timezone           string     `json:"timezone"`
	DisplayColor       string     `json:"display_color"`
	EnabledForDisplay  bool       `json:"enabled_for_display"`
	Writable           bool       `json:"writable"`
	DefaultWriteTarget bool       `json:"default_write_target"`
	SyncCursor         string     `json:"sync_cursor"`
	LastSyncedAt       *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// Event represents a calendar event stored locally in SQLite.
type Event struct {
	ID               string     `json:"id"`
	Provider         string     `json:"provider"`
	ProviderEventID  string     `json:"provider_event_id"`
	ProviderUID      string     `json:"provider_uid"`
	CalendarID       string     `json:"calendar_id"`
	ETag             string     `json:"etag"`
	Version          int64      `json:"version"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Location         string     `json:"location"`
	StartAt          time.Time  `json:"start_at"`
	EndAt            time.Time  `json:"end_at"`
	Timezone         string     `json:"timezone"`
	AllDay           bool       `json:"all_day"`
	Status           string     `json:"status"` // "confirmed", "tentative", "cancelled"
	RecurrenceRule   string     `json:"recurrence_rule,omitempty"`
	RecurrenceParent string     `json:"recurrence_parent,omitempty"`
	Organizer        string     `json:"organizer,omitempty"`
	Source           string     `json:"source"` // "remote_sync", "local_ui", "email_candidate"
	RemoteCreatedAt  *time.Time `json:"remote_created_at,omitempty"`
	RemoteUpdatedAt  *time.Time `json:"remote_updated_at,omitempty"`
	LocalCreatedAt   time.Time  `json:"local_created_at"`
	LocalUpdatedAt   time.Time  `json:"local_updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

// MailSource represents an email intake configuration.
type MailSource struct {
	ID                string     `json:"id"`
	PersonID          string     `json:"person_id"`
	Provider          string     `json:"provider"` // "gmail", "microsoft"
	ProviderAccountID string     `json:"provider_account_id"`
	Enabled           bool       `json:"enabled"`
	FilterMode        string     `json:"filter_mode"` // "label", "sender", "subject", "combined"
	FilterConfig      string     `json:"filter_config"`
	LastCursor        string     `json:"last_cursor"`
	LastCheckedAt     *time.Time `json:"last_checked_at,omitempty"`
	Status            string     `json:"status"`
}

// ProcessedMessage prevents duplicate processing of email messages.
type ProcessedMessage struct {
	ID                string    `json:"id"`
	Provider          string    `json:"provider"`
	ProviderMessageID string    `json:"provider_message_id"`
	MailSourceID      string    `json:"mail_source_id"`
	ReceivedAt        time.Time `json:"received_at"`
	ContentHash       string    `json:"content_hash"`
	ProcessingState   string    `json:"processing_state"` // "processed", "ignored", "error"
	ParserVersion     string    `json:"parser_version"`
	CandidateID       *string   `json:"candidate_id,omitempty"`
	EventID           *string   `json:"event_id,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CandidateState defines approval lifecycle states for event candidates.
type CandidateState string

const (
	CandidatePending      CandidateState = "pending"
	CandidateApproved     CandidateState = "approved"
	CandidateRejected     CandidateState = "rejected"
	CandidateAutoApproved CandidateState = "auto_approved"
	CandidateDuplicate    CandidateState = "duplicate"
	CandidateError        CandidateState = "error"
)

// EventCandidate represents an event extracted from email pending or auto-approved.
type EventCandidate struct {
	ID                  string         `json:"id"`
	SourceProvider      string         `json:"source_provider"`
	SourceAccount       string         `json:"source_account"`
	SourceMessageID     string         `json:"source_message_id"`
	SourceSender        string         `json:"source_sender"`
	SourceSubject       string         `json:"source_subject"`
	Title               string         `json:"title"`
	Description         string         `json:"description"`
	Location            string         `json:"location"`
	StartAt             time.Time      `json:"start_at"`
	EndAt               time.Time      `json:"end_at"`
	Timezone            string         `json:"timezone"`
	AllDay              bool           `json:"all_day"`
	Confidence          float64        `json:"confidence"`
	ParserMethod        string         `json:"parser_method"` // "ics", "deterministic_nl", "lan_llm"
	AmbiguityReason     string         `json:"ambiguity_reason,omitempty"`
	SuggestedCalendarID string         `json:"suggested_calendar_id,omitempty"`
	State               CandidateState `json:"state"`
	CreatedAt           time.Time      `json:"created_at"`
	ReviewedAt          *time.Time     `json:"reviewed_at,omitempty"`
	ReviewedBy          string         `json:"reviewed_by,omitempty"`
}

// QueueState defines the execution state of an outbound mutation.
type QueueState string

const (
	QueuePending    QueueState = "pending"
	QueueProcessing QueueState = "processing"
	QueueRetry      QueueState = "retry"
	QueueCompleted  QueueState = "completed"
	QueueDeadLetter QueueState = "dead_letter"
)

// MutationQueueItem represents a durable local calendar mutation queued for remote sync.
type MutationQueueItem struct {
	ID             string     `json:"id"`
	Operation      string     `json:"operation"` // "create", "update", "delete"
	Provider       string     `json:"provider"`
	CalendarID     string     `json:"calendar_id"`
	LocalEventID   string     `json:"local_event_id"`
	Payload        string     `json:"payload"`
	IdempotencyKey string     `json:"idempotency_key"`
	AttemptCount   int        `json:"attempt_count"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	LastError      string     `json:"last_error,omitempty"`
	State          QueueState `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Conflict represents a detected collision between local mutations and remote updates.
type Conflict struct {
	ID            string     `json:"id"`
	LocalEventID  string     `json:"local_event_id"`
	RemoteEventID string     `json:"remote_event_id"`
	ConflictType  string     `json:"conflict_type"` // "concurrent_edit", "remote_deleted", "etag_mismatch"
	LocalPayload  string     `json:"local_payload"`
	RemotePayload string     `json:"remote_payload"`
	State         string     `json:"state"` // "pending", "resolved_local", "resolved_remote", "dismissed"
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	Resolution    string     `json:"resolution,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// AuditEntry tracks security, configuration, and data-modifying actions.
type AuditEntry struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Actor        string    `json:"actor"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Details      string    `json:"details"`
	IPAddress    string    `json:"ip_address"`
}

// Setting stores key-value application settings.
type Setting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Session represents an authenticated admin session.
type Session struct {
	TokenHash    string    `json:"token_hash"`
	UserID       string    `json:"user_id"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}

// DeviceMetadata stores appliance identification and state.
type DeviceMetadata struct {
	DeviceName    string    `json:"device_name"`
	Timezone      string    `json:"timezone"`
	InitializedAt time.Time `json:"initialized_at"`
	Version       string    `json:"version"`
	KioskSettings string    `json:"kiosk_settings"` // JSON serialized settings
}

// TrustedSender holds allowlist definitions for auto-approval policies.
type TrustedSender struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Domain      string    `json:"domain"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}
