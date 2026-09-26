# Design

## Theory

### Why Glacier Deep Archive

Of everything AWS sells, Glacier Deep Archive is the cheapest place to put a
byte and still get 11-nines durability: **$0.00099/GB-month**, roughly $1/TB
per month. The catch is retrieval: 12–48 hours and per-request fees that
punish small objects. GlacierVault's whole design is shaped by that
trade-off — accept slow retrieval, then ruthlessly minimize everything else.

### Content-defined chunking + deduplication

Backups are performed by [Rustic](https://github.com/rustic-rs/rustic), a
restic-compatible backup tool. Files are split into content-defined chunks,
each chunk is encrypted and hashed, and identical chunks are stored once.
Consequences:

- **Incremental forever.** After the first backup, only changed chunks are
  uploaded. A daily backup of a quiet file server is a handful of packs.
- **Cross-file and cross-snapshot dedup.** The same photo in two folders, or
  unchanged data across 30 daily snapshots, is stored once.
- **Snapshots are cheap.** A snapshot is just a tree of references to chunks,
  so keeping a year of dailies costs little beyond the first full backup.

### The hot/cold split

A rustic "cold storage" repository is two buckets acting as one:

- **Hot bucket (S3 Standard):** config, keys, snapshots, indexes, trees.
  Everything needed to *understand* the repository without touching Glacier.
  Small, fast, and billed at Standard rates — but it's kilobytes-to-gigabytes,
  not terabytes.
- **Cold bucket (Deep Archive):** data packs. The actual file contents, and
  ~everything you pay storage on.

This split is what makes instant snapshot browsing possible: listing files
reads trees and indexes from the hot bucket, never from Glacier.

### Why 512 MiB packs

Deep Archive bills **$0.05 per 1,000 PUTs** and **~40 KB of overhead per
object**. Storing 1 TB as 4 MiB objects means 262,144 PUTs (**$13.11**) and
~10 GB of billed overhead. Storing it as 512 MiB packs means 2,048 PUTs
(**$0.10**) and ~80 MB of overhead. New repositories are initialized with
**512 MiB data packs / 32 MiB tree packs** (rustic grows these automatically
as the repo grows).

The honest trade-off, stated in the code: higher memory use during backup
(rustic buffers whole packs, several in parallel) and **coarser partial
restores** — retrieving a 1 KB file still thaws its entire 512 MiB pack.
For a vault, that's the right trade.

### Why Bulk retrieval by default

Bulk ($0.0025/GB, up to 48 h) vs. Standard ($0.02/GB, ~12 h): 8× cheaper for
waiting longer. Data that has sat untouched in a vault for months can wait
another day. Every restore uses Bulk unless you go out of your way.

### Why CloudFront for restore downloads

CloudFront gives every account **1 TB/month of free data transfer out** —
and S3-to-CloudFront transfer is free. A restore that downloads packs
through a private CloudFront distribution instead of directly from S3
therefore pays **$0 egress** for the first terabyte each month, versus ~$0.09/GB
from S3. The distribution requires signed URLs for every request, so the
bucket stays private; the appliance mints those URLs inside a
localhost-only proxy, per object, with a 2-minute expiry. Rustic never sees
a signed URL — it just talks S3-shaped HTTP to the proxy.

The free allowance is account-wide and resets monthly; it is shared with any
other CloudFront traffic in the account. Past 1 TB, CloudFront overage is
~$0.085/GB in the US/Europe (the `PriceClass_100` distribution only uses
US/EU edge locations, the cheapest). Pack objects are content-addressed and
immutable, so the distribution caches them aggressively (30-day default TTL):
a repeat restore of the same packs is served from the edge instead of
re-fetched from S3. Backups never touch CloudFront (it can't receive
uploads) — this is a download-only path.

### Why S3 Batch Operations for restores

Thawing N packs with individual `RestoreObject` calls means N request
charges and N things to track. One S3 Batch Operations job takes a manifest
of keys, fans the restores out, and reports completion — for $0.25 + $1.00
per million objects. The warmup tool
([warmup-s3-archives](https://gitlab.com/philipmw/warmup-s3-archives))
submits the batch job and *blocks until Glacier reports the packs
available*, listening on the SQS queue for completion notifications. Rustic
then downloads normally. One command, one wait, no polling loops in our code.

### Local metadata catalog

After every backup, the app syncs snapshot metadata into a local SQLite
catalog. The UI reads the catalog — never Glacier — so browsing is instant
and free. Catalog sync failures are surfaced on the job record, because a
silent sync failure once caused snapshots to never appear in the UI.

### Encryption and trust model

- Repository data is encrypted by rustic with a random 32-byte repo password
  generated at setup (`/config/repo.password`, mode 0600).
- AWS credentials for daily operation belong to a **limited IAM user** created
  by the CDK stack; its access key is created by the app at setup and stored
  encrypted in the database. Your powerful setup credentials are used once,
  for deployment, and never again.
- The web UI has a single admin login (bcrypt-hashed password, JWT sessions).

## Implementation

### Components

| Component | Role |
|---|---|
| Go API (`api/`) | HTTP server, scheduler, backup/restore engines, provisioning, SQLite DB |
| Next.js UI (`frontend/`) | Static frontend served by the Go server |
| Rustic 0.11.4 | Backup, snapshot, and restore engine; cold-storage repository format |
| warmup-s3-archives 1.3.0 | Submits S3 Batch restore jobs and waits for Glacier (invoked by rustic's `--warm-up-command`) |
| Localhost S3→CloudFront proxy (`api/internal/cloudfront`) | Serves rustic's S3 GET/HEAD as per-object signed CloudFront URLs; passes through anything else (e.g. ListObjectsV2) to real S3 via SigV4; loopback-only |
| CDK stack (`cdk/`) | Provisions the core AWS resources (S3, SQS, IAM) |
| CloudFront provisioner (`api/internal/cloudfront`) | Creates the distribution, OAC, key group, and cache policy via the AWS SDK after the CDK deploy |
| SQLite (`/database/glaciervault.db`) | Backup definitions, jobs, restores, snapshot catalog, encrypted credentials |

The Docker image pins rustic 0.11.4 and warmup-s3-archives 1.3.0 (downloaded
as a standalone binary from the upstream GitLab project). The Dockerfile
fails the build if the warmup binary can't be installed — no silent skips.

### AWS resources (created by the CDK stack, plus CloudFront)

| Resource | Purpose |
|---|---|
| Hot bucket | Repository metadata (S3 Standard) |
| Cold bucket | Data packs (Deep Archive) |
| Batch-manifests bucket | Per-restore key manifests for S3 Batch Operations |
| Batch-reports bucket | Batch job completion reports |
| Cold-events SQS queue | `OBJECT_RESTORE_COMPLETED` notifications the warmup tool waits on |
| `rustic-iam-user` | Limited IAM user for daily backup/restore operations |
| S3 batch IAM role | Role S3 Batch Operations assumes to read manifests and restore objects |
| CloudFront distribution | Free-egress download path for restores (created by the app via the AWS SDK, not CDK) |
| CloudFront origin access control | Lets the distribution read the cold bucket while it stays private |
| CloudFront key group + signing key | Signed-URL auth; the private key is generated locally and stored encrypted in the DB |

The CDK app itself is not vendored in this repo — the Docker build fetches
`glacier-cold-storage-cdk` from upstream `rustic-rs/rustic-aws` at image
build time. CloudFront is provisioned separately (Go, AWS SDK) right after
the CDK deploy, while the setup credentials are still available.

### Backup flow

```text
Scheduler (cron) or "Run now"
  → Go engine spawns: rustic backup --tag <name> <source paths...>
  → chunks → encrypts → dedups → PUTs data packs to cold bucket (Deep Archive),
    metadata to hot bucket (S3 Standard)
  → job log streams to the DB; UI tails it live via SSE
  → on success: catalog sync (snapshots → SQLite)
```

Source paths are paths *inside the container* — host directories are mounted
read-only via Docker volumes (e.g. `- /home:/mnt/home:ro`).

### Snapshot browsing flow (no Glacier involved)

```text
UI → GET /api/snapshots            (local catalog — instant, free)
UI → GET /api/snapshots/{id}/files (rustic ls against the HOT bucket only)
```

`rustic ls` needs only trees/indexes, which live in the hot bucket, so
browsing a snapshot never triggers a retrieval.

### Restore flow (the warmup sequence)

```text
UI: pick snapshot → browse → select files (or nothing = full) → destination
  → POST /api/restores  →  restore job row (status: queued)
  → engine spawns ONE command:

    rustic restore <snapshot> <destination> \
      --warm-up-command "glaciervault-warmup %paths" \
      --warm-up-batch 1000

  1. rustic resolves the EXACT pack set the selected files need
     (dedup-aware: shared packs thawed once)
  2. per batch of ≤1000 keys, rustic execs glaciervault-warmup with the S3 keys
  3. glaciervault-warmup runs warmup-s3-archives, using a per-job config
     written to the job's working dir (mode 0600: batch role ARN,
     manifest/report buckets, account ID, SQS queue URL, BULK tier, copy
     expiry — see sizing below):
       - uploads the key manifest to the batch-manifests bucket
       - creates the S3 Batch Operations restore job (BULK tier)
       - blocks until every pack reports OBJECT_RESTORE_COMPLETED via SQS.
     The tool's wait budget is expiration_in_days × 24h with no separate
     knob, so a 1-day expiry alone would give up after 24h on a healthy
     thaw (BULK can take up to 48h). The wrapper retries the tool on its
     "Timed out waiting" message — up to 3 attempts × 24h = 72h total per
     batch. Retries are safe: the tool's RestoreStatus pre-check
     short-circuits on packs that thawed between attempts, and duplicate
     Batch restore requests are idempotent. Any other tool failure aborts
     immediately.
     **Warm-up sizing:** before the first Batch job is submitted, the
     server counts data packs in the index (`rustic cat index`) and writes
     warmup-plan.txt (batch count N, download headroom DL). The wrapper
     sets each batch's expiration_in_days individually —
     E_k = 2·(N−k) + DL + 1 days — because batches run sequentially while
     each copy's clock starts at its own thaw: early batches must outlive
     the later thaws, late batches only their own download. A single batch
     keeps the 1-day minimum. The full calculation, with worked examples,
     is in [Workflow §4](workflow.md); it is the design intent and still
     needs a live multi-batch run to confirm.
  4. rustic downloads the thawed packs and writes files to the destination.
     When the CloudFront free-egress path is enabled, the restore runs with
     a per-job rustic profile whose cold backend points at the localhost
     proxy (`endpoint = "http://127.0.0.1:18923"`); pack downloads then flow
     through signed CloudFront URLs instead of paid S3 egress. The proxy
     serves GET/HEAD on object keys via CloudFront and passes anything else
     the distribution can't serve (notably ListObjectsV2 for keys/ and
     snapshots/) through to real S3, signed with SigV4. Only the
     download phase uses the proxy — warmup, snapshots, and backups keep
     using direct S3. If the proxy is unreachable the restore fails open to
     direct S3 (it costs more, but it completes).
  5. job marked completed; log retained on the restore record
```

**Restart resilience:** the moment warmup-s3-archives submits the Batch
job, its job ID is captured from the tool's output and persisted to
`restore_jobs.batch_job_id` (status advances to `retrieval_in_progress`).
If the container restarts mid-warmup, startup reconciliation triages every
non-terminal job: rows with no Batch job ID are marked failed (nothing was
submitted server-side; safe to retry), while rows with a job ID resume — a
background goroutine polls `s3:DescribeJob` until the job completes and
then runs step 4 as a plain download-only restore (no second Batch job, no
second 48 h wait). The Batch job ID is also exposed on the job-detail API
as `batchJobId` for debugging.

**Known gap — multi-batch restarts:** only the *first* submitted batch's
job ID is persisted (`WHERE batch_job_id IS NULL` ignores later ones), and
the wrapper's batch counter lives in the temp work dir, which does not
survive container replacement. If the container restarts during a
multi-batch (1000+ packs) warmup, unsubmitted batches are lost for that
restore: the resume path waits for the recorded batch, then runs the
warmup-free download, which fails on packs that were never thawed. The
recovery is to start a new restore — `warmup-s3-archives` re-checks every
pack via `HeadObject` and only re-requests still-cold ones, so thawed
batches are reused until their per-batch copy expiry. Single-batch
restores are unaffected.

Restore stages shown in the UI: `queued → warmup_requested →
retrieval_in_progress → retrieval_complete → restoring → completed`
(`failed` on error). The long pole is step 3 — Bulk retrieval can take up to
48 hours, during which the job sits in the warmup/retrieval stages. That is
normal.

Notable implementation details:

- **Restore destination is positional** (`rustic restore <snap> <dest>`);
  rustic has no `--target` flag.
- **`%paths` batching** is a rustic 0.11 feature: the literal `%paths` in the
  warm-up command is replaced with the batch's S3 keys.
- **Config parsing is strict**: `warmup-s3-archives` requires the tier as
  uppercase `BULK` in `warmup-s3-archives-config.toml`, and the Rust AWS SDK
  needs `AWS_REGION` set — both handled by the engine.
- **Role ARN, not role name**: CloudFormation reports an IAM role's physical
  ID as its *name*; the deploy step resolves the real ARN via IAM `GetRole`,
  and restores tolerate a stored bare name from older setups.
- **Snapshot JSON changed in rustic 0.11** (`snapshots --json` now returns
  grouped objects); the parser handles 0.11 groups, 0.9 grouped pairs, and
  flat arrays, with unit tests built from real 0.11.4 output.

### Prune flow

Deleting a snapshot (`rustic forget`) only removes the snapshot reference.
`Prune repository` rewrites packs to drop unreferenced data — it can take a
long time and, on large repos, can trigger Glacier retrieval charges. The UI
warns before both, and offers "delete + prune" as one action. Pruning data
younger than 180 days still incurs the Deep Archive early-deletion charge.

### Related reading

- [`../cold-storage-arch.md`](../cold-storage-arch.md) — earlier hybrid-architecture notes
- [`../design.md`](../design.md) — original design sketch
