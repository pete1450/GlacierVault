# GlacierVault

## Overview

GlacierVault is a self-hosted backup appliance that provides a simple web interface for configuring and operating AWS Glacier Deep Archive backups using Rustic and rustic-aws cold stoage infrastructure setup- https://github.com/rustic-rs/rustic-aws/tree/main/glacier-cold-storage-cdk

The primary goal is to make AWS Deep Archive backups accessible to non-AWS experts while preserving the extremely low storage costs and disaster recovery capabilities of Glacier Deep Archive.

Users should be able to:

* Deploy a single Docker container
* Enter AWS credentials
* Click "Setup AWS"
* Select folders to back up
* Configure backup schedules
* Restore data with a button click

without needing to understand AWS CDK, S3 lifecycle rules, Glacier retrieval jobs, SQS, or Rustic internals.

---

# Goals

## Functional Goals

* One-click AWS infrastructure deployment
* Scheduled backups
* Incremental backups
* Encrypted backups
* Append-only repositories
* Glacier Deep Archive storage
* Point-in-time restore capability
* Restore progress tracking
* Multi-folder backup support

## Non-Goals

* Real-time synchronization
* Multi-user access control
* Enterprise backup features
* Cloud provider support beyond AWS (initial release)

---

# Architecture

## Components

### Web UI

Provides:

* Initial setup wizard
* AWS configuration
* Backup source management
* Schedule management
* Snapshot browsing
* Restore management
* Job monitoring

Technology:

* React
* Next.js

---

### API Service

Responsibilities:

* User authentication
* AWS provisioning orchestration
* Backup scheduling
* Restore scheduling
* Snapshot indexing
* Job state management

Technology:

* Go preferred
* Rust acceptable

---

### Backup Engine

Wraps Rustic commands.

Responsibilities:

* Initialize repositories
* Execute backups
* Execute snapshot listing
* Execute restores
* Execute warmup operations

Runs Rustic as subprocesses.

Example:

rustic backup /data/photos

rustic snapshots

rustic restore

---

### AWS Provisioning Service
https://github.com/rustic-rs/rustic-aws/tree/main/glacier-cold-storage-cdk

Responsible for deploying rustic-aws infrastructure.

Workflow:

1. Validate credentials
2. Download or bundle CDK assets
3. Bootstrap AWS account
4. Deploy stacks
5. Store generated resource identifiers

Resources created:

* S3 buckets
* Deep Archive storage
* SQS queues
* IAM roles
* Lambda functions
* Notification rules

---

### Scheduler

Responsible for executing backup jobs.

Technology:

* Cron parser
* Internal scheduler

Examples:

0 2 * * *

0 */6 * * *

---

### Metadata Database

Stores:

* AWS configuration
* Backup definitions
* Job history
* Snapshot catalog
* Restore requests

Technology:
SQLite

---

# Docker Architecture

Single-container deployment.

Container includes:

* Web UI
* API
* Scheduler
* Rustic binary
* Warmup utility
* AWS CLI
* Node runtime
* CDK runtime

Persistent volumes:

/config

/database

/cache

/logs

User specified volumes:

Mounts to host that are the folders which need to be backed up

---

# Setup Flow

## Initial Wizard

Step 1

Enter:

* AWS Access Key
* AWS Secret Key
* Region

Step 2

Validate credentials.

Step 3

Display estimated AWS resources.

Step 4

User clicks:

Deploy Infrastructure

Step 5

Application executes:

cdk bootstrap

cdk deploy

Step 6

Store generated resource information.

Step 7

Initialize Rustic repositories.

Setup complete.

---

# Backup Configuration

Fields:

Name

Source Paths

Examples:

/mnt/media

/home

/docker

Schedule

Examples:

Daily

Weekly

Custom Cron

Retention Label

Examples:

Critical

Archive

Personal

Compression Level

Encryption Password

---

# Backup Execution

Workflow:

Scheduler triggers job.

Application executes:

rustic backup

Progress streamed to UI.

Job history recorded.

Snapshot metadata refreshed.

Notification generated.

---

# Restore Workflow

## User Flow

Select Backup

Select Snapshot

Select Destination Folder

Click Restore

---

## System Flow

1. Determine required packs

2. Execute warmup utility

3. Submit Glacier retrieval requests

4. Monitor retrieval status

5. Wait for archive availability

6. Execute Rustic restore

7. Stream progress to UI

8. Mark restore complete

---

# Restore States

Queued

Warmup Requested

Retrieval In Progress

Retrieval Complete

Restoring

Completed

Failed

---

# Security Model

AWS credentials encrypted at rest.

Encryption:

AES-256

Secrets stored separately from application configuration.

Optional integration:

Docker Secrets

Hashicorp Vault

AWS Secrets Manager

---

# Disaster Recovery

User must export:

* Encryption password
* Recovery key
* AWS account information

Application provides:

Generate Recovery Package

Contains:

* Repository configuration
* Bucket identifiers
* Restore instructions

No backup should depend solely on the application itself.

---

# Future Enhancements

## Multi-Machine Agent Support

Install lightweight agents.

Central UI manages backups across multiple hosts.

---

## Backup Verification

Periodic automated restore tests.

---

# MVP Scope

Version 1.0 includes:

* Single Docker container
* AWS setup wizard
* CDK deployment
* Folder backups
* Cron scheduling
* Snapshot browsing
* Restore workflow
* Job history
* SQLite database

Everything else deferred until after initial release.
