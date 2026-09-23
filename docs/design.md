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
| CDK stack (`cdk/`) | Provisions all AWS resources |
| SQLite (`/database/glaciervault.db`) | Backup definitions, jobs, restores, snapshot catalog, encrypted credentials |

The Docker image pins rustic 0.11.4 and warmup-s3-archives 1.3.0 (downloaded
as a standalone binary from the upstream GitLab project). The Dockerfile
fails the build if the warmup binary can't be installed — no silent skips.

### AWS resources (created by the CDK stack)

| Resource | Purpose |
|---|---|
| Hot bucket | Repository metadata (S3 Standard) |
| Cold bucket | Data packs (Deep Archive) |
| Batch-manifests bucket | Per-restore key manifests for S3 Batch Operations |
| Batch-reports bucket | Batch job completion reports |
| Cold-events SQS queue | `OBJECT_RESTORE_COMPLETED` notifications the warmup tool waits on |
| `rustic-iam-user` | Limited IAM user for daily backup/restore operations |
| S3 batch IAM role | Role S3 Batch Operations assumes to read manifests and restore objects |

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
      --warm-up-command "warmup-s3-archives %paths" \
      --warm-up-batch 1000

  1. rustic resolves the EXACT pack set the selected files need
     (dedup-aware: shared packs thawed once)
  2. per batch of ≤1000 keys, rustic execs warmup-s3-archives with the S3 keys
  3. warmup-s3-archives, using a per-job config written to the job's working
     dir (mode 0600: batch role ARN, manifest/report buckets, account ID,
     SQS queue URL, BULK tier, 2-day expiry):
       - uploads the key manifest to the batch-manifests bucket
       - creates the S3 Batch Operations restore job (BULK tier)
       - blocks until every pack reports OBJECT_RESTORE_COMPLETED via SQS
         (up to the 72 h overall timeout)
  4. rustic downloads the thawed packs and writes files to the destination
  5. job marked completed; log retained on the restore record
```

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
