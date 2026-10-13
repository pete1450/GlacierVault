'use client'
import { useEffect } from 'react'
import {
  PRICES_AS_OF,
  PRICES_REGION,
  DEEP_ARCHIVE_GB_MONTH,
  STANDARD_GB_MONTH,
  PACK_BYTES,
  formatUSD,
  formatBytes,
  scenarioForBytes,
  restoreCostItems,
  restoreCostTotal,
  type RestoreScenario,
} from '@/lib/restoreCost'

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
  const halfPacks = Math.ceil(packCount / 2)
  const scenarios: RestoreScenario[] = [
    scenarioForBytes(
      'One small file',
      `warms its whole ${formatBytes(PACK_BYTES)} pack`,
      Math.min(PACK_BYTES, packBytes || PACK_BYTES),
    ),
    scenarioForBytes(
      'Half the repository',
      `${halfPacks.toLocaleString()} of ${packCount.toLocaleString()} packs`,
      packBytes / 2,
    ),
    scenarioForBytes('Whole repository', `${packCount.toLocaleString()} packs`, packBytes),
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
            const items = restoreCostItems(sc)
            const total = restoreCostTotal(sc)
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
