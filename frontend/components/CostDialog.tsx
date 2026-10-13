'use client'
import { useEffect } from 'react'

export const PRICES_AS_OF = 'September 2026'
export const PRICES_REGION = 'us-east-1'

// Prices from docs/costs.md (AWS S3 pricing page, us-east-1).
const DEEP_ARCHIVE_GB_MONTH = 0.00099
const STANDARD_GB_MONTH = 0.023
const BULK_RETRIEVAL_GB = 0.0025
const BULK_RETRIEVAL_PER_1K_REQ = 0.025
const BATCH_JOB = 0.25
const BATCH_PER_M_OBJECTS = 1.0
const CF_FREE_TB_MONTH = 1
const CF_OVERAGE_GB = 0.085
const PACK_BYTES = 512 * 1024 * 1024
const WARMUP_BATCH_SIZE = 1000
const DOWNLOAD_GB_PER_DAY = 1080

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

interface LineItem {
  label: string
  detail: string
  cost: number
}

interface Scenario {
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

function scenarioItems(s: Scenario): LineItem[] {
  const gb = s.bytes / 1024 ** 3
  const items: LineItem[] = [
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

export default function CostDialog({
  packBytes,
  totalBytes,
  onClose,
}: {
  packBytes: number
  totalBytes: number
  onClose: () => void
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const coldGB = packBytes / 1024 ** 3
  const hotGB = Math.max(0, (totalBytes - packBytes) / 1024 ** 3)
  const coldMo = coldGB * DEEP_ARCHIVE_GB_MONTH
  const hotMo = hotGB * STANDARD_GB_MONTH

  const packCount = Math.max(1, Math.ceil(packBytes / PACK_BYTES))
  const scenarios: Scenario[] = [
    {
      title: 'One small file',
      subtitle: `warms its whole ${formatBytes(PACK_BYTES)} pack`,
      packs: 1,
      bytes: Math.min(PACK_BYTES, packBytes || PACK_BYTES),
      batches: 1,
    },
    {
      title: 'Half the repository',
      subtitle: `${Math.ceil(packCount / 2).toLocaleString()} of ${packCount.toLocaleString()} packs`,
      packs: Math.ceil(packCount / 2),
      bytes: packBytes / 2,
      batches: Math.max(1, Math.ceil(Math.ceil(packCount / 2) / WARMUP_BATCH_SIZE)),
    },
    {
      title: 'Whole repository',
      subtitle: `${packCount.toLocaleString()} packs`,
      packs: packCount,
      bytes: packBytes,
      batches: Math.max(1, Math.ceil(packCount / WARMUP_BATCH_SIZE)),
    },
  ]

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-label="Cost breakdown">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative w-full max-w-2xl max-h-[88vh] overflow-y-auto bg-surface border border-line rounded-2xl shadow-2xl">
        <div className="sticky top-0 flex items-center justify-between px-6 py-4 bg-surface/95 backdrop-blur border-b border-line">
          <div>
            <h2 className="text-lg font-bold text-ink">Cost breakdown</h2>
            <p className="text-xs text-muted">
              {PRICES_REGION} · prices as of {PRICES_AS_OF} · Bulk retrieval tier
            </p>
          </div>
          <button onClick={onClose} aria-label="Close" className="p-2 rounded-lg text-muted hover:text-ink hover:bg-raised">
            ✕
          </button>
        </div>

        <div className="px-6 py-5 space-y-7">
          {/* Storage */}
          <section>
            <h3 className="text-sm font-bold uppercase tracking-wider text-faint mb-3">Storage · per month</h3>
            <div className="space-y-2 text-sm">
              <div className="flex justify-between gap-4">
                <span className="text-body">Cold — Deep Archive <span className="text-faint">({coldGB.toFixed(1)} GB × ${DEEP_ARCHIVE_GB_MONTH}/GB)</span></span>
                <span className="font-mono font-semibold">{formatUSD(coldMo)}</span>
              </div>
              <div className="flex justify-between gap-4">
                <span className="text-body">Hot — S3 Standard <span className="text-faint">({hotGB.toFixed(1)} GB × ${STANDARD_GB_MONTH}/GB)</span></span>
                <span className="font-mono font-semibold">{formatUSD(hotMo)}</span>
              </div>
              <div className="flex justify-between gap-4 pt-2 border-t border-line font-bold">
                <span>Estimated monthly storage</span>
                <span className="font-mono text-accent-strong">{formatUSD(coldMo + hotMo)}</span>
              </div>
            </div>
            <p className="text-xs text-faint mt-2">Deep Archive has a 180-day minimum: deleting early still bills the remainder.</p>
          </section>

          {/* Restore scenarios */}
          {scenarios.map(sc => {
            const items = scenarioItems(sc)
            const total = items.reduce((a, b) => a + b.cost, 0)
            return (
              <section key={sc.title}>
                <h3 className="text-sm font-bold uppercase tracking-wider text-faint mb-1">Restore · {sc.title}</h3>
                <p className="text-xs text-muted mb-3">{sc.subtitle}</p>
                <div className="space-y-2 text-sm">
                  {items.map(it => (
                    <div key={it.label} className="flex justify-between gap-4">
                      <span className="text-body">
                        {it.label} <span className="text-faint">({it.detail})</span>
                      </span>
                      <span className="font-mono">{formatUSD(it.cost)}</span>
                    </div>
                  ))}
                  <div className="flex justify-between gap-4 pt-2 border-t border-line font-bold">
                    <span>Estimated restore cost</span>
                    <span className="font-mono text-accent-strong">{formatUSD(total)}</span>
                  </div>
                </div>
              </section>
            )
          })}

          <p className="text-xs text-faint border-t border-line pt-4">
            Estimates assume 512 MiB packs and the Bulk tier (up to 48 h thaw). Egress rides the
            CloudFront free tier (1 TB/mo/account). Thawed S3 copies are billed at the Standard
            rate for their actual lifetime. Re-check AWS pricing before budgeting at scale.
          </p>
        </div>
      </div>
    </div>
  )
}
