# GlacierVault

A self-hosted backup appliance that makes AWS Glacier Deep Archive as simple as possible: you bring AWS credentials, it handles the rest. One Docker container with a web UI — it deploys the AWS infrastructure with CDK, schedules encrypted [Rustic](https://github.com/rustic-rs/rustic) backups into Deep Archive, and walks restores through Glacier retrieval (with a private CloudFront distribution for free-egress downloads and Apprise notifications when jobs finish).

## ⚠️ Work in progress

This is very much a work-in-progress repo. Effort went into a robust design, but very little of the output has been verified end to end — restores in particular are still being proven out. **Do not rely on this project for your real backups yet.** Open to PRs if anyone wants to dig in.

<img width="934" height="555" alt="image" src="https://github.com/user-attachments/assets/ea0e41c0-9c51-4071-b240-ac97daf67864" />
<img width="1053" height="926" alt="image" src="https://github.com/user-attachments/assets/2a25e9a7-2b83-4184-8159-2c7a3614b07d" />

## Quick start

1. **Create a setup IAM user** in the AWS console (it deploys real infrastructure, so it needs broad permissions — use once, then delete it). See [docs/setup.md](docs/setup.md) for the exact policy.
2. **Run the container:**

```yaml
# docker-compose.yml
services:
  glaciervault:
    image: glaciervault:latest
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
docker compose up -d --build
```

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
