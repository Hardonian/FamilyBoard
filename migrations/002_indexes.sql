-- 002_indexes.sql: Performance indexes for FamilyBoard SQLite database

CREATE INDEX IF NOT EXISTS idx_events_calendar_range ON events (calendar_id, start_at, end_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_start_at ON events (start_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_provider_lookup ON events (provider, provider_event_id);
CREATE INDEX IF NOT EXISTS idx_events_provider_uid ON events (provider_uid);

CREATE INDEX IF NOT EXISTS idx_mutation_queue_state ON mutation_queue (state, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_mutation_queue_idempotency ON mutation_queue (idempotency_key);

CREATE INDEX IF NOT EXISTS idx_candidates_state ON event_candidates (state, created_at);
CREATE INDEX IF NOT EXISTS idx_candidates_message ON event_candidates (source_message_id);

CREATE INDEX IF NOT EXISTS idx_processed_messages_hash ON processed_messages (content_hash);
CREATE INDEX IF NOT EXISTS idx_processed_messages_provider ON processed_messages (provider, provider_message_id);

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);
