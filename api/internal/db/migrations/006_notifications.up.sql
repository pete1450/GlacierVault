-- Apprise notification destinations and per-event switches.
-- Destinations are stored newline-separated; the switches gate which
-- events dispatch through the apprise CLI.
CREATE TABLE IF NOT EXISTS notification_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    destinations TEXT NOT NULL DEFAULT '',
    notify_backup_completed INTEGER NOT NULL DEFAULT 0,
    notify_warmup_completed INTEGER NOT NULL DEFAULT 0,
    notify_restore_completed INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO notification_config (id) VALUES (1);

-- Dedupe guard for the warmup-complete notification: the happy-path
-- watcher and the restart-resume path can both observe the S3 Batch job
-- completing, but the user should get exactly one notification per job.
ALTER TABLE restore_jobs ADD COLUMN warmup_notified_at TIMESTAMP;
