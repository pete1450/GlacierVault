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
   - **Scoped:** attach a custom policy. CDK bootstrap + deploy + the app's
     CloudFront provisioning need the permissions below — no `service:*`
     wildcards, and resources scoped wherever AWS allows it. Replace
     `<account>` with your AWS account ID, `<region>` with your region, and
     `<stack>` with the stack name (default `rustic-cold-backups`).

     ```json
     {
       "Version": "2012-10-17",
       "Statement": [
         {
           "Sid": "STSIdentity",
           "Effect": "Allow",
           "Action": ["sts:GetCallerIdentity"],
           "Resource": "*"
         },
         {
           "Sid": "CloudFormationStacks",
           "Effect": "Allow",
           "Action": [
             "cloudformation:CreateStack",
             "cloudformation:UpdateStack",
             "cloudformation:DeleteStack",
             "cloudformation:DescribeStacks",
             "cloudformation:DescribeStackEvents",
             "cloudformation:DescribeStackResources",
             "cloudformation:GetTemplate",
             "cloudformation:GetTemplateSummary",
             "cloudformation:CreateChangeSet",
             "cloudformation:DescribeChangeSet",
             "cloudformation:ExecuteChangeSet",
             "cloudformation:DeleteChangeSet",
             "cloudformation:ValidateTemplate",
             "cloudformation:ListStacks"
           ],
           "Resource": [
             "arn:aws:cloudformation:<region>:<account>:stack/CDKToolkit/*",
             "arn:aws:cloudformation:<region>:<account>:stack/<stack>/*"
           ]
         },
         {
           "Sid": "IAMCDKBootstrapRoles",
           "Effect": "Allow",
           "Action": [
             "iam:GetRole",
             "iam:CreateRole",
             "iam:DeleteRole",
             "iam:PutRolePolicy",
             "iam:DeleteRolePolicy",
             "iam:AttachRolePolicy",
             "iam:DetachRolePolicy",
             "iam:UpdateAssumeRolePolicy",
             "iam:TagRole",
             "iam:PassRole"
           ],
           "Resource": [
             "arn:aws:iam::<account>:role/cdk-*-deploy-role-<account>-<region>",
             "arn:aws:iam::<account>:role/cdk-*-file-publishing-role-<account>-<region>",
             "arn:aws:iam::<account>:role/cdk-*-image-publishing-role-<account>-<region>",
             "arn:aws:iam::<account>:role/cdk-*-lookup-role-<account>-<region>",
             "arn:aws:iam::<account>:role/cdk-*-cfn-exec-role-<account>-<region>"
           ]
         },
         {
           "Sid": "IAMStackResourcesViaCloudFormation",
           "Effect": "Allow",
           "Action": [
             "iam:CreateUser",
             "iam:DeleteUser",
             "iam:GetUser",
             "iam:PutUserPolicy",
             "iam:DeleteUserPolicy",
             "iam:GetUserPolicy",
             "iam:CreateAccessKey",
             "iam:CreateRole",
             "iam:DeleteRole",
             "iam:GetRole",
             "iam:PutRolePolicy",
             "iam:DeleteRolePolicy",
             "iam:AttachRolePolicy",
             "iam:DetachRolePolicy",
             "iam:PassRole"
           ],
           "Resource": "*",
           "Condition": {
             "StringEquals": {
               "aws:CalledVia": "cloudformation.amazonaws.com"
             }
           }
         },
         {
           "Sid": "S3CDKAssetBucket",
           "Effect": "Allow",
           "Action": [
             "s3:CreateBucket",
             "s3:DeleteBucket",
             "s3:GetBucketLocation",
             "s3:GetBucketPolicy",
             "s3:PutBucketPolicy",
             "s3:GetBucketVersioning",
             "s3:PutBucketVersioning",
             "s3:GetBucketEncryption",
             "s3:PutBucketEncryption",
             "s3:GetBucketPublicAccessBlock",
             "s3:PutBucketPublicAccessBlock",
             "s3:ListBucket",
             "s3:GetObject",
             "s3:PutObject",
             "s3:DeleteObject"
           ],
           "Resource": [
             "arn:aws:s3:::cdk-*-assets-<account>-<region>",
             "arn:aws:s3:::cdk-*-assets-<account>-<region>/*"
           ]
         },
         {
           "Sid": "S3StackBucketsViaCloudFormation",
           "Effect": "Allow",
           "Action": [
             "s3:CreateBucket",
             "s3:DeleteBucket",
             "s3:GetBucketLocation",
             "s3:GetBucketPolicy",
             "s3:PutBucketPolicy",
             "s3:GetBucketVersioning",
             "s3:PutBucketVersioning",
             "s3:GetBucketEncryption",
             "s3:PutBucketEncryption",
             "s3:ListBucket",
             "s3:GetObject",
             "s3:PutObject",
             "s3:DeleteObject"
           ],
           "Resource": "arn:aws:s3:::<stack>-*",
           "Condition": {
             "StringEquals": {
               "aws:CalledVia": "cloudformation.amazonaws.com"
             }
           }
         },
         {
           "Sid": "S3ColdBucketPolicyForCloudFront",
           "Effect": "Allow",
           "Action": ["s3:GetBucketPolicy", "s3:PutBucketPolicy"],
           "Resource": "arn:aws:s3:::<stack>-*"
         },
         {
           "Sid": "SQSStackQueue",
           "Effect": "Allow",
           "Action": [
             "sqs:CreateQueue",
             "sqs:DeleteQueue",
             "sqs:GetQueueAttributes",
             "sqs:GetQueueUrl",
             "sqs:SetQueueAttributes",
             "sqs:TagQueue",
             "sqs:ListQueues"
           ],
           "Resource": "arn:aws:sqs:<region>:<account>:<stack>-*"
         },
         {
           "Sid": "ECRCDKAssets",
           "Effect": "Allow",
           "Action": [
             "ecr:CreateRepository",
             "ecr:DeleteRepository",
             "ecr:DescribeRepositories",
             "ecr:BatchCheckLayerAvailability",
             "ecr:PutImage",
             "ecr:InitiateLayerUpload",
             "ecr:UploadLayerPart",
             "ecr:CompleteLayerUpload"
           ],
           "Resource": "arn:aws:ecr:<region>:<account>:repository/cdk-*-container-assets-<account>-<region>"
         },
         {
           "Sid": "ECRAuthToken",
           "Effect": "Allow",
           "Action": ["ecr:GetAuthorizationToken"],
           "Resource": "*"
         },
         {
           "Sid": "SSMCDKBootstrap",
           "Effect": "Allow",
           "Action": ["ssm:GetParameter", "ssm:PutParameter", "ssm:DeleteParameter"],
           "Resource": "arn:aws:ssm:<region>:<account>:parameter/cdk-bootstrap/*"
         },
         {
           "Sid": "CloudFrontProvisioning",
           "Effect": "Allow",
           "Action": [
             "cloudfront:CreateDistribution",
             "cloudfront:GetDistribution",
             "cloudfront:UpdateDistribution",
             "cloudfront:DeleteDistribution",
             "cloudfront:CreateOriginAccessControl",
             "cloudfront:GetOriginAccessControl",
             "cloudfront:CreatePublicKey",
             "cloudfront:GetPublicKey",
             "cloudfront:CreateKeyGroup",
             "cloudfront:GetKeyGroup",
             "cloudfront:CreateCachePolicy",
             "cloudfront:GetCachePolicy",
             "cloudfront:ListDistributions"
           ],
           "Resource": "*"
         }
       ]
     }
     ```

     Notes on what couldn't be narrowed further:
     - **`sts:GetCallerIdentity` and `ecr:GetAuthorizationToken`** don't
       support resource-level permissions — `*` is the only option.
     - **CloudFront create actions** (distribution, OAC, key group, cache
       policy, public key) don't support resource-level permissions either.
       Actions are enumerated instead of `cloudfront:*`, which is the best
       AWS allows.
     - **IAM/S3 calls made by CloudFormation itself** (creating the stack's
       user, batch role, and buckets, whose names CDK generates) are gated
       by the `aws:CalledVia: cloudformation.amazonaws.com` condition — the
       credential can only make those calls when CloudFormation is the
       caller, not directly.
     - **CDK-generated names** start with the stack name, so `<stack>-*`
       matches the stack's buckets and queue. If you renamed the stack,
       update the prefix.
     - If your bootstrap is already in place and you never use container
       assets, the two `ECR*` statements can be dropped.
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
- For SSO via your own identity provider, see [Authentication](auth.md).
- For scripting and automation, see the [API reference](api.md).

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
   double-check the key pair and region. This screen also has a
   **Read-only snapshots** checkbox (unchecked by default): check it to keep
   the infrastructure append-only — the backup user won't get
   `s3:DeleteObject`, so snapshots can't be deleted afterward. Leave it
   unchecked to allow snapshot deletion (recommended for most people).
   Either choice can be changed later in **Settings → Snapshot deletion
   permission**.
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
