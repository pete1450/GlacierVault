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
| Retention Label | `critical`, `archive`, or `personal` | Organizational tag; pick the one that matches how much you care |
| Compression Level | 1 (fastest) – 22 (smallest), default 3 | Photos/video are already compressed — low is fine. Text and VM images compress well — go higher. |
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

Remember the **180-day minimum**: pruning data archived less than 180 days
ago still bills you for the remainder. Don't churn the vault.

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
   `rustic -P ./rustic restore <snapshot-id> /destination --warm-up-command "warmup-s3-archives %paths" --warm-up-batch 1000`.
   The tool submits an S3 Batch restore at the cheapest BULK tier and waits
   for Glacier, then rustic downloads automatically.

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
