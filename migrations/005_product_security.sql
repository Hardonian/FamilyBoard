CREATE TABLE IF NOT EXISTS pairing_codes (
    token_hash TEXT PRIMARY KEY,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS paired_devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    revoked_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_pairing_codes_expires ON pairing_codes (expires_at);
CREATE INDEX IF NOT EXISTS idx_paired_devices_active ON paired_devices (revoked_at, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS privacy_exports (
    id TEXT PRIMARY KEY,
    requested_at DATETIME NOT NULL,
    completed_at DATETIME,
    state TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT ''
);
