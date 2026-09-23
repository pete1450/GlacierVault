# GlacierVault Documentation

GlacierVault is a self-hosted backup appliance that makes AWS Glacier Deep Archive
as simple as possible: you bring AWS credentials, it handles infrastructure,
scheduling, encryption, and restores through a web UI.

Start here, then follow the guides in order:

| Document | What it covers |
|---|---|
| [Goals](goals.md) | Why this project exists, what it optimizes for, and what it explicitly does *not* try to do |
| [Cost Guide](costs.md) | Real AWS pricing (verified Sept 2026), worked cost examples for documents, photo archives, and VM backups, and full vs. partial restore costs with breakdowns |
| [Design](design.md) | Design theory (why Deep Archive, why big packs, why Bulk) and how it is implemented (components, data flows, the restore warmup sequence) |
| [Setup](setup.md) | Prerequisites, creating an IAM user with the right permissions, Docker deployment, and the setup wizard |
| [User Workflow](workflow.md) | End-to-end usage: setup → create backup → monitor snapshots → restore → prune → disaster recovery |
| [Page Overview](pages.md) | What every page in the UI does, section by section |

Related reading already in the repo root:

- [`../design.md`](../design.md) — early design notes
- [`../cold-storage-arch.md`](../cold-storage-arch.md) — hybrid hot/cold storage architecture notes

> **Status:** GlacierVault is a work in progress. The restore path, snapshot
> management, and pruning have been exercised, but a full end-to-end Deep
> Archive restore at scale has limited verification. Do not rely on it as your
> only backup yet — keep a second copy of anything irreplaceable until you have
> personally tested a restore.
