CREATE TABLE IF NOT EXISTS app_config (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    master_key_path TEXT NOT NULL DEFAULT '/config/master.key',
    setup_complete INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS aws_config (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    region               TEXT NOT NULL,
    encrypted_access_key TEXT NOT NULL,
    encrypted_secret_key TEXT NOT NULL,
    stack_name           TEXT NOT NULL DEFAULT 'rustic-cold-backups',
    hot_bucket           TEXT,
    cold_bucket          TEXT,
    sqs_url              TEXT,
    iam_user             TEXT,
    batch_role_arn       TEXT,
    deployed_at          DATETIME
);

CREATE TABLE IF NOT EXISTS backup_definitions (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL UNIQUE,
    source_paths      TEXT NOT NULL,  -- JSON array
    schedule          TEXT NOT NULL,  -- cron expression
    retention_label   TEXT NOT NULL DEFAULT 'archive',
    compression_level INTEGER NOT NULL DEFAULT 3,
    encrypted_password TEXT NOT NULL,
    enabled           INTEGER NOT NULL DEFAULT 1,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS snapshots (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id   TEXT NOT NULL UNIQUE,  -- rustic snapshot ID
    backup_def_id INTEGER REFERENCES backup_definitions(id),
    hostname      TEXT NOT NULL,
    tags          TEXT,            -- JSON array
    total_size    INTEGER NOT NULL DEFAULT 0,
    file_count    INTEGER NOT NULL DEFAULT 0,
    backup_time   DATETIME NOT NULL,
    synced_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_snapshots_backup_def ON snapshots(backup_def_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_backup_time ON snapshots(backup_time DESC);

CREATE TABLE IF NOT EXISTS file_index (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    size        INTEGER NOT NULL DEFAULT 0,
    mtime       DATETIME,
    is_dir      INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_file_index_snapshot ON file_index(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_file_index_path ON file_index(path);

CREATE TABLE IF NOT EXISTS backup_jobs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    backup_def_id  INTEGER REFERENCES backup_definitions(id),
    started_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at   DATETIME,
    status         TEXT NOT NULL DEFAULT 'running', -- running, completed, failed
    bytes_transferred INTEGER NOT NULL DEFAULT 0,
    error_message  TEXT,
    log_output     TEXT
);

CREATE INDEX IF NOT EXISTS idx_backup_jobs_status ON backup_jobs(status);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_started ON backup_jobs(started_at DESC);

CREATE TABLE IF NOT EXISTS restore_jobs (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id          INTEGER NOT NULL REFERENCES snapshots(id),
    requested_paths      TEXT NOT NULL, -- JSON array; empty = full snapshot
    destination          TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'queued', -- queued, warmup_requested, retrieval_in_progress, retrieval_complete, restoring, completed, failed
    warmup_status        TEXT,
    retrieval_started_at DATETIME,
    restore_started_at   DATETIME,
    completed_at         DATETIME,
    error_message        TEXT,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_restore_jobs_status ON restore_jobs(status);
