-- Roll back the snapshot-delete grant flag. The column is left in place
-- (older SQLite has no DROP COLUMN); it is ignored without the IAM code.
SELECT 1;
