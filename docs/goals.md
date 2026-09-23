# Goals

## Mission

Make AWS Glacier Deep Archive — the cheapest durable cloud storage on earth —
usable by people who are not AWS experts. You bring credentials; GlacierVault
handles the rest.

## Goals

1. **Cheapest possible long-term storage.** Every design decision is weighed
   against dollars per terabyte per year. Deep Archive at ~$0.00099/GB-month
   is the floor of the AWS price list, and the app is built to stay on it.

2. **Request costs matter as much as storage costs.** Deep Archive charges
   $0.05 per 1,000 PUTs and bills ~40 KB of overhead per object. Millions of
   small objects would cost more in requests and overhead than in bytes, so
   GlacierVault packs data into 512 MiB objects to keep the object count —
   and therefore the request bill — tiny.

3. **Cheapest possible restores.** Restores default to the Bulk retrieval tier
   ($0.0025/GB, up to 48 h) instead of Standard ($0.02/GB, ~12 h). Bulk is 8×
   cheaper; for data you haven't needed in months, waiting is the right trade.

4. **Instant browsing, no retrieval required.** You should be able to see every
   snapshot, browse every file, and check how much you're storing *without*
   paying Glacier to thaw anything. A local metadata catalog plus a hot
   (S3 Standard) metadata bucket make the UI fast and free to browse.

5. **One-click infrastructure.** The setup wizard deploys the whole AWS stack
   (buckets, queue, IAM user, batch role) with CDK, then initializes the
   repository. No CloudFormation templates to hand-edit, no IAM policies to
   compose from scratch.

6. **Encryption by default.** Repository contents are encrypted with a
   randomly generated repo password. The app never stores your data in the
   clear, and the recovery package lets you get it back without the appliance.

7. **Snapshot management should be easy.** Snapshots are the unit of "what do
   I have and when." Listing, browsing, deleting, and pruning them must be
   obvious operations in the UI — not CLI archaeology.

8. **Stored-data usage must be visible.** A storage summary (total bytes, pack
   bytes, snapshot count, logical size) is always one glance away, because you
   can't manage costs you can't see.

9. **Self-hosted, single container.** One Docker image, one SQLite database,
   volumes for config/data/cache/logs. No managed services to babysit, no
   SaaS subscription, no data leaving your control except to your own buckets.

10. **Recoverable without the appliance.** If the container, the VM, or the
    whole homelab dies, the downloadable recovery package plus the documented
    manual restore procedure must be enough to get your data back with stock
    `rustic` and AWS CLI tools.

## Non-goals

- **Not for hot data.** If you access it monthly, use S3 Standard or Standard-IA.
  Deep Archive has a 180-day minimum storage duration — deleting early still
  bills you for 180 days.
- **Not instant restores.** Bulk retrieval takes up to 48 hours by design.
  This is a vault, not a file server.
- **Not a sync tool.** There is no two-way sync, no file versioning UI beyond
  snapshots, no sharing links.
- **Not a compliance appliance.** There is no WORM/object-lock mode, no audit
  log export, no legal-hold workflow. (Deep Archive itself is 11-nines
  durable, but GlacierVault adds no compliance layer.)
- **Not multi-tenant.** One admin login, one AWS account, one repository.
- **Not a replacement for the 3-2-1 rule.** Until you have personally
  completed a test restore, keep a second copy of anything irreplaceable.

## How success is measured

- A non-AWS-expert can go from zero to first backup in under 30 minutes.
- Storing 1 TB costs on the order of **$1/month** all-in.
- Restoring 1 TB with Bulk costs on the order of **$3 in AWS fees**
  (plus egress — see the [Cost Guide](costs.md)).
- Browsing snapshots and planning a restore never triggers a retrieval charge.
- A full restore can be completed from the recovery package alone, following
  only the written docs.
