-- 004_retention_and_indexes.sql: Optimized compound query indexes and privacy settings

CREATE INDEX IF NOT EXISTS idx_events_range_composite ON events (deleted_at, start_at, end_at, calendar_id);

INSERT INTO settings ("key", "value", "updated_at")
SELECT 'kiosk_privacy_mode', 'full', CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM settings WHERE "key" = 'kiosk_privacy_mode');
