'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import {
  PRICES_AS_OF,
  PRICES_REGION,
  PACK_BYTES,
  formatUSD,
  formatBytes,
  scenarioForBytes,
  restoreCostItems,
  restoreCostTotal,
} from '@/lib/restoreCost'

export default function RestoreConfirmDialog({
  snapshotLabel,
  fileSummary,
  bytes,
  destination,
  onConfirm,
  onClose,
}: {
  snapshotLabel: string
  fileSummary: string
  bytes: number
  destination: string
  onConfirm: () => Promise<number>
  onClose: () => void
}) {
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')
  const [jobId, setJobId] = useState<number | null>(null)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !confirming) onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, confirming])

  const scenario = scenarioForBytes('This restore', '', Math.max(bytes, 1))
  const items = restoreCostItems(scenario)
  const total = restoreCostTotal(scenario)

  async function handleConfirm() {
    setConfirming(true)
    setError('')
    try {
      const id = await onConfirm()
      setJobId(id)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setConfirming(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-label="Confirm restore">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => !confirming && onClose()} />
      <div className="relative w-full max-w-lg max-h-[88vh] overflow-y-auto bg-surface border border-line rounded-2xl shadow-2xl">
        <div className="sticky top-0 flex items-center justify-between px-6 py-4 bg-surface/95 backdrop-blur border-b border-line">
          <div>
            <h2 className="text-lg font-bold text-ink">Confirm restore</h2>
            <p className="text-xs text-muted">
              {snapshotLabel} → <span className="font-mono">{destination}</span>
            </p>
          </div>
          {!confirming && jobId === null && (
            <button onClick={onClose} aria-label="Close" className="p-2 rounded-lg text-muted hover:text-ink hover:bg-raised">
              ✕
            </button>
          )}
        </div>

        <div className="px-6 py-5 space-y-5">
          {jobId !== null ? (
            <div className="text-center py-6 space-y-3">
              <div className="mx-auto w-12 h-12 rounded-full bg-ok/15 border border-ok/40 flex items-center justify-center text-ok text-2xl">
                ✓
              </div>
              <p className="font-semibold text-ink">Restore job #{jobId} started</p>
              <p className="text-sm text-muted">
                Glacier retrieval may take 12–48 hours. You&apos;ll be notified when it&apos;s ready.
              </p>
              <div className="flex gap-3 justify-center pt-2">
                <Link
                  href="/restore"
                  className="px-4 py-2 rounded-lg bg-accent hover:bg-accent-deep text-white text-sm font-medium transition-colors"
                >
                  Track progress →
                </Link>
                <button
                  onClick={onClose}
                  className="px-4 py-2 rounded-lg bg-raised hover:bg-emboss text-sm font-medium transition-colors"
                >
                  Close
                </button>
              </div>
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between text-sm bg-raised rounded-lg px-4 py-3">
                <span className="text-body">{fileSummary}</span>
                <span className="font-mono font-semibold">{formatBytes(bytes)}</span>
              </div>

              <section>
                <h3 className="text-sm font-bold uppercase tracking-wider text-faint mb-1">
                  Estimated cost <span className="font-mono text-accent-strong">{formatUSD(total)}</span>
                </h3>
                <p className="text-xs text-muted mb-3">
                  {PRICES_REGION} · prices as of {PRICES_AS_OF} · Bulk tier · ~{scenario.packs.toLocaleString()} pack{scenario.packs === 1 ? '' : 's'} to warm
                </p>
                <div className="space-y-2 text-sm">
                  {items.map(it => (
                    <div key={it.label} className="flex justify-between gap-4">
                      <span className="text-body">
                        {it.label} <span className="text-faint">({it.detail})</span>
                      </span>
                      <span className="font-mono">{formatUSD(it.cost)}</span>
                    </div>
                  ))}
                </div>
                <p className="text-xs text-faint mt-3">
                  Estimate only — actual cost depends on which {formatBytes(PACK_BYTES)} packs hold the
                  restored data. Egress rides the CloudFront free tier (1 TB/mo).
                </p>
              </section>

              {error && (
                <p className="text-sm text-err bg-err/10 border border-err/40 rounded-lg px-4 py-3">
                  {error}
                </p>
              )}

              <div className="flex gap-3 pt-1">
                <button
                  onClick={handleConfirm}
                  disabled={confirming}
                  className="flex-1 px-4 py-2.5 rounded-lg bg-accent hover:bg-accent-deep disabled:opacity-50 text-white text-sm font-semibold transition-colors"
                >
                  {confirming ? 'Starting restore…' : `Confirm restore · ~${formatUSD(total)}`}
                </button>
                <button
                  onClick={onClose}
                  disabled={confirming}
                  className="px-4 py-2.5 rounded-lg bg-raised hover:bg-emboss disabled:opacity-50 text-sm font-medium transition-colors"
                >
                  Cancel
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
