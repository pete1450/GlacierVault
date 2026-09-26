# Cost Guide

How much GlacierVault actually costs on AWS, with worked examples and full
breakdowns. Prices are for **us-east-1 (N. Virginia)**, verified September 2026
against the [AWS S3 pricing page](https://aws.amazon.com/s3/pricing/). Other
regions differ by roughly 5–30%. AWS changes prices occasionally — re-check
before budgeting at scale.

## The price list that matters

### Storage

| Where | Storage class | $/GB-month | Notes |
|---|---|---|---|
| Cold bucket (your data) | Glacier Deep Archive | **$0.00099** | ~$1.01/TB-month. 180-day minimum. |
| Hot bucket (metadata) | S3 Standard | $0.023 | Holds config, snapshots, indexes, trees, keys. Tiny compared to data. |
| Batch manifest/report buckets | S3 Standard | $0.023 | Manifests are kilobytes; effectively $0. |

### Requests (upload side)

| Operation | $/1,000 requests |
|---|---|
| PUT/COPY/POST/LIST to Deep Archive | **$0.05** |
| PUT to S3 Standard (hot bucket) | $0.005 |
| GET | $0.0004 |

### Retrieval (restore side)

| Tier | Data | Requests | Typical time |
|---|---|---|---|
| **Bulk** (GlacierVault default) | **$0.0025/GB** | **$0.025/1,000** | up to 48 h |
| Standard | $0.02/GB | $0.10/1,000 | ~12 h |

### S3 Batch Operations (used by every restore)

| Item | Price |
|---|---|
| Per job | **$0.25** |
| Per million objects processed | **$1.00** |

Every GlacierVault restore submits one S3 Batch Operations restore job, so
every restore costs **at least ~$0.25** before a single byte is retrieved.

### Data transfer out (egress)

GlacierVault routes restore downloads through a private CloudFront
distribution (signed URLs minted inside the appliance), so egress consumes
**CloudFront's free data-transfer allowance** instead of paid S3 egress:

| Volume | Price |
|---|---|
| First 1 TB/month (all customers) | **free** |
| 10 million HTTP/HTTPS requests/month | **free** |
| Beyond that | ~$0.085/GB (US/Europe) |

Notes:

- The 1 TB allowance is **account-wide and monthly**: it is shared with any
  other CloudFront traffic in the account and resets each billing month.
- Transfer from the S3 origin to CloudFront is free; there is no fixed
  monthly charge for the distribution itself.
- Backups (uploads) don't touch CloudFront at all — transfer *in* is free
  regardless.

Egress used to be the largest line item on a big restore. With the
free-egress path, restores up to 1 TB in a month normally cost **$0** in
transfer.

### The two Deep Archive fine-print items

1. **180-day minimum storage duration.** Delete (or prune) data that has been
   stored fewer than 180 days and you are billed for the remainder anyway.
   Don't vault data you might throw away next month.
2. **~40 KB billable overhead per object.** AWS adds ~40 KB of metadata per
   archived object (and bills objects smaller than 40 KB *as* 40 KB). A million
   tiny files stored as a million objects wastes ~40 GB of billed storage and
   $50 in PUTs. This is the entire reason GlacierVault packs everything into
   **512 MiB data packs**: 1 TB becomes ~2,048 objects instead of potentially
   millions.

## Worked examples

Assumptions: us-east-1, Bulk retrieval, restored copies kept 1 day (the app
default; the AWS minimum), egress counted against CloudFront's 1 TB/month free data-transfer
allowance (account-wide; assumed otherwise unused here).

### Example 1 — Documents: 10 GB, ~50,000 small files

**Backing up (one-time):**

| Item | Math | Cost |
|---|---|---|
| Data packs | 10 GB ÷ 512 MiB ≈ 20 objects → 20 PUTs × $0.05/1K | $0.001 |
| Per-object overhead | 20 × 40 KB ≈ 0.8 MB | ~$0.00 |
| **Total upload** | | **≈ $0.00** |

**Storing:** 10 GB × $0.00099 = **$0.0099/month (~$0.12/year)**

> Without packing: 50,000 objects × 40 KB = ~2 GB of billed overhead, and
> 50,000 PUTs = **$2.50** just to upload. Packing saves ~$2.50 on day one.

**Full restore (Bulk):**

| Item | Math | Cost |
|---|---|---|
| Retrieval data | 10 GB × $0.0025 | $0.025 |
| Retrieval requests | 20 × $0.025/1K | ~$0.00 |
| Batch job | $0.25 + (20 objects → ~$0.00) | $0.25 |
| Egress | 10 GB, inside the 1 TB CloudFront allowance | $0.00 |
| **Total** | | **≈ $0.28** |

### Example 2 — Family photo archive: 500 GB, ~40,000 photos

**Backing up (one-time):**

| Item | Math | Cost |
|---|---|---|
| Data packs | 500 GB ÷ 512 MiB ≈ 1,000 objects → 1,000 PUTs × $0.05/1K | $0.05 |
| **Total upload** | | **≈ $0.05** |

**Storing:** 500 GB × $0.00099 = **$0.50/month (~$5.94/year)**

Incremental backups only upload changed data (rustic dedplicates), so the
ongoing cost is a few packs per run — typically cents.

**Full restore (Bulk):**

| Item | Math | Cost |
|---|---|---|
| Retrieval data | 500 GB × $0.0025 | $1.25 |
| Retrieval requests | 1,000 × $0.025/1K | $0.025 |
| Batch job | $0.25 + (1,000 objects → $0.001) | $0.251 |
| Egress | 500 GB, inside the 1 TB CloudFront allowance | **$0.00** |
| **Total** | | **≈ $1.53** |

Note how different this used to be: without the free-egress path, 500 GB of
S3 egress was ~$36 and dominated the restore. Now the whole restore is the
$1.53 of Glacier retrieval and request fees.

**Partial restore — one 25 MB photo (Bulk):**

Rustic only needs the pack(s) containing that photo. With 512 MiB packs, a
single-file restore warms whole 512 MiB packs — coarse granularity is the
trade-off for cheap storage (see [Design](design.md)).

| Item | Math | Cost |
|---|---|---|
| Retrieval data | 0.5 GB × $0.0025 | $0.001 |
| Batch job | $0.25 (one job per restore) | $0.25 |
| Egress | 0.5 GB, inside the CloudFront allowance | $0.00 |
| **Total** | | **≈ $0.25** |

Takeaway: partial restores are cheap in bytes, but each restore is a new
batch job — **batch up small restores** instead of doing them one at a time.

### Example 3 — VM backups: 2 TB (four 500 GB VMs)

**Backing up (initial):**

| Item | Math | Cost |
|---|---|---|
| Data packs | 2,048 GB ÷ 512 MiB ≈ 4,096 objects → 4,096 PUTs × $0.05/1K | $0.20 |
| **Total upload** | | **≈ $0.20** |

**Storing:** 2,048 GB × $0.00099 = **$2.03/month (~$24.33/year)**

With 5% monthly churn, each month adds ~100 GB → ~+$0.10/month storage and
~$0.01 in PUTs. Compression (adjustable 1–22 per backup definition) usually
shrinks VM images noticeably before they ever hit S3.

**Full restore (Bulk):**

| Item | Math | Cost |
|---|---|---|
| Retrieval data | 2,048 GB × $0.0025 | $5.12 |
| Retrieval requests | 4,096 × $0.025/1K | $0.10 |
| Batch job | $0.25 + (4,096 objects → $0.004) | $0.254 |
| Egress | first 1,024 GB free; remaining 1,024 GB × ~$0.085 | **≈ $87.04** |
| **Total** | | **≈ $92.51** |

The 1 TB free allowance covers the first half; only the second ~1 TB pays
egress. (Without the free-egress path this restore was ~$181, almost all of
it transfer.) A 2 TB restore could approach $0 egress only if the downloads
were deliberately split across billing months — the app restores in one shot,
so budget the overage above.

**Same restore, Standard tier (urgent, ~12 h instead of ~48 h):**

| Item | Math | Cost |
|---|---|---|
| Retrieval data | 2,048 GB × $0.02 (8× Bulk) | $40.96 |
| Retrieval requests | 4,096 × $0.10/1K | $0.41 |
| Batch job + egress | $0.254 + ~$87.04 | $87.29 |
| **Total** | | **≈ $128.66** |

Standard tier costs ~8× more on the retrieval line and only buys speed —
egress is unchanged. GlacierVault defaults to Bulk; Standard is not currently
exposed in the UI.

### The always-on "idle" cost

Even with no restores, you pay for:

| Item | Typical size | $/month |
|---|---|---|
| Cold bucket (your data) | whatever you store × $0.00099 | see above |
| Hot bucket (metadata) | a few GB for a TB-scale repo × $0.023 | <$0.10 |
| Batch manifest/report buckets | kilobytes | $0.00 |
| SQS queue | first 1M requests/month free | $0.00 |
| CloudFront distribution | no fixed charge; requests inside the free tier | $0.00 |

A 1 TB vault idles at roughly **$1.10/month**.

## Rules of thumb

1. **Egress is free up to 1 TB/month per account** via the CloudFront path.
   Only restores (or other CloudFront traffic) beyond that pay ~$0.085/GB.
   The allowance resets monthly and is shared with any other CloudFront use
   in the account.
2. **Big packs win twice.** 512 MiB packs minimize both PUT costs at backup
   time and request counts at restore time. The cost is coarser partial
   restores (a 1 KB file still thaws its whole 512 MiB pack).
3. **Every restore costs ≥ ~$0.25** (one batch job). Group small restores.
4. **Bulk is 8× cheaper than Standard** on retrieval. Unless a restore is
   genuinely urgent, wait the up-to-48 hours.
5. **Don't vault short-lived data.** The 180-day minimum means deleting early
   still bills you. The UI warns before pruning for exactly this reason.
6. **Incrementals are cheap.** Rustic deduplicates by content, so scheduled
   backups only upload changed chunks — a daily backup of a quiet file server
   is a handful of packs.

## What GlacierVault does *not* charge you for

- Browsing snapshots, listing files, viewing the storage summary — all served
  from the local catalog and the hot bucket. **$0 in retrieval fees.**
- Checking restore status, streaming job logs — local database. **$0.**
- The SQS notifications the warmup tool listens on — inside the free tier. **$0.**
