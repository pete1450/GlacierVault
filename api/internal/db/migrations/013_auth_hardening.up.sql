-- Auth hardening: rate limiting, TOTP 2FA, API tokens, session invalidation.

-- Failed login attempts for rate limiting.
CREATE TABLE IF NOT EXISTS login_attempts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    ip           TEXT NOT NULL,
    attempted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    success      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_login_attempts_ip_time ON login_attempts(ip, attempted_at);

-- API tokens for automation.
CREATE TABLE IF NOT EXISTS api_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME,
    expires_at  DATETIME,
    revoked     INTEGER NOT NULL DEFAULT 0
);

-- TOTP 2FA and session epoch on app_config.
ALTER TABLE app_config ADD COLUMN totp_secret TEXT DEFAULT '';
ALTER TABLE app_config ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE app_config ADD COLUMN totp_backup_codes TEXT DEFAULT '[]';
ALTER TABLE app_config ADD COLUMN token_epoch INTEGER NOT NULL DEFAULT 0;
