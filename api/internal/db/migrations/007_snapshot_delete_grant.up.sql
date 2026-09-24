-- Tracks whether GlacierVault granted the backup (rustic) IAM user the
-- narrow s3:DeleteObject inline policy (GlacierVaultSnapshotDelete).
-- 1 = granted (snapshot delete/prune works), 0 = not granted (append-only).
-- This records what GlacierVault last did; it is not a live IAM query
-- (the admin credentials used to change it are never retained).
ALTER TABLE aws_config ADD COLUMN snapshot_delete_granted INTEGER NOT NULL DEFAULT 0;
