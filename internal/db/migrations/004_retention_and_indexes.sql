-- 004_retention_and_indexes.sql: Optimized compound query indexes and privacy settings

CREATE INDEX IF NOT EXISTS idx_events_range_composite ON events (deleted_at, start_at, end_at, calendar_id);

INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES
('kiosk_privacy_mode', 'full', CURRENT_TIMESTAMP);
