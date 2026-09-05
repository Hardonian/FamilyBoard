-- 001_initial_schema.sql: Initial schema for FamilyBoard

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS people (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Seed initial Scott and Michelle family accounts
INSERT OR IGNORE INTO people (id, display_name, email, enabled, created_at, updated_at) VALUES
('person-scott', 'Scott', 'scottrmhardie@gmail.com', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('person-michelle', 'Michelle', 'michelleyounghardie@gmail.com', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

CREATE TABLE IF NOT EXISTS oauth_accounts (
    id TEXT PRIMARY KEY,
    person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_account_id TEXT NOT NULL,
    verified_email TEXT NOT NULL,
    scopes TEXT NOT NULL,
    credential_reference TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(provider, provider_account_id),
    UNIQUE(person_id, provider)
);

CREATE TABLE IF NOT EXISTS calendars (
    id TEXT PRIMARY KEY,
    oauth_account_id TEXT NOT NULL REFERENCES oauth_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_calendar_id TEXT NOT NULL,
    name TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'America/Toronto',
    display_color TEXT NOT NULL DEFAULT '#3b82f6',
    enabled_for_display INTEGER NOT NULL DEFAULT 1,
    writable INTEGER NOT NULL DEFAULT 1,
    default_write_target INTEGER NOT NULL DEFAULT 0,
    sync_cursor TEXT NOT NULL DEFAULT '',
    last_synced_at DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(provider, provider_calendar_id)
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_event_id TEXT NOT NULL DEFAULT '',
    provider_uid TEXT NOT NULL DEFAULT '',
    calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    etag TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    start_at DATETIME NOT NULL,
    end_at DATETIME NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'America/Toronto',
    all_day INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'confirmed',
    recurrence_rule TEXT NOT NULL DEFAULT '',
    recurrence_parent TEXT NOT NULL DEFAULT '',
    organizer TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'remote_sync',
    remote_created_at DATETIME,
    remote_updated_at DATETIME,
    local_created_at DATETIME NOT NULL,
    local_updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);

CREATE TABLE IF NOT EXISTS mail_sources (
    id TEXT PRIMARY KEY,
    person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_account_id TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    filter_mode TEXT NOT NULL DEFAULT 'label',
    filter_config TEXT NOT NULL DEFAULT 'FamilyBoard',
    last_cursor TEXT NOT NULL DEFAULT '',
    last_checked_at DATETIME,
    status TEXT NOT NULL DEFAULT 'active'
);

CREATE TABLE IF NOT EXISTS event_candidates (
    id TEXT PRIMARY KEY,
    source_provider TEXT NOT NULL,
    source_account TEXT NOT NULL,
    source_message_id TEXT NOT NULL,
    source_sender TEXT NOT NULL,
    source_subject TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    start_at DATETIME NOT NULL,
    end_at DATETIME NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'America/Toronto',
    all_day INTEGER NOT NULL DEFAULT 0,
    confidence REAL NOT NULL DEFAULT 0.0,
    parser_method TEXT NOT NULL,
    ambiguity_reason TEXT NOT NULL DEFAULT '',
    suggested_calendar_id TEXT,
    state TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL,
    reviewed_at DATETIME,
    reviewed_by TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS processed_messages (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_message_id TEXT NOT NULL,
    mail_source_id TEXT REFERENCES mail_sources(id) ON DELETE SET NULL,
    received_at DATETIME NOT NULL,
    content_hash TEXT NOT NULL,
    processing_state TEXT NOT NULL DEFAULT 'processed',
    parser_version TEXT NOT NULL DEFAULT '1.0',
    candidate_id TEXT REFERENCES event_candidates(id) ON DELETE SET NULL,
    event_id TEXT REFERENCES events(id) ON DELETE SET NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(provider, provider_message_id)
);

CREATE TABLE IF NOT EXISTS mutation_queue (
    id TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    provider TEXT NOT NULL,
    calendar_id TEXT NOT NULL,
    local_event_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS conflicts (
    id TEXT PRIMARY KEY,
    local_event_id TEXT NOT NULL,
    remote_event_id TEXT NOT NULL,
    conflict_type TEXT NOT NULL,
    local_payload TEXT NOT NULL,
    remote_payload TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending',
    resolved_at DATETIME,
    resolution TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS trusted_senders (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL DEFAULT '',
    domain TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_log (
    id TEXT PRIMARY KEY,
    timestamp DATETIME NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    details TEXT NOT NULL,
    ip_address TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL,
    last_active_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS device_metadata (
    device_name TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'America/Toronto',
    initialized_at DATETIME NOT NULL,
    version TEXT NOT NULL,
    kiosk_settings TEXT NOT NULL DEFAULT '{}'
);
