# GlacierVault

A self-hosted backup appliance that makes cheap AWS Glacier Deep Archive as simple as possible: you bring AWS credentials, it handles the rest. One Docker container with a web UI — it deploys the AWS infrastructure with CDK, schedules encrypted [Rustic](https://github.com/rustic-rs/rustic) backups into Deep Archive, and walks restores through Glacier retrieval with a private CloudFront distribution for free-egress downloads and Apprise notifications when jobs finish.

## Built on

- [Rustic](https://github.com/rustic-rs/rustic) — the backup engine (encrypted, deduplicated repositories)
- [glacier-cold-storage-cdk](https://github.com/rustic-rs/rustic-aws/tree/main/glacier-cold-storage-cdk) — the CDK project that provisions the S3/SQS/IAM infrastructure
- [warmup-s3-archives](https://gitlab.com/philipmw/warmup-s3-archives) — restores archived S3 objects via Batch Operations before download
- [Apprise](https://github.com/caronc/apprise) — notification dispatch (Discord, Slack, Telegram, email, ntfy, webhooks, …)

## ⚠️ Work in progress


Thought process
 - Deep archive is dirt-cheap but retrieval can be a pain
 - Bulk retrieval is cheap($2.50 per TB)
 - 1TB per month egress from AWS through Cloudfront is free
 - PUT/GET request need to be managed intelligently 
 - Warmup process takes a while
 - Wouldn't it be nice to have this all in a single app

---

## THIS VERY MUCH A WORK-IN-PROGRESS REPO!
 - I've tested from backup to restore but it has been limited.
 - Do not rely on this project for your real backups yet!
 - I'd be very open to PRs if anyone wants to dig in.

<img width="934" height="555" alt="image" src="https://github.com/user-attachments/assets/ea0e41c0-9c51-4071-b240-ac97daf67864" />
<img width="1053" height="926" alt="image" src="https://github.com/user-attachments/assets/2a25e9a7-2b83-4184-8159-2c7a3614b07d" />

## Quick start

1. **Create a setup IAM user** in the AWS console (it deploys real infrastructure, so it needs broad permissions — use once, then delete it). See [docs/setup.md](docs/setup.md) for the exact policy.
2. **Run the container:**

```yaml
# docker-compose.yml
services:
  glaciervault:
    # Prebuilt image from tagged releases (ghcr.io/pete1450/glaciervault).
    # To build locally instead, see "Building the image yourself" in docs/setup.md.
    image: ghcr.io/pete1450/glaciervault:latest
    ports:
      - "8080:8080"
    environment:
      - INITIAL_PASSWORD=changeme   # web UI login
    volumes:
      - glaciervault-config:/config
      - glaciervault-database:/database
      - glaciervault-cache:/cache
      - glaciervault-logs:/logs
      # Mount the folders you want to back up (read-only):
      - /home:/mnt/home:ro

volumes:
  glaciervault-config:
  glaciervault-database:
  glaciervault-cache:
  glaciervault-logs:
```

```bash
docker compose up -d
```

The compose file pulls the prebuilt image for the latest tagged release. To
build the image yourself, see [docs/setup.md](docs/setup.md).

3. **Open `http://localhost:8080`** and follow the setup wizard: paste your AWS key/secret/region, review the resource estimate, and hit **Deploy Infrastructure**. CDK bootstrap + deploy takes 5–10 minutes, then the app initializes the backup repositories and provisions the CloudFront free-egress path automatically.
4. Add a backup source on the **Backups** page (name, source paths, schedule) and you're done — or press **Run now** for the first backup.

## Docs

Everything else lives in [docs/](docs/):

- [Setup](docs/setup.md) — IAM user creation, wizard walkthrough, teardown
- [User workflow](docs/workflow.md) — backups, snapshots, restores, costs in practice
- [Cost guide](docs/costs.md) — verified AWS pricing with worked examples
- [Design](docs/design.md) — architecture and theory
- [Goals](docs/goals.md) — what this optimizes for (and what it doesn't)
- [Pages](docs/pages.md) — tour of every UI page
