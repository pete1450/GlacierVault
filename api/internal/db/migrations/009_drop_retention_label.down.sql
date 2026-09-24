-- Restore the retention label column. The handlers no longer read or write
-- it, so this only matters for external tooling that queries the column.
ALTER TABLE backup_definitions ADD COLUMN retention_label TEXT NOT NULL DEFAULT 'archive';
