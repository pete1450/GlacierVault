# User Workflow

The full end-to-end loop: set up → back up → watch → restore → clean up →
survive disaster. For the one-time infrastructure part, see [Setup](setup.md).

## 1. Create a backup definition

**Backups → + New Backup.** Each definition is a named set of source folders
plus a schedule.

| Field | What it means | Guidance |
|---|---|---|
| Name | Label, also used as a rustic tag | e.g. "Home Photos", "VM images" |
| Source Paths | Folders **inside the container** | `/mnt/home`, not `/home` — must match your Docker volume mounts |
| Schedule | When it runs | Daily 2 AM / Weekly Sun 2 AM / Every 6 hours / Hourly / custom cron |
| Compression Level | 1 (fastest) – 22 (smallest), default 3 | Actually applied: passed to Rustic as `--set-compression`. Photos/video are already compressed — low is fine. Text and VM images compress well — go higher. |
| Encryption Password | Asked once, at creation | See the warning below |

> ⚠️ **About the encryption password field:** the UI collects a password per
> backup definition, but the repository currently uses **one repo-wide
> password**, generated randomly at setup and stored at `/config/repo.password`
> (mode 0600). The per-definition value is stored encrypted but is not what
> protects your data. **The secrets that actually matter are the repo
> password and the recovery package** — safeguard those. (Per-backup
> encryption is a known UX wart.)

After saving, the definition appears as a card with **Run now**, **Enable /
Disable**, **Edit**, and **Delete**. Disabling pauses the schedule without
deleting anything.

## 2. Run backups

- **Scheduled:** the in-app scheduler fires per the cron expression and runs
  `rustic backup --tag <name> <paths…>`. Incremental forever — only changed
  chunks upload.
- **On demand:** **Run now** on the backup card. Use it for the first backup
  (so you can watch it) and before big changes.

Watch progress on the **Jobs** page: live log streaming, bytes transferred,
duration. A job that completes but shows a *catalog sync* error needs
attention — snapshots won't appear in the UI until the catalog syncs
(Settings → Rebuild snapshot catalog can repair it).

## 3. Monitor snapshots

**Snapshots** page:

- **Storage summary bar** (top): total bytes, data-pack bytes, index size,
  snapshot count, logical size. Your always-visible cost dashboard.
- **Sidebar:** every snapshot — hostname, time, file count, size. Click to
  browse.
- **File browser:** drill through folders (served from the hot bucket —
  **browsing is instant and costs nothing in Glacier fees**), with name, size,
  and modified date.

This is also where restores start (below) and where old snapshots are
deleted.

## 4. Restore

Restores go through Glacier, so set expectations first: **Bulk retrieval
takes up to 48 hours** and every restore submits a $0.25 S3 Batch Operations
job. Plan restores; batch small ones together.

**Steps:**

1. On **Snapshots**, open the snapshot you want.
2. Browse and **check the files/folders** you need — or check nothing to
   restore the **entire snapshot**.
3. Enter a **destination path** (inside the container, e.g.
   `/restore/output` — mount a volume there if you want the files on the host).
4. Click **Restore**. You're taken to the **Restores** page.

**What happens next** (see [Design](design.md) for the full sequence):

- The job moves through `Queued → Warmup requested → Retrieving from
  Glacier → Retrieval complete → Restoring files → Completed`.
- The long stage is **Retrieving from Glacier**: the warmup tool waits on SQS
  for Glacier to finish thawing your packs. For Bulk, budget up to 48 h.
  The log tail on the Restores page shows progress lines as they stream in.
- When retrieval completes, rustic downloads the packs and writes your files.
  This part is fast.

**Surviving a container restart:** if the container goes down while a
restore is waiting on Glacier, the job is not lost. GlacierVault records
the S3 Batch job ID the moment the warmup tool submits it; on startup,
interrupted jobs are triaged automatically — jobs that never submitted a
Batch job are marked failed (safe to retry), while jobs with a recorded
Batch job ID **resume**: the server re-attaches to the in-flight Batch job
and runs the download as soon as Glacier reports the packs restored. No
duplicate Batch job, no second 48-hour wait. (Caveat: the temporary
restored copies expire — see the sizing below — so a restart that lasts
longer than the copy lifetime still needs a fresh restore.)

> **Multi-batch restores and restarts — known gap.** Only the *first*
> submitted batch's Batch job ID is recorded. If the container restarts
> during a multi-batch (1000+ packs) warmup, any batches that had not been
> submitted yet are **lost for that restore**: the resume path waits for
> the one recorded batch and then runs a warmup-free download, which will
> fail on packs that were never thawed. Single-batch restores (≤ 1000
> packs) are unaffected — the single recorded job covers the whole warmup.
> If you hit this, start a new restore for the same snapshot and paths:
> the warmup tool re-checks every pack and only re-requests the still-cold
> ones, so already-thawed batches are picked up, not re-thawed (their
> copies stay valid until their per-batch expiry).

### Warm-up sizing: batches, copy expiry, and timeout

This is the part of the restore most likely to surprise you, so here is the
full reasoning.

rustic warms packs in **sequential batches of 1000** (`--warm-up-batch
1000`): batch 2's S3 Batch restore job is only submitted after batch 1 has
*fully thawed*, and the download only starts after the *last* batch thaws.
But a restored (thawed) S3 copy lives only `expiration_in_days` from the
moment *its own* thaw completes. With a single fixed expiry, a multi-batch
restore is caught in a trap:

- too short, and the first batches' copies **expire before the download
  starts** (each later batch can take up to the 48 h Bulk SLA);
- too long, and the last batches' copies sit in S3 Standard for days after
  the download finished, costing storage for nothing.

GlacierVault therefore sizes every restore **dynamically**, before the
first Batch job is submitted, and logs the full calculation to the restore
log:

1. **Count packs.** `rustic cat index` is parsed for distinct data packs —
   an upper bound on what the restore needs (the safe direction;
   overestimating costs pennies, underestimating breaks restores).
2. **Batches:** N = ceil(packs / 1000).
3. **Download headroom:** DL = 0 for a single batch, else
   ceil(packs × 0.5 GB / download-rate), with a default download rate of
   1080 GB/day (≈100 Mbit/s; change it in **Settings → Restore warm-up
   tuning** if your link is slower).
4. **Per-batch copy expiry** (set by the `glaciervault-warmup` wrapper
   before each batch's Batch job is submitted):
   `E_k = 2·(N−k) + DL + 1` days. The `2·(N−k)` covers the worst case where
   every later batch takes the full 48 h Bulk SLA; `DL` covers the download
   on a slow link; `+1` is the AWS minimum.
5. **Overall timeout:** `72·N + 24·(DL+1)` hours (72 h per batch: 3 wrapper
   attempts × 24 h SQS watch budget, plus download headroom).

Worked examples (full 1000-pack batches, default 1080 GB/day):

| Packs | Batches | Copy expiry per batch (days) | Overall timeout |
|---|---|---|---|
| 1–1000 | 1 | [1] | 96 h |
| 1001–2000 | 2 | [4, 2] | 192 h |
| 2001–3000 | 3 | [7, 5, 3] | 288 h |
| 3001–4000 | 4 | [9, 7, 5, 3] | 360 h |

A single batch — everything up to ~500 GB — keeps the **1-day** copy
expiry. Longer expiries only ever apply past that, and each batch gets the
minimum its position requires: the first batch survives the later thaws,
the last batch only covers its own download.

> **Needs further consideration and testing.** The 48 h-per-batch SLA
> bound and the sequential-batch behavior are verified against rustic's
> warm-up implementation, but no multi-batch (1000+ pack) restore has been
> run end-to-end yet: the per-batch expiry rewrite, the SQS budget
> interaction (`expiration_in_days × 24 h` per tool invocation), and the
> batch-counter recovery across a container restart all need a live
> multi-batch run to confirm. The download-rate default (1080 GB/day ≈
> 100 Mbit/s) is a reasonable starting point, not a measurement — check your
> actual restore throughput and tune it.
> Until then, treat the table above as the design intent, not a proven
> behavior.

**Partial vs. full restore costs** (us-east-1, Bulk, CloudFront free-egress path enabled):

| Restore | Approx. cost | Why |
|---|---|---|
| One 25 MB photo from a 500 GB archive | **~$0.25** | Warms its 512 MiB pack ($0.001 in bytes) + one $0.25 batch job; egress inside the free allowance |
| Full 500 GB archive | **~$1.53** | Retrieval + batch job only — 500 GB fits the 1 TB CloudFront allowance |
| Full 2 TB VM backup | **~$92.51** | $5.47 retrieval + ~$87 egress overage (second TB of the month) |

Pack downloads go through the private CloudFront distribution by default
(1 TB/month free). Full breakdowns and more scenarios are in the
[Cost Guide](costs.md).

> Partial restores thaw **whole 512 MiB packs** — restoring a 1 KB file still
> retrieves its entire pack. That's the price of cheap storage; the byte
> cost is negligible, just don't expect surgical precision.

## 5. Delete snapshots and prune

- **Delete** (🗑 on a snapshot): removes the snapshot reference
  (`rustic forget`). Data shared with other snapshots stays.
- **Prune repository** (top bar): rewrites packs to drop unreferenced data
  and reclaims the space. **Slow** on large repos and can trigger Glacier
  retrieval charges — the confirmation dialog says so, believe it.
- There's a combined **"delete + prune now"** checkbox in the delete dialog.

> Deletion needs `s3:DeleteObject` on the backup buckets, which the upstream
> infrastructure deliberately omits (append-only mode protects your archives
> if the backup credentials leak). GlacierVault grants the backup user a
> narrow delete-only policy automatically at setup — consciously trading some
> of that protection for a working delete feature. Prefer maximum protection?
> Check **Read-only snapshots** during setup to skip the grant. Either way,
> you can grant or revoke it later in **Settings → Snapshot deletion
> permission** with a temporary AWS admin key. If deletion fails with
> AccessDenied, that's the first place to look.

Remember the **180-day minimum**: pruning data archived less than 180 days
ago still bills you for the remainder. Don't churn the vault.

If a delete fails with `NoSuchKey`/`NotFound` on the snapshot file, the
snapshot is already gone from the repository (both buckets) and only the
local catalog entry is stale — this can happen after an earlier interrupted
delete, since rustic removes the file from both backends non-atomically and
fails if either copy is already missing. GlacierVault detects this, drops
the stale catalog entry, and reports success instead of an error.

## 6. Disaster recovery (without the appliance)

If the container/host is gone, everything you need is in **Settings →
Recovery → Download recovery package**: bucket names, region, repo password,
and manual restore instructions. The outline:

1. Install `rustic` and `warmup-s3-archives` locally.
2. Unzip the package and `cd` into it. It contains `rustic.toml` (repo
   config with credentials), `repo.password`, and
   `warmup-s3-archives-config.toml` (batch role, buckets, queue URL, BULK
   tier) — the tool reads its config from the working directory.
3. Export your AWS credentials (`AWS_ACCESS_KEY_ID`,
   `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`).
4. List snapshots: `rustic -P ./rustic snapshots` (hot bucket — instant).
5. Restore (destination is positional; `%paths` is replaced by rustic with
   the exact S3 keys the snapshot needs):
   `rustic -P ./rustic restore <snapshot-id> /destination --warm-up-command "glaciervault-warmup %paths" --warm-up-batch 1000`.
   The wrapper retries warmup-s3-archives on its SQS wait timeout (the
   tool's wait budget is expiration_in_days × 24h with no separate knob, and
   BULK can take up to 48h); the tool submits an S3 Batch restore at the
   cheapest BULK tier and waits for Glacier, then rustic downloads automatically.

**Test this before you need it.** A backup you haven't restored is a hope,
not a backup. After your first real backup completes, do a small partial
restore and confirm the files.

## 7. Ongoing hygiene

- **Check the storage bar monthly.** Unexpected growth means a backup is
  catching something it shouldn't (caches, temp dirs — exclude them via
  source path choices).
- **Keep the recovery package current.** Re-download it after any
  re-setup or credential rotation.
- **Rotate the UI password** if it was ever generated into logs.
- **Watch for the 100 GB egress budget** if you restore often — it's per
  calendar month across the whole AWS account.
- **Set up notifications.** Add your Apprise destination URLs in Settings →
  Notifications and flip on the events you care about (backup completed,
  warmup complete, restore complete) so a 48-hour Bulk warmup doesn't need
  babysitting.
