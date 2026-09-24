-- Roll back the restore_jobs rebuild. The nullable snapshot_id with
-- ON DELETE SET NULL is left in place: restoring NOT NULL could fail if any
-- links were already cleared, and the relaxed form is strictly more
-- permissive. Ignored without the updated handlers.
SELECT 1;
