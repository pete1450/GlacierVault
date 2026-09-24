-- The retention label was stored and displayed but never consumed by any
-- backup, prune, or restore path. Remove the dead column entirely.
ALTER TABLE backup_definitions DROP COLUMN retention_label;
