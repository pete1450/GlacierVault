DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS login_attempts;
-- Note: SQLite does not support DROP COLUMN in older versions; the
-- totp_* and token_epoch columns on app_config are left in place.
