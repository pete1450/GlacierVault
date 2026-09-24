-- Deleting a snapshot failed with FOREIGN KEY constraint failed whenever a
-- restore job referenced it: restore_jobs.snapshot_id was NOT NULL with a
-- plain REFERENCES (NO ACTION). Rebuild the table so the link is nullable
-- with ON DELETE SET NULL: deleting a snapshot now preserves restore
-- history and just clears the link, mirroring how backup-definition
-- deletion nulls its references instead of destroying history.
ALTER TABLE restore_jobs RENAME TO restore_jobs_legacy;

CREATE TABLE restore_jobs (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id          INTEGER REFERENCES snapshots(id) ON DELETE SET NULL,
    requested_paths      TEXT NOT NULL,
    destination          TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'queued',
    warmup_status        TEXT,
    retrieval_started_at DATETIME,
    restore_started_at   DATETIME,
    completed_at         DATETIME,
    error_message        TEXT,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    batch_job_id         TEXT,
    warmup_notified_at   TIMESTAMP
);

INSERT INTO restore_jobs (id, snapshot_id, requested_paths, destination, status, warmup_status, retrieval_started_at, restore_started_at, completed_at, error_message, created_at, batch_job_id, warmup_notified_at)
    SELECT id, snapshot_id, requested_paths, destination, status, warmup_status, retrieval_started_at, restore_started_at, completed_at, error_message, created_at, batch_job_id, warmup_notified_at
    FROM restore_jobs_legacy;

DROP TABLE restore_jobs_legacy;

CREATE INDEX IF NOT EXISTS idx_restore_jobs_status ON restore_jobs(status);
