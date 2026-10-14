# GlacierVault API Reference

Base URL: `http://localhost:8080` (default Docker port).

All endpoints return JSON. Errors look like `{"error": "message"}`.

## Authentication

Two ways to authenticate:

1. **Session cookie** — set by `POST /api/auth/login` or the OIDC callback.
   Browser clients use this automatically (`HttpOnly`, `SameSite=Strict`).
2. **API token** — `Authorization: Bearer gv_...` header. For scripts and
   automation. Tokens bypass TOTP. Create them in Settings → Security.

Endpoints marked 🔒 require authentication (cookie or Bearer token).

---

## Auth — public

### `POST /api/auth/login`
Password login. Rate-limited: 5 failures from an IP → 15-minute lockout (429).

```bash
curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"password":"YOUR_PASSWORD"}' -c cookies.txt
```

- `200 {"status":"ok"}` — sets session cookie.
- `202 {"status":"totp_required","pendingToken":"..."}` — TOTP enabled;
  complete with `POST /api/auth/totp/verify-login`.
- `401` — wrong password. `429` — locked out.

### `POST /api/auth/totp/verify-login`
Complete a TOTP-challenged login. No auth required (uses the pending token).

```bash
curl -s -X POST http://localhost:8080/api/auth/totp/verify-login \
  -H 'Content-Type: application/json' \
  -d '{"pendingToken":"...","code":"123456"}' -c cookies.txt
```

`code` accepts a 6-digit TOTP code or a single-use backup code.

### `GET /api/auth/config`
Returns `{"authType":"password"}` or `{"authType":"oidc"}`. Used by the login
page to decide whether to show the password form or the SSO button.

### `GET /api/version`
Returns `{"version":"..."}` — the build version (git SHA in Docker images,
`"dev"` for local builds). No auth required. Shown in the sidebar footer.

### `GET /api/auth/oidc/login`
Starts the OIDC flow. Redirects to the identity provider.

### `GET /api/auth/oidc/callback`
OIDC redirect target. Exchanges the code, creates the session, redirects to `/`.

---

## Auth — session management 🔒

### `POST /api/auth/logout`
Clears the session cookie.

### `POST /api/auth/logout-all`
Invalidates **all** sessions (bumps the token epoch) and clears the current
cookie. Other devices must log in again.

### `POST /api/auth/change-password`
Body: `{"currentPassword":"...","newPassword":"..."}` (min 8 chars).
Changing the password also invalidates all other sessions.

---

## Two-Factor Authentication (TOTP) 🔒

Password-auth only. Not available when `AUTH_TYPE=oidc` (MFA is delegated
to the identity provider).

### `GET /api/auth/totp/status`
Returns `{"enabled":true|false}`.

### `POST /api/auth/totp/setup`
Generates a secret. Returns `{"secret":"...","url":"otpauth://..."}`.
Scan the QR (or enter the secret manually) in your authenticator app, then
confirm with `verify-setup`.

### `POST /api/auth/totp/verify-setup`
Body: `{"code":"123456"}`. Enables TOTP. Returns
`{"status":"ok","backupCodes":["...","..."]}` — 10 single-use codes, shown
once. Save them.

### `POST /api/auth/totp/disable`
Body: `{"password":"..."}`. Disables TOTP and clears the secret and backup
codes.

---

## API Tokens 🔒

Long-lived `gv_...` Bearer tokens for automation. Plaintext is shown only
once at creation; only a bcrypt hash is stored.

### `GET /api/auth/tokens`
Lists tokens: `[{"id":1,"name":"backup-script","createdAt":"...","lastUsedAt":"...","expiresAt":"..."}]`.

### `POST /api/auth/tokens`
Body: `{"name":"backup-script","expiresIn":"720h"}` (`expiresIn` optional;
Go duration string, empty = never expires).
Returns `{"id":1,"name":"...","token":"gv_..."}` — **copy the token now**.

```bash
curl -s -X POST http://localhost:8080/api/auth/tokens \
  -H "Authorization: Bearer gv_EXISTING_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"my-script","expiresIn":"720h"}'
```

### `DELETE /api/auth/tokens/{id}`
Revokes a token immediately.

---

## Setup 🔒

### `POST /api/setup/validate`
Body: AWS credentials. Validates them against AWS before deploying.

### `POST /api/setup/deploy`
Body: full setup config (buckets, region, credentials, CloudFront options).
Provisions infrastructure. Long-running.

### `GET /api/setup/status`
Returns setup/deployment progress.

---

## Backups 🔒

### `GET /api/backups`
List backup jobs (scheduled backup definitions).

### `POST /api/backups`
Create a backup definition. Body includes paths, schedule, retention.

### `GET /api/backups/{id}`
Get one backup definition.

### `PUT /api/backups/{id}`
Update a backup definition.

### `DELETE /api/backups/{id}`
Delete a backup definition.

### `POST /api/backups/{id}/run`
Trigger a backup run now. Returns the job id.

---

## Jobs 🔒

### `GET /api/jobs`
List jobs (backup runs, restores, etc.) with status.

### `GET /api/jobs/{id}`
Get one job's details and status.

### `GET /api/jobs/{id}/stream`
Server-sent events stream of job log output. `Content-Type: text/event-stream`.

```bash
curl -N http://localhost:8080/api/jobs/42/stream \
  -H "Authorization: Bearer gv_TOKEN"
```

### `POST /api/jobs/{id}/cancel`
Cancel a running job.

---

## Snapshots 🔒

### `GET /api/snapshots`
List snapshots.

### `GET /api/snapshots/{id}`
Get one snapshot's metadata.

### `GET /api/snapshots/{id}/files?path=/`
Browse files inside a snapshot. `path` query param selects the directory.

### `DELETE /api/snapshots/{id}`
Delete a snapshot (`rustic forget`; optional prune).

---

## Storage 🔒

### `GET /api/storage`
Storage summary: total bytes, pack/index bytes, snapshot counts.

### `POST /api/repo/prune`
Prune unreferenced data from the repository. Long-running.

---

## Restores 🔒

### `POST /api/restores`
Start a restore. Body: `{"snapshotId":1,"paths":["/home/user/docs"],"destination":"/restore-target"}`.

### `GET /api/restores`
List restore jobs.

### `GET /api/restores/{id}`
Get one restore job's status.

### `GET /api/restores/tuning`
Get warm-up tuning: `{"downloadGbPerDay":1080}`.

### `PUT /api/restores/tuning`
Body: `{"downloadGbPerDay":1080}`. Controls Glacier warm-up planning rate.

---

## Catalog 🔒

### `POST /api/catalog/rebuild`
Re-index snapshots from the hot repository. Background job. Run if snapshots
are missing after a disruption.

---

## CloudFront (free-egress restores) 🔒

### `GET /api/settings/cloudfront`
Status: `{"enabled":true,"provisioning":false,"domain":"...","proxyReady":true,"lastError":""}`.

### `POST /api/settings/cloudfront/enable`
Body: `{"accessKey":"...","secretKey":"..."}`. Provisions the distribution.
Takes a few minutes; poll the status endpoint.

### `POST /api/settings/cloudfront/disable`
Disables the free-egress path. Restores fall back to paid S3 egress.

---

## IAM — snapshot-delete permission 🔒

### `GET /api/settings/iam/snapshot-delete`
Returns `{"granted":true|false}`.

### `POST /api/settings/iam/snapshot-delete`
Body: `{"accessKey":"...","secretKey":"..."}`. Grants the IAM permission
needed for snapshot deletion.

### `POST /api/settings/iam/snapshot-delete/revoke`
Revokes the permission.

---

## Notifications 🔒

### `GET /api/notifications/config`
Returns `{"destinations":["discord://..."],"notifyBackupCompleted":true,"notifyWarmupCompleted":true,"notifyRestoreCompleted":true}`.
Destinations are [apprise](https://github.com/caronc/apprise) URLs.

### `PUT /api/notifications/config`
Save the config (same shape as GET).

### `POST /api/notifications/test`
Body: `{"destinations":["ntfy://ntfy.sh/my-topic"]}`. Sends a test notification.

---

## Recovery 🔒

### `GET /api/recovery/package`
Downloads the recovery package (JSON attachment): bucket names, region, and
recovery instructions. Store it somewhere safe — it's everything needed to
restore data without this appliance.

```bash
curl -OJ http://localhost:8080/api/recovery/package \
  -H "Authorization: Bearer gv_TOKEN"
```
