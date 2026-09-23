-- Durable handle for in-progress Glacier warmups.
-- The S3 Batch restore job ID submitted by warmup-s3-archives is recorded
-- here as soon as the tool reports it. On container restart, startup
-- reconciliation re-attaches to the stored job via s3:DescribeJob instead
-- of submitting a duplicate Batch job and waiting another 48h.
ALTER TABLE restore_jobs ADD COLUMN batch_job_id TEXT;
