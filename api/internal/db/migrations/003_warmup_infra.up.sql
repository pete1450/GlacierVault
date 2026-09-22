ALTER TABLE aws_config ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN batch_manifests_bucket TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN batch_reports_bucket TEXT NOT NULL DEFAULT '';
