// Shared restore-cost estimation logic, based on docs/costs.md pricing.
// Prices: AWS us-east-1, as of September 2026.

export const PRICES_AS_OF = 'September 2026'
export const PRICES_REGION = 'us-east-1'

export const DEEP_ARCHIVE_GB_MONTH = 0.00099
export const STANDARD_GB_MONTH = 0.023
export const BULK_RETRIEVAL_GB = 0.0025
export const BULK_RETRIEVAL_PER_1K_REQ = 0.025
export const BATCH_JOB = 0.25
export const BATCH_PER_M_OBJECTS = 1.0
export const CF_FREE_TB_MONTH = 1
export const CF_OVERAGE_GB = 0.085
export const PACK_BYTES = 512 * 1024 * 1024
export const WARMUP_BATCH_SIZE = 1000
export const DOWNLOAD_GB_PER_DAY = 1080

export function formatUSD(n: number): string {
  if (n < 0.01 && n > 0) return '<$0.01'
  return `$${n.toFixed(2)}`
}

export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

export interface CostLineItem {
  label: string
  detail: string
  cost: number
}

export interface RestoreScenario {
  title: string
  subtitle: string
  packs: number
  bytes: number
  batches: number
}

function avgExpiryDays(batches: number, totalGB: number): number {
  if (batches <= 1) return 1
  const dl = Math.ceil(totalGB / DOWNLOAD_GB_PER_DAY)
  let sum = 0
  for (let k = 1; k <= batches; k++) sum += 2 * (batches - k) + dl + 1
  return sum / batches
}

/** Build a RestoreScenario for an arbitrary byte count. */
export function scenarioForBytes(title: string, subtitle: string, bytes: number): RestoreScenario {
  const packs = Math.max(1, Math.ceil(bytes / PACK_BYTES))
  return {
    title,
    subtitle,
    packs,
    bytes,
    batches: Math.max(1, Math.ceil(packs / WARMUP_BATCH_SIZE)),
  }
}

/** Itemized cost breakdown for a restore scenario. */
export function restoreCostItems(s: RestoreScenario): CostLineItem[] {
  const gb = s.bytes / 1024 ** 3
  const items: CostLineItem[] = [
    {
      label: 'S3 Batch restore jobs',
      detail: `${s.batches} × $${BATCH_JOB.toFixed(2)}`,
      cost: s.batches * BATCH_JOB,
    },
    {
      label: 'Batch per-object charge',
      detail: `${s.packs.toLocaleString()} objects × $${BATCH_PER_M_OBJECTS.toFixed(2)}/M`,
      cost: (s.packs / 1e6) * BATCH_PER_M_OBJECTS,
    },
    {
      label: 'Bulk retrieval — data',
      detail: `${gb.toFixed(1)} GB × $${BULK_RETRIEVAL_GB}/GB`,
      cost: gb * BULK_RETRIEVAL_GB,
    },
    {
      label: 'Bulk retrieval — requests',
      detail: `${s.packs.toLocaleString()} × $${BULK_RETRIEVAL_PER_1K_REQ}/1k`,
      cost: (s.packs / 1000) * BULK_RETRIEVAL_PER_1K_REQ,
    },
  ]
  const avgDays = avgExpiryDays(s.batches, gb)
  items.push({
    label: 'Thawed copies (S3 Standard)',
    detail: `${gb.toFixed(1)} GB × $${STANDARD_GB_MONTH}/GB-mo × ${avgDays.toFixed(1)}d avg`,
    cost: gb * STANDARD_GB_MONTH * (avgDays / 30),
  })
  const freeGB = CF_FREE_TB_MONTH * 1024
  const overGB = Math.max(0, gb - freeGB)
  items.push({
    label: 'Egress via CloudFront',
    detail: overGB > 0 ? `${overGB.toFixed(0)} GB over free tier × $${CF_OVERAGE_GB}/GB` : 'within 1 TB/mo free tier',
    cost: overGB * CF_OVERAGE_GB,
  })
  return items
}

export function restoreCostTotal(s: RestoreScenario): number {
  return restoreCostItems(s).reduce((a, b) => a + b.cost, 0)
}
