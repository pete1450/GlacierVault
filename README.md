# GlacierVault

A self-hosted backup appliance that provides a simple web UI for scheduling and operating AWS Glacier Deep Archive backups. Built on [Rustic](https://github.com/rustic-rs/rustic) and the [rustic-aws glacier-cold-storage-cdk](https://github.com/rustic-rs/rustic-aws/tree/main/glacier-cold-storage-cdk) infrastructure project.

The goal is to make AWS Deep Archive as simple as possible to non-AWS experts: you bring credentials, GlacierVault handles the rest.

---

## THIS VERY MUCH A WORK-IN-PROGRESS REPO!
 - Currently the CDK does create resources and scheduled backups do seem to work. This is after a couple iterations with Copilot and pile of debugging. It ain't vibe-coding, but it's not far off at this point.
 - Effort at a robust design was taken but very little of the output has been verified.]
 - Heck who knows if a lot of the claimed features in the README below this exist but as a whole it's there.
 - Snapshots aren't being listed and thereforce restore hasn't been tested
 - Do not use rely on this project for your real backups yet!
I'd be very open to PRs if anyone wants to dig in.

## Features

- One-click AWS infrastructure deployment (CDK)
- Scheduled and on-demand incremental backups
- AES-256 encrypted, append-only repositories
- Glacier Deep Archive storage (lowest AWS storage cost)
- Instant snapshot browsing via a local metadata catalog — no Glacier retrieval needed just to look at your backups
- Point-in-time restore with progress tracking
- Multi-folder backup support
- Single Docker container deployment

---

## Quick Start

### 1. Run the container

```yaml
# docker-compose.yml (minimal)
services:
  glaciervault:
    image: glaciervault:latest
    ports:
      - "8080:8080"
    volumes:
      - glaciervault-config:/config
      - glaciervault-database:/database
      - glaciervault-cache:/cache
      - glaciervault-logs:/logs
      # Mount the folders you want to back up:
      - /home:/mnt/home:ro
      - /srv/media:/mnt/media:ro
    environment:
      - INITIAL_PASSWORD=changeme

volumes:
  glaciervault-config:
  glaciervault-database:
  glaciervault-cache:
  glaciervault-logs:
```

```bash
docker compose up -d
```

Then open `http://localhost:8080`.

### 2. Complete the setup wizard

Navigate to `/setup` and follow the three-step wizard:

1. Enter your AWS Access Key, Secret Key, and region.
2. GlacierVault validates the credentials and shows a summary of the resources it will create.
3. Click **Deploy Infrastructure** — this runs `cdk bootstrap` + `cdk deploy` and wires everything up automatically.

After deployment the app initialises the Rustic repositories and you can start configuring backup jobs.

---

## AWS Permissions

> **Note:** The exact minimum IAM permissions are still being determined. For now, using an IAM user with `AdministratorAccess` is the simplest option. Scoping this down is a planned improvement.

The CDK deployment creates:

- Two S3 buckets (hot metadata repository + cold data repository)
- Glacier Deep Archive lifecycle rules
- SQS queues for restore notifications
- IAM roles
- Lambda functions for warmup/notification

---

## Backup Configuration

Each backup job requires:

| Field | Example |
|---|---|
| Name | `Home directory` |
| Source paths | `/mnt/home`, `/mnt/media` |
| Schedule | `0 2 * * *` (daily at 2 AM) |
| Retention label | `Critical` / `Archive` / `Personal` |
| Encryption password | _(stored AES-256 encrypted)_ |

---

## Restore Workflow

Because backup data lives in Glacier Deep Archive, restores require a retrieval step before the data can be downloaded:

1. Browse snapshots instantly from the local catalog (no AWS calls needed).
2. Select the snapshot and destination folder, then click **Restore**.
3. GlacierVault submits Glacier retrieval requests and polls for completion.
4. Once archives are available, Rustic streams the data to the destination.

Restore states: **Queued → Warmup Requested → Retrieval In Progress → Retrieval Complete → Restoring → Completed**

---

## Architecture

```
┌─────────────────────────────────────────────┐
│              Docker Container               │
│                                             │
│  Next.js UI  ──►  Go API  ──►  Rustic CLI  │
│                    │                        │
│               Scheduler                     │
│               SQLite catalog                │
└─────────────────────────────────────────────┘
         │                    │
    AWS CDK deploy      AWS S3 / Glacier
    (setup only)        (backup storage)
```

### Components

| Component | Technology | Role |
|---|---|---|
| Web UI | Next.js / React | Setup wizard, backup management, snapshot browser, restore UI |
| API | Go (chi router) | Auth, provisioning, scheduling, snapshot indexing, job state |
| Backup engine | Rustic (subprocess) | Init repos, run backups, list snapshots, execute restores |
| Provisioner | AWS CDK via shell | Deploy/teardown Glacier infrastructure |
| Scheduler | `robfig/cron` | Trigger backup jobs on cron schedules |
| Metadata catalog | SQLite | Local index of snapshots and file trees for instant browsing |

### Hybrid cold-storage design

The repository uses Rustic's official two-bucket cold-storage layout:

- **Hot bucket** — stores repository metadata (`config`, `snapshots`, `indexes`, `trees`, `keys`). Fast access, small volume.
- **Cold bucket** — stores backup pack data in Glacier Deep Archive. Extremely cheap; retrieval required before restore.

A local SQLite catalog mirrors snapshot and file-tree metadata so the UI never needs to hit Glacier just to display backup history.

The catalog is **disposable** — if it's lost or corrupted, it can be rebuilt by re-reading the repository:

```bash
# Rebuild triggered from the UI, or manually via the API
rustic snapshots --json   # re-enumerate all snapshots
rustic ls <snapshot-id> --json  # re-populate file indexes
```

---

## Security

- AWS credentials are encrypted at rest with AES-256 before being written to the config volume.
- JWT-based session auth on the API.
- Backup data is encrypted end-to-end by Rustic before it leaves the host.

### Disaster recovery

GlacierVault can generate a **Recovery Package** containing your repository configuration, bucket identifiers, encryption password reminder, and manual restore instructions. No restore should ever depend solely on this application being available.

---

## Teardown / Starting Over

> If you delete the S3 bucket manually, CloudFormation's `CDKToolkit` stack will still exist but point to the deleted bucket. To avoid a stuck stack:
>
> 1. Delete the `CDKToolkit` stack first.
> 2. Delete the GlacierVault application stack.
> 3. Delete any remaining S3 buckets.
>
> If a stack deletion gets stuck, you can force it with a role that has broad permissions:
> ```bash
> aws cloudformation delete-stack \
>   --stack-name CDKToolkit \
>   --role-arn arn:aws:iam::<account>:role/<super-role>
> ```

---

## Development

### Prerequisites

- Go 1.22+
- Node.js 20+
- Docker (for the full container build)

### Run locally

```bash
# API
cd api
go run ./cmd/server

# Frontend (separate terminal)
cd frontend
npm install
npm run dev
```

### Build the container

```bash
docker build -f docker/Dockerfile -t glaciervault:latest .
```

---

## Project Structure

```
api/                  Go API service
  cmd/server/         Entry point
  internal/
    catalog/          Snapshot metadata sync
    crypto/           AES credential encryption
    db/               SQLite + migrations
    engine/           Rustic subprocess wrapper
    provisioning/     AWS CDK deployment
    restore/          Restore job orchestration
    scheduler/        Cron job runner
    server/           HTTP router
frontend/             Next.js UI
  app/
    setup/            Initial setup wizard
    backups/          Backup job management
    snapshots/        Snapshot browser
    jobs/             Job history
docker/               Dockerfile + compose file
cdk/                  (CDK assets)
```

---

## Roadmap / Future Enhancements

- Scope down minimum IAM permissions required
- Email / webhook notifications on job completion or failure