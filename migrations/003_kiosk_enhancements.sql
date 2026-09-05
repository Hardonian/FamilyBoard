-- 003_kiosk_enhancements.sql: Quick-add templates and kiosk settings

CREATE TABLE IF NOT EXISTS quick_templates (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    calendar_id TEXT REFERENCES calendars(id) ON DELETE SET NULL,
    default_duration_minutes INTEGER NOT NULL DEFAULT 60,
    icon TEXT NOT NULL DEFAULT '📅',
    category TEXT NOT NULL DEFAULT 'general',
    location TEXT NOT NULL DEFAULT '',
    all_day INTEGER NOT NULL DEFAULT 0,
    display_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL
);

-- Seed initial household quick templates
INSERT OR IGNORE INTO quick_templates (id, title, calendar_id, default_duration_minutes, icon, category, location, all_day, display_order, created_at) VALUES
('tmpl-soccer', 'Soccer Practice', NULL, 60, '⚽', 'sports', 'Sports Complex', 0, 1, CURRENT_TIMESTAMP),
('tmpl-swimming', 'Swimming', NULL, 60, '🏊', 'sports', 'Community Pool', 0, 2, CURRENT_TIMESTAMP),
('tmpl-school', 'School Event', NULL, 60, '🏫', 'school', '', 0, 3, CURRENT_TIMESTAMP),
('tmpl-dentist', 'Dentist', NULL, 45, '🦷', 'health', 'Dental Clinic', 0, 4, CURRENT_TIMESTAMP),
('tmpl-doctor', 'Doctor Appointment', NULL, 45, '🩺', 'health', 'Medical Centre', 0, 5, CURRENT_TIMESTAMP),
('tmpl-birthday', 'Birthday', NULL, 0, '🎂', 'celebration', '', 1, 6, CURRENT_TIMESTAMP),
('tmpl-dinner', 'Family Dinner', NULL, 90, '🍽️', 'family', '', 0, 7, CURRENT_TIMESTAMP),
('tmpl-grocery', 'Grocery Shopping', NULL, 60, '🛒', 'errands', '', 0, 8, CURRENT_TIMESTAMP),
('tmpl-work', 'Work Focus', NULL, 60, '💼', 'work', '', 0, 9, CURRENT_TIMESTAMP);

-- Seed initial kiosk settings
INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES
('kiosk_default_view', 'hybrid', CURRENT_TIMESTAMP),
('kiosk_time_format', '12h', CURRENT_TIMESTAMP),
('kiosk_week_start', '0', CURRENT_TIMESTAMP),
('kiosk_birthday_highlight', 'true', CURRENT_TIMESTAMP),
('kiosk_recent_suggestions', 'true', CURRENT_TIMESTAMP),
('kiosk_weather_city', 'Toronto', CURRENT_TIMESTAMP);
