# Hybrid Cold Storage Architecture

## Overview

GlacierVault uses a hybrid architecture that combines Rustic's official cold-storage repository design with a locally maintained metadata catalog.

The goal is to achieve:

* Lowest possible AWS storage costs
* Fast user interface responsiveness
* Instant snapshot browsing
* Minimal AWS API usage
* Full compatibility with Rustic's official Glacier workflow

The application should never require Glacier retrieval operations merely to browse backups.

Users should be able to view backup history, file listings, and snapshot details immediately, even when all backup data resides in Glacier Deep Archive.

---

# Architecture

## AWS Layer

The AWS infrastructure is provisioned using the rustic-aws CDK project.

Components include:

```text
AWS
├─ Hot Repository Bucket
├─ Cold Repository Bucket
├─ SQS Queue
├─ Lambda Functions
├─ Glacier Restore Notifications
└─ Warmup Infrastructure
```

### Hot Repository

Stores repository metadata required for normal backup operations.

Contents:

```text
config
snapshots
indexes
trees
keys
```

Characteristics:

* Fast access
* Low storage volume
* Supports snapshot discovery
* Supports backup operations

---

### Cold Repository

Stores backup pack data.

Contents:

```text
data packs
```

Characteristics:

* Glacier Deep Archive
* Extremely low storage cost
* Retrieval required before restore

---

# Local Metadata Catalog

## Purpose

The Metadata Catalog provides a complete local index of backup contents.

The catalog exists solely to improve user experience.

It is not the authoritative source of backup data.

The Rustic repositories remain the source of truth.

---

## Benefits

Without a local catalog:

```text
User Opens UI
    ↓
Query AWS
    ↓
Query Repository
    ↓
Display Snapshots
```

With a local catalog:

```text
User Opens UI
    ↓
Read SQLite
    ↓
Display Snapshots Immediately
```

This eliminates unnecessary cloud operations and allows the application to feel responsive despite using archival storage.

---

# Metadata Synchronization

## After Backup Completion

After every successful backup:

```text
Rustic Backup
    ↓
Rustic Snapshot Enumeration
    ↓
Metadata Extraction
    ↓
Catalog Update
```

The application executes:

```bash
rustic snapshots --json
```

and updates the catalog.

Optionally:

```bash
rustic ls <snapshot> --json
```

may be used to populate detailed file indexes.

---

## Catalog Contents

### Snapshots

Stores:

* Snapshot ID
* Backup date
* Hostname
* Tags
* Backup source
* Total size
* File count

---

### File Index

Stores:

* Path
* Size
* Modification time
* Snapshot ID
* Directory hierarchy

This enables browsing backup contents without contacting AWS.

---

### Backup Jobs

Stores:

* Job ID
* Schedule
* Status
* Runtime
* Data transferred

---

### Restore Jobs

Stores:

* Requested files
* Warmup status
* Retrieval progress
* Restore destination
* Completion status

---

# Restore Workflow

## User Experience

The user can browse backups immediately.

Example:

```text
Backups
├─ Server A
│   ├─ June 1
│   ├─ June 2
│   └─ June 3
└─ Server B
    ├─ June 2
    └─ June 4
```

Selecting a snapshot displays file contents from the local catalog.

No AWS requests are required.

---

## Restore Execution

Once the user initiates a restore:

```text
Select Snapshot
    ↓
Select Files
    ↓
Select Destination
    ↓
Start Restore
```

The application:

```text
Lookup Snapshot
    ↓
Determine Required Packs
    ↓
Execute Warmup
    ↓
Submit Glacier Retrieval Requests
    ↓
Wait for Retrieval Completion
    ↓
Execute Rustic Restore
```

Only this stage interacts with Glacier storage.

---

# Catalog Rebuild

The metadata catalog is considered disposable.

If the catalog becomes corrupted or lost:

```text
Restore Catalog Database
       OR
Rebuild Catalog
```

Rebuild process:

```text
Enumerate Repository
    ↓
Read Snapshots
    ↓
Read File Trees
    ↓
Repopulate SQLite
```

No backup data is lost because the repository remains authoritative.

---

# Design Principles

## Repository Is Source Of Truth

The catalog is a cache.

All backup integrity depends on Rustic repositories, not the local database.

---

## Fast UI

Browsing backups should never trigger Glacier retrievals.

Users should experience sub-second navigation for:

* Snapshot lists
* Backup history
* File browsing
* Search

---

## Minimize AWS Costs

The application should avoid unnecessary:

* S3 GET requests
* Glacier retrieval requests
* Repository scans

All routine UI operations should use the local catalog.

---

## Graceful Disaster Recovery

A complete system recovery should be possible using only:

* AWS account access
* Repository password
* Rustic tooling

The application database should never be required to recover data.
