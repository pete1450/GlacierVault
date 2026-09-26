CREATE TABLE IF NOT EXISTS restore_config (
    id                 INTEGER PRIMARY KEY CHECK (id = 1),
    download_gb_per_day INTEGER NOT NULL DEFAULT 1080
);
INSERT OR IGNORE INTO restore_config (id) VALUES (1);
