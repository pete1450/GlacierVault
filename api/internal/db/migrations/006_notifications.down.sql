-- Roll back notification settings. The warmup_notified_at column is left
-- in place (older SQLite has no DROP COLUMN); it is ignored without the
-- notify package.
DROP TABLE IF EXISTS notification_config;
