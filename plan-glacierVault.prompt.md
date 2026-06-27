# Plan: GlacierVault Implementation

## TL;DR
Build a single-container self-hosted backup appliance wrapping Rustic + AWS Glacier Deep Archive. A Go API backend drives Rustic subprocesses, a CDK provisioning flow, a cron scheduler, and a SQLite metadata catalog. A Next.js frontend provides the setup wizard, backup config, snapshot browser, and restore UI.

---

## Phase 1: Foundation & Project Scaffold

1. Establish monorepo layout:
   ```
   glaciervault/
   ├── api/           # Go module
   ├── frontend/      # Next.js app
   ├── docker/        # Dockerfile + docker-compose
   └── cdk/           # Bundled CDK assets from rustic-aws
   ```
2. Initialize Go module (`go mod init github.com/glaciervault/api`)
3. Initialize Next.js app (`npx create-next-app@latest frontend --typescript`)
4. Write `docker/Dockerfile` — multi-stage build:
   - Stage 1: Build Go binary
   - Stage 2: Build Next.js static output
   - Final: Debian/Ubuntu slim with Rustic binary, AWS CLI, Node runtime, CDK, built artifacts
5. Write `docker-compose.yml` with volumes: `/config`, `/database`, `/cache`, `/logs`, and user-defined backup source mounts
6. Define SQLite schema (migration files in `api/internal/db/migrations/`):
   - `aws_config` — region, encrypted_access_key, encrypted_secret_key, stack_name, hot_bucket, cold_bucket, sqs_url, iam_user, batch_role_arn, deployed_at
   - `backup_definitions` — id, name, source_paths (JSON), schedule, retention_label, compression_level, encrypted_password, enabled, created_at
   - `snapshots` — id, snapshot_id (rustic), backup_def_id, hostname, tags (JSON), total_size, file_count, backup_time, synced_at
   - `file_index` — id, snapshot_id (FK), path, size, mtime, is_dir
   - `backup_jobs` — id, backup_def_id, started_at, completed_at, status, bytes_transferred, error_message, log_output
   - `restore_jobs` — id, snapshot_id (FK), requested_paths (JSON), destination, status, warmup_status, retrieval_started_at, restore_started_at, completed_at, error_message

---

## Phase 2: AWS Provisioning Service

7. Bundle the `glacier-cold-storage-cdk` CDK project inside the container at `/opt/cdk/`
8. Implement `api/internal/provisioning/` package in Go:
   - `ValidateCredentials(key, secret, region)` — call `sts:GetCallerIdentity` via AWS SDK
   - `EstimateResources()` — return static description of what will be deployed (4 S3 buckets, 1 SQS queue, 1 IAM user, 1 IAM role)
   - `BootstrapAndDeploy(key, secret, region, stackName)` — set env vars, run `cdk bootstrap` then `cdk deploy --require-approval never --outputs-file /tmp/cdk-outputs.json`, capture stdout/stderr to stream to UI
   - `ParseOutputs(outputsFile)` — read CloudFormation outputs JSON, extract bucket names, SQS URL, IAM role ARN
   - `CreateIAMAccessKey(user)` — call AWS SDK to create access key for the IAM user created by CDK, store in DB
9. Provision endpoints:
   - `POST /api/setup/validate` — validate AWS credentials
   - `POST /api/setup/deploy` — trigger CDK deploy, return job ID for SSE streaming
   - `GET /api/setup/status` — deployment status and resource summary

---

## Phase 3: Backup Engine

10. Implement `api/internal/engine/` package in Go:
    - `InitRepository(hotBucket, coldBucket, password)` — run `rustic init` with cold-storage config
    - `RunBackup(defID, sourcePaths []string, tags []string, password string)` — run `rustic backup`, capture JSON progress output, write to backup_jobs
    - `ListSnapshots(password string)` — run `rustic snapshots --json`, return parsed structs
    - `ListFiles(snapshotID, password string)` — run `rustic ls <id> --json`, return file tree
    - `RunRestore(snapshotID, paths []string, destination, password string)` — run `rustic restore`
    - All commands write stdout/stderr to a ring buffer per job ID for streaming
11. Rustic config template written to `/config/rustic.toml` after provisioning, using hot/cold bucket from DB

---

## Phase 4: Metadata Sync

12. Implement `api/internal/catalog/` package:
    - `SyncAfterBackup(jobID)` — call `ListSnapshots`, diff against DB, insert new snapshots
    - `IndexSnapshot(snapshotID)` — call `ListFiles`, bulk-insert into `file_index`
    - `RebuildCatalog()` — enumerate all rustic snapshots, re-index all file trees
13. Expose endpoints:
    - `POST /api/catalog/rebuild` — trigger full rebuild (background job)
    - `GET /api/catalog/status` — sync status

---

## Phase 5: Scheduler

14. Implement `api/internal/scheduler/` using `robfig/cron` library:
    - Parse cron expressions from `backup_definitions`
    - On trigger: create `backup_jobs` record, call engine, call catalog sync
    - Support named schedules: "Daily" = `0 2 * * *`, "Weekly" = `0 2 * * 0`
15. Scheduler starts on API server boot, watches `backup_definitions` for changes

---

## Phase 6: Restore Workflow

16. Implement `api/internal/restore/` package:
    - `InitiateRestore(snapshotID, paths, destination)` — create restore_job record with status=Queued
    - `ExecuteWarmup(restoreJobID)` — invoke `warmup-s3-archives` binary, update status=WarmupRequested
    - `PollRetrievalStatus(restoreJobID)` — poll SQS queue for `s3:ObjectRestore:Completed` events, update status=RetrievalComplete
    - `ExecuteRusticRestore(restoreJobID)` — run restore once retrieval complete
    - Background goroutine polls active restore jobs every 60s
17. Restore endpoints:
    - `POST /api/restores` — initiate restore
    - `GET /api/restores` — list restore jobs
    - `GET /api/restores/:id` — restore job detail + progress

---

## Phase 7: Core API Service

18. Implement `api/internal/server/` using `chi` router:
    - Auth middleware: simple password-based session token (bcrypt hash stored in DB, JWT session)
    - SSE endpoint `GET /api/jobs/:id/stream` — stream log lines from engine ring buffer
    - All handlers reference packages from phases 2–6
19. Full endpoint surface:
    - Auth: `POST /api/auth/login`, `POST /api/auth/logout`
    - Setup: `/api/setup/validate`, `/api/setup/deploy`, `/api/setup/status`
    - Backups: CRUD on `/api/backups`
    - Jobs: `GET /api/jobs`, `GET /api/jobs/:id`
    - Snapshots: `GET /api/snapshots`, `GET /api/snapshots/:id/files`
    - Restores: `POST /api/restores`, `GET /api/restores`, `GET /api/restores/:id`
    - Catalog: `POST /api/catalog/rebuild`
    - Recovery: `GET /api/recovery/package`
20. Credential encryption: AES-256-GCM with key derived from a machine-generated master key stored in `/config/master.key` (created on first boot, never leaves container unless explicitly exported)

---

## Phase 8: Web UI

21. Next.js app with pages:
    - `/setup` — multi-step wizard (credentials → validate → estimated resources → deploy → init repo)
    - `/` — dashboard (next backup time, last job status, storage summary)
    - `/backups` — list and create backup definitions
    - `/snapshots` — snapshot browser with file tree (reads local catalog)
    - `/restore` — restore request flow
    - `/jobs` — job history with live log streaming via SSE
    - `/settings` — change password, recovery package download
22. UI state management: React Query for server state, Zustand for local UI state
23. Component library: shadcn/ui (Tailwind-based, easy self-hosted setup)
24. SSE integration for live job log tailing

---

## Phase 9: Security Hardening

25. AES-256-GCM encryption for all secrets in DB (backup passwords, AWS keys) using master key
26. Optional Docker Secrets / env var override for master key
27. Session auth with short-lived JWT, refresh token stored in httpOnly cookie
28. No credentials in URLs or query params
29. Recovery package endpoint: exports encrypted ZIP containing rustic config, bucket names, repo password hint, restore instructions

---

## Relevant Files (to create)

- `api/cmd/server/main.go` — entrypoint
- `api/internal/db/` — SQLite schema + migrations (using `golang-migrate`)
- `api/internal/provisioning/provisioner.go`
- `api/internal/engine/rustic.go`
- `api/internal/catalog/sync.go`
- `api/internal/scheduler/scheduler.go`
- `api/internal/restore/restore.go`
- `api/internal/server/router.go`
- `api/internal/crypto/aes.go`
- `frontend/app/` — Next.js App Router pages
- `docker/Dockerfile`
- `docker/docker-compose.yml`
- `cdk/` — bundled glacier-cold-storage-cdk assets

---

## Verification

1. `docker build -t glaciervault .` — container builds cleanly, all binaries present
2. `docker run -p 8080:8080 glaciervault` — UI accessible, setup wizard renders
3. Integration test: supply test AWS credentials → validate → deploy CDK stack → confirm 4 S3 buckets + SQS queue exist in AWS
4. Create a backup definition for `/tmp/testdata` → trigger manual backup → confirm rustic snapshot created, snapshot appears in UI from SQLite catalog without AWS calls
5. Browse snapshot file tree — confirm no AWS API calls made
6. Initiate restore → confirm restore job transitions through all states
7. `go test ./...` — unit tests pass for engine, catalog, scheduler, crypto packages
8. Verify `/config/master.key` is created on first boot and AWS credentials in DB are not stored in plaintext

---

## Decisions

- **API language**: Go (preferred per design doc)
- **Router**: `chi` — lightweight, idiomatic Go, supports middleware cleanly
- **CDK bundling**: CDK assets bundled in container (not downloaded at runtime) to remove external dependency at deploy time
- **IAM access key creation**: Done programmatically via AWS SDK after CDK deploy (CDK creates the IAM user but not the access key)
- **warmup-s3-archives**: Use the binary from the rustic-aws repo; bundled in container
- **File index population**: Done lazily per snapshot on first browse, not eagerly for all snapshots
- **No Lambda**: The CDK project deploys no Lambda — restore polling is done by the app via SQS

## Out of Scope (MVP)

- Multi-machine agent support
- Backup verification (automated restore tests)
- Multi-user access control
- Non-AWS cloud providers
- HashiCorp Vault / AWS Secrets Manager integration (optional future)
