'use client'
import { useEffect, useState } from 'react'
import {
  getSetupStatus, type SetupStatus,
  grantSnapshotDeletePermission, revokeSnapshotDeletePermission, getSnapshotDeleteStatus,
  errMsg,
} from '@/lib/api'

export default function InfrastructurePage() {
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [iamAccessKey, setIamAccessKey] = useState('')
  const [iamSecretKey, setIamSecretKey] = useState('')
  const [iamMsg, setIamMsg] = useState('')
  const [iamBusy, setIamBusy] = useState(false)
  const [iamGranted, setIamGranted] = useState<boolean | null>(null)

  useEffect(() => {
    getSetupStatus().then(setStatus).catch(() => {})
    getSnapshotDeleteStatus().then(s => setIamGranted(s.granted)).catch(() => {})
  }, [])

  async function handleGrantSnapshotDelete(e: React.FormEvent) {
    e.preventDefault()
    setIamMsg('')
    setIamBusy(true)
    try {
      await grantSnapshotDeletePermission(iamAccessKey, iamSecretKey)
      setIamAccessKey(''); setIamSecretKey('')
      setIamGranted(true)
      setIamMsg('Permission granted — snapshot deletion should work now.')
    } catch (err) {
      setIamMsg(`Error: ${errMsg(err)}`)
    } finally {
      setIamBusy(false)
    }
  }

  async function handleRevokeSnapshotDelete() {
    setIamMsg('')
    setIamBusy(true)
    try {
      await revokeSnapshotDeletePermission(iamAccessKey, iamSecretKey)
      setIamAccessKey(''); setIamSecretKey('')
      setIamGranted(false)
      setIamMsg('Permission revoked — snapshots are read-only (append-only) again.')
    } catch (err) {
      setIamMsg(`Error: ${errMsg(err)}`)
    } finally {
      setIamBusy(false)
    }
  }

  return (
    <div className="space-y-6">
    <section className="bg-surface rounded-xl p-6 space-y-3">
      <h2 className="font-semibold text-lg">Infrastructure</h2>
      {status ? (
        <dl className="grid grid-cols-2 gap-3 text-sm">
          <div><dt className="text-muted text-xs">Region</dt><dd className="font-mono">{status.region || '—'}</dd></div>
          <div><dt className="text-muted text-xs">Deployed</dt><dd>{status.deployedAt ? new Date(status.deployedAt).toLocaleString() : '—'}</dd></div>
          <div><dt className="text-muted text-xs">Hot bucket</dt><dd className="font-mono break-all">{status.hotBucket || '—'}</dd></div>
          <div><dt className="text-muted text-xs">Cold bucket</dt><dd className="font-mono break-all">{status.coldBucket || '—'}</dd></div>
        </dl>
      ) : (
        <p className="text-muted text-sm">Loading…</p>
      )}
    </section>
    <section className="bg-surface rounded-xl p-6 space-y-4">
      <h2 className="font-semibold text-lg">Snapshot deletion permission</h2>
      <p className="text-sm text-muted">
        Deleting snapshots (<code className="text-body">rustic forget</code>) and pruning
        need <code className="text-body">s3:DeleteObject</code> on the backup buckets, but
        the infrastructure is append-only by design and doesn&apos;t grant it. Granting it
        weakens that protection (a leaked backup credential could delete archives) —
        revoking restores it. Either way takes effect immediately.
      </p>
      <div className="flex items-center gap-2 text-sm">
        <span className={`inline-block w-2.5 h-2.5 rounded-full ${iamGranted ? 'bg-ok' : 'bg-emboss'}`} />
        <span>
          {iamGranted === null ? 'Loading…' : iamGranted
            ? 'Delete permission granted — snapshots can be deleted'
            : 'Not granted — snapshots are read-only (append-only)'}
        </span>
      </div>
      <p className="text-xs text-muted">
        Uses a temporary AWS admin access key (the same kind used during setup). It is
        used for this request only and is never stored.
      </p>
      <form onSubmit={handleGrantSnapshotDelete} className="space-y-3 max-w-sm">
        <div>
          <label className="block text-sm text-body mb-1">AWS access key ID</label>
          <input
            type="text"
            value={iamAccessKey}
            onChange={e => setIamAccessKey(e.target.value)}
            className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm font-mono"
            required
            autoComplete="off"
          />
        </div>
        <div>
          <label className="block text-sm text-body mb-1">AWS secret access key</label>
          <input
            type="password"
            value={iamSecretKey}
            onChange={e => setIamSecretKey(e.target.value)}
            className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm font-mono"
            required
            autoComplete="off"
          />
        </div>
        <div className="flex gap-2">
          <button
            type="submit"
            disabled={iamBusy}
            className="px-4 py-2 bg-accent hover:bg-accent-deep disabled:bg-raised rounded-lg text-sm font-medium transition-colors"
          >
            {iamBusy ? 'Working…' : 'Grant delete permission'}
          </button>
          <button
            type="button"
            onClick={handleRevokeSnapshotDelete}
            disabled={iamBusy || !iamAccessKey || !iamSecretKey}
            className="px-4 py-2 bg-raised hover:bg-emboss disabled:bg-raised disabled:text-muted rounded-lg text-sm font-medium transition-colors"
          >
            Revoke (append-only)
          </button>
        </div>
      </form>
      {iamMsg && <p className="text-sm text-body">{iamMsg}</p>}
    </section>
    </div>
  )
}
