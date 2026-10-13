# Page Overview

Every page in the GlacierVault UI, what it's for, and what's on it.

## Dashboard (`/`)
<img width="1069" height="517" alt="image" src="https://github.com/user-attachments/assets/ad5eea38-dff0-4440-aa8d-ccefbe870633" />

The landing page. If setup hasn't completed, you're redirected to `/setup`.

- **Latest job** card: status of the most recent backup job.
- **Backup definitions** summary: how many sources are configured.
- **Snapshots** summary: count and total logical size across snapshots.
- Quick links into Backups, Snapshots, and Jobs.

*Tip: this is the "is everything okay?" glance — green jobs and a growing
snapshot count mean the appliance is doing its job.*

## Backup Sources (`/backups`)
<img width="1048" height="308" alt="image" src="https://github.com/user-attachments/assets/e4bc1255-db38-48ea-8476-15268d68a9bd" />

Where backup definitions live. Each definition is a card showing name,
enabled/disabled state, schedule, compression level, and
source paths, with actions:

- **▶ Run now** — starts a backup immediately (creates a job you can watch
  on the Jobs page).
- **Enable / Disable** — pauses the schedule without deleting the definition.
- **Edit** — change name, paths, schedule, compression.
  (The encryption password is only asked at creation.)
- **Delete** — removes the definition (with confirmation). Does not touch
  existing snapshots.

**+ New Backup** opens the creation form: name, one or more source paths
(paths *inside the container*, e.g. `/mnt/photos` — mount host dirs read-only
via Docker volumes), schedule presets (Daily 2 AM, Weekly Sun 2 AM, Every 6
hours, Hourly, or custom cron), compression slider (1–22), and the encryption password field.
Cron expressions are evaluated in the container's `TZ` timezone (set
`TZ=America/Chicago` in docker-compose.yml; defaults to UTC) — the Backups
page shows which zone is active next to each schedule.

## Jobs (`/jobs`)
<img width="1055" height="626" alt="image" src="https://github.com/user-attachments/assets/b0219756-edb8-423d-9996-1d5010443ad4" />

Every backup run (and the setup deploy) is a job.

- **Job list:** id, status (`running` / `completed` / `failed`), which backup
  definition it belongs to, bytes transferred, duration, start time. Failed
  jobs show the error inline.
- **Detail view:** full log output with **live streaming** while running
  (server-sent events), color-coded `[error]` / `[warn]` lines.

*Tip: when something goes wrong, this page has the answer — rustic's own
output is in the log verbatim.*

## Snapshots (`/snapshots`)
<img width="1072" height="467" alt="image" src="https://github.com/user-attachments/assets/857028cd-e43d-402c-a08f-4661d4718d13" />

The heart of the app: everything you've backed up, browsable without
touching Glacier.

- **Storage summary bar** (top): total repository bytes, bytes in data packs,
  index size, snapshot count, logical size — plus the **Prune repository**
  button (with a confirmation warning about duration and potential Glacier
  charges).
- **Sidebar:** snapshot list — hostname, backup time, file count, total size.
  🗑 deletes a snapshot (with an optional "also prune now" checkbox).
- **File browser:** breadcrumb navigation, name/size/modified columns,
  checkboxes for selecting files and folders (select-all per folder).
- **First-browse indexing:** the first time you open a snapshot, GlacierVault
  builds its file index (`rustic ls --long` streamed into the local catalog:
  paths, sizes, dates) and shows live progress — a few minutes of CPU for
  very large snapshots, then instant browsing afterwards. One-time per
  snapshot.
- **Pagination:** folders with more than 1000 items are paged (1000/page
  default, 250/500/1000/2500 selectable) with page numbers and a total count.
- **Restore panel:** destination path input + **Restore** button. With
  nothing checked it restores the **full snapshot**; with selections it
  restores only those paths. Notes the 12–48 h Glacier wait.

*Tip: browsing here is free — it reads the hot bucket and local catalog
only. Browse as much as you like before committing to a restore.*

## Restores (`/restore`)
<img width="1046" height="694" alt="image" src="https://github.com/user-attachments/assets/b34db0c9-d734-44ed-90b9-c88ee9e4f407" />

Tracks every restore job from request to files-on-disk.

- **Job list:** id, stage badge, destination, start time. Auto-refreshes.
- **Detail view:**
  - **Stage timeline:** `Queued → Warmup requested → Retrieving from Glacier
    → Retrieval complete → Restoring files → Completed`, with the current
    stage pulsing. `Failed` shows the error message.
  - **Facts:** destination, snapshot id, retrieval/restore timestamps,
    full-vs-partial path count, and the requested path list for partials.
  - **Log output:** streamed lines from rustic and the warmup tool.

*Tip: a job sitting in "Retrieving from Glacier" for many hours is normal
on the Bulk tier — that's Glacier working, not the app stuck. Budget up to
48 h.*

## Settings (`/settings`)
<p float="left">
<img width="500" alt="image" src="https://github.com/user-attachments/assets/41517e75-952c-44aa-b5fd-877c15fe2240" /> <img width="500" alt="image" src="https://github.com/user-attachments/assets/62d4e6dc-3670-4df4-85a2-72d72ef4f83f" /> <img width="500" alt="image" src="https://github.com/user-attachments/assets/9487fe26-073e-4bfa-a74a-b9aab8ff47d9" />
</p>

- **Infrastructure:** region, deploy timestamp, hot and cold bucket names
  (from the setup record).
- **Free-egress restores (CloudFront):** status of the private CloudFront
  distribution used for restore downloads. Shows enabled/disabled, the
  distribution hostname, and whether the local signing proxy is running.
  **Enable** with a temporary AWS admin access key (used for the
  provisioning request only, never stored) — needed for installs that
  predate the feature or where provisioning failed during setup. **Disable**
  to fall back to paid S3 egress.
- **Snapshot deletion permission:** shows whether the backup user may delete
  objects (`s3:DeleteObject` — needed for snapshot delete and prune; the
  infrastructure is append-only by design and omits it). **Grant** or
  **Revoke (append-only)** with a temporary AWS admin access key (used for
  the request only, never stored). Revoking restores the append-only
  protection at the cost of disabling snapshot deletion.
- **Recovery:** **Download recovery package** (bucket names, region, repo
  password, manual restore instructions — store it somewhere safe) and
  **Rebuild snapshot catalog** (re-indexes snapshots from the hot repo; use
  if snapshots go missing after a disruption).
- **Notifications:** Apprise destination URLs (one per line —
  `discord://…`, `ntfy://…`, `mailto://…`, any service Apprise supports; see
  the Apprise wiki for URL formats) plus on/off switches for **backup
  completed**, **warmup complete** (Glacier finished thawing a restore's
  packs), and **restore complete**. The API shells out to the `apprise` CLI
  in the container; **Send test notification** verifies your URLs before you
  rely on them. Events dispatch in the background and never block a backup
  or restore.
- **Account:** change the UI password (min 8 chars), log out.

## Setup (`/setup`)

The first-run wizard (also reachable later to re-run/refresh the deployment).

1. **Credentials** — AWS access key, secret key, region, stack name.
2. **Validate** — confirms the identity via STS and shows the resource
   estimate (4 S3 buckets, 1 SQS queue, 1 IAM user, 1 IAM role,
   1 CloudFront distribution). Also offers the **Read-only snapshots**
   checkbox (unchecked by default): check it to keep the infrastructure
   append-only (no snapshot deletion), leave it unchecked to allow deletion.
   Changeable later in Settings.
3. **Deploy** — live CDK bootstrap + deploy logs (5–10 min), then automatic
   CloudFront provisioning for free-egress restores.
4. **Done** — confirmation; button to the dashboard.

Re-running setup on an existing deployment backfills any missing fields
(account ID, batch buckets) without harm.

## Login (`/login`)

Single admin login. Sessions are JWTs valid for 8 hours. The initial
password comes from the `INITIAL_PASSWORD` environment variable, or is
randomly generated and printed to the container log on first boot — change
it in Settings.
