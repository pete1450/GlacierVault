-- CloudFront free-egress restore path.
-- The private signing key is stored encrypted (same master key as the other
-- aws_config secrets); it never leaves the container except inside
-- short-lived signed URLs minted by the localhost proxy.
ALTER TABLE aws_config ADD COLUMN cf_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE aws_config ADD COLUMN cf_distribution_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN cf_domain TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN cf_key_pair_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN cf_key_group_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN cf_oac_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN cf_cache_policy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN cf_private_key_enc TEXT NOT NULL DEFAULT '';
ALTER TABLE aws_config ADD COLUMN cf_provisioned_at TEXT NOT NULL DEFAULT '';
