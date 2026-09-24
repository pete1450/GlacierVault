# Setup

## What you need

- An **AWS account** (with access to create IAM users).
- **Docker** (Engine 24+ or Docker Desktop) and Docker Compose.
- A machine that stays on — this is a backup *appliance*, so it needs to be
  running when your schedules fire. A homelab server or always-on mini PC is
  ideal.
- About 10 minutes, plus 5–10 for the infrastructure deploy.

## 1. Create an IAM user for setup

GlacierVault's setup wizard deploys real AWS infrastructure with CDK, so the
credentials you paste into it need permission to create buckets, queues,
roles, users, and a CloudFormation stack. These credentials are used **once**,
at setup; afterward the app switches to a limited IAM user it creates for
daily operation.

In the AWS Console:

1. Go to **IAM → Users → Create user**. Name it something like
   `glaciervault-setup`. (No console access needed.)
2. On the permissions step, you have two options:
   - **Simple:** attach the AWS-managed `AdministratorAccess` policy. Easiest,
     and safe enough for a credential you will use once and can delete after
     setup completes.
   - **Scoped:** attach a custom policy with roughly these permissions
     (CDK bootstrap + deploy needs all of them):

     ```json
     {
       "Version": "2012-10-17",
       "Statement": [{
         "Effect": "Allow",
         "Action": [
           "cloudformation:*",
           "iam:*",
           "s3:*",
           "sqs:*",
           "cloudfront:*",
           "sts:GetCallerIdentity",
           "ecr:*",
           "ssm:PutParameter",
           "ssm:GetParameter"
         ],
         "Resource": "*"
       }]
     }
     ```

     > In practice CDK touches IAM, S3, SQS, and CloudFormation. The `ecr:*`
     > entry covers CDK asset publishing if your bootstrap uses it; omit it
     > if your bootstrap is already in place. `cloudfront:*` is needed for
     > the free-egress distribution the app provisions after the CDK deploy.
     > `ssm:PutParameter` / `ssm:GetParameter` are for the `/cdk-bootstrap/…`
     > version key that `cdk bootstrap` writes — without them bootstrap fails.
     >
     > Be honest with yourself about what this policy means: `iam:*` lets the
     > holder create roles and attach `AdministratorAccess` to them (CDK
     > bootstrap itself does exactly that for its deploy role), so a
     > determined holder of these credentials can reach full admin regardless
     > of how you scope the rest. The scoped policy limits accidental blast
     > radius; the real protection is using the credential once and deleting
     > the user (or at least its access key) right after setup — see below.
     > `Resource: "*"` is the practical minimum because CDK generates bucket,
     > role, and policy names at deploy time.
3. Finish user creation, then open the user → **Security credentials →
   Create access key** → use case "Command Line Interface (CLI)". Copy the
   **Access key ID** and **Secret access key** — the secret is shown once.

> **After setup succeeds**, you can delete this IAM user (or at least delete
> its access key). The appliance provisions its own limited `rustic-iam-user`
> and never needs the setup credentials again.

## 2. Deploy the container

```yaml
# docker-compose.yml
services:
  glaciervault:
    # Prebuilt image from tagged releases (ghcr.io/pete1450/glaciervault).
    image: ghcr.io/pete1450/glaciervault:latest
    ports:
      - "8080:8080"
    environment:
      # Set your UI login password here (or change it after first boot).
      - INITIAL_PASSWORD=<redacted>
    volumes:
      - glaciervault-config:/config
      - glaciervault-database:/database
      - glaciervault-cache:/cache
      - glaciervault-logs:/logs
      # Mount the folders you want to back up (read-only):
      - /home:/mnt/home:ro
      - /srv/media:/mnt/media:ro
```

Notes:

- **Volumes matter.** `/config` holds `rustic.toml` and the generated repo
  password; `/database` holds everything the UI shows. Back these two up
  separately (or download the recovery package — see [User Workflow](workflow.md)).
- **Source paths are container paths.** If you mount `/home` at `/mnt/home`,
  your backup source path is `/mnt/home`, not `/home`. Mount read-only
  (`:ro`) — backups never need write access.
- `INITIAL_PASSWORD` sets the web UI login password. If you omit it, the
  server generates a random one and **prints it in the container log** on
  first boot — check `docker logs` and change it in Settings.

```bash
docker compose up -d
```

Then open `http://<host>:8080`, log in, and you'll be redirected to `/setup`.

### Building the image yourself

Tagged releases (`v*`) are built automatically by
[`.github/workflows/docker-publish.yml`](../.github/workflows/docker-publish.yml)
and published to `ghcr.io/pete1450/glaciervault` (`:latest` plus the version
tags). If you'd rather build locally:

```bash
docker build -f docker/Dockerfile -t glaciervault:latest .
```

then point the compose file at your local image:

```yaml
services:
  glaciervault:
    image: glaciervault:latest
```

## 3. Run the setup wizard

The wizard has four steps:

1. **Credentials.** Paste the Access Key ID, Secret Access Key, region
   (default `us-east-1` — cheapest for most of these services), and a stack
   name (default `rustic-cold-backups`; change it if you run multiple stacks).
2. **Validate.** The app calls STS to confirm the identity and shows a
   resource estimate: 4 S3 buckets, 1 SQS queue, 1 IAM user, 1 IAM role,
   1 CloudFront distribution (free-egress restores). If this fails,
   double-check the key pair and region.
3. **Deploy.** CDK bootstrap (first time only) + `cdk deploy`, with live
   logs streamed to the page. Takes **5–10 minutes**. Don't close the page,
   but if you do, the job record keeps the logs.
4. **Done.** The app creates an access key for the new limited
   `rustic-iam-user` (stored encrypted in the database), writes
   `/config/rustic.toml`, generates the 32-byte repo password, and runs
   `rustic init` with **512 MiB data / 32 MiB tree packs**.
5. **Free-egress provisioning.** Right after the CDK deploy, the app
   provisions a private CloudFront distribution in front of the cold bucket
   (origin access control, signed-URL key group, cache policy) using the
   setup credentials while they are still available. No domain, DNS, or
   certificate setup is needed — it uses the generated
   `*.cloudfront.net` hostname, which is protected by signed URLs. If this
   step fails, setup still completes; you can enable it later from
   Settings (see below).

### What the deploy creates

| Resource | Name pattern | Purpose |
|---|---|---|
| Hot bucket | `<stack>-hotbucket…` | Repo metadata (S3 Standard) |
| Cold bucket | `<stack>-coldbucket…` | Data packs (Deep Archive) |
| Batch-manifests bucket | `<stack>-batchmanifests…` | Per-restore key manifests |
| Batch-reports bucket | `<stack>-batchreports…` | Batch job completion reports |
| SQS queue | `<stack>-coldevents…` | Glacier restore notifications |
| IAM user | `rustic-iam-user` | Limited credentials for daily ops |
| IAM role | `<stack>-s3batchrole…` | Assumed by S3 Batch Operations |
| CloudFront distribution | `d…cloudfront.net` | Free-egress restore downloads (signed URLs only) |

Bucket names get a random suffix (global uniqueness). The account ID, bucket
names, queue URL, and resolved role ARN are persisted in the database so
restores can build their warmup configs later.

### Enabling free-egress restores on an existing install

If your appliance was set up before the CloudFront path existed (or the
provisioning step failed), go to **Settings → Free-egress restores
(CloudFront)** and enter a temporary AWS admin access key — the same kind of
credentials used during setup. The app provisions the distribution and then
**discards the credentials**; they are never stored. The signing private key
is generated locally and kept encrypted in the database.

The free allowance is 1 TB/month of CloudFront data transfer per account
(shared with any other CloudFront use); past that, overage is ~$0.085/GB in
US/Europe. See [Costs](costs.md) for worked examples.

## 4. After setup

- **Re-run setup if you upgrade** from an older version and anything looks
  empty (account ID, batch buckets): the wizard backfills the new fields.
  A plain container/image rebuild never requires re-running setup.
- **Delete the setup IAM user** (or its access key) — see step 1.
- **Download the recovery package** from Settings → Recovery and store it
  somewhere safe (password manager, USB stick). It contains bucket names,
  region, and the repo password: everything needed to restore *without*
  this appliance.
- **Change the UI password** in Settings if you used a generated one.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| "Credentials invalid" at validate | Wrong key pair, wrong region, or the key was deactivated. |
| Deploy fails on IAM | Setup user lacks `iam:*` — use AdministratorAccess or the scoped policy above. |
| "No stacks match the name" | Fixed in current versions (stack name is passed as CDK context). Rebuild the image. |
| Setup done but snapshots never appear | Check the backup job's log for a catalog sync error; the error is now surfaced on the job record. |
| `warmup-s3-archives: value does not begin with "arn:"` | Old setup stored the role *name* instead of ARN — fixed in current versions; rebuild the image (no re-deploy needed). |
