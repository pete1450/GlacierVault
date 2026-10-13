'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import Shell from '@/components/Shell'
import IcebergMeter from '@/components/IcebergMeter'
import CostDialog from '@/components/CostDialog'
import { formatBytes, formatUSD } from '@/lib/restoreCost'
import { listJobs, listBackups, listSnapshots, getSetupStatus, getStorage, type Job, type BackupDef, type Snapshot, type StorageInfo } from '@/lib/api'

const DEEP_ARCHIVE_GB_MONTH = 0.00099
const STANDARD_GB_MONTH = 0.023

function statusPill(status: string) {
  const s = status.toLowerCase()
  const cls =
    s === 'completed' || s === 'complete'
      ? 'bg-ok/15 text-ok border-ok/40'
      : s === 'running'
        ? 'bg-accent-soft text-accent-strong border-accent/40'
        : s === 'failed'
          ? 'bg-err/15 text-err border-err/40'
          : 'bg-warn/15 text-warn border-warn/40'
  return (
    <span className={`px-2.5 py-0.5 rounded-full text-[11px] font-bold uppercase tracking-wider border ${cls}`}>
      {status}
    </span>
  )
}

export default function Dashboard() {
  const router = useRouter()
  const [jobs, setJobs] = useState<Job[]>([])
  const [backups, setBackups] = useState<BackupDef[]>([])
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [storage, setStorage] = useState<StorageInfo | null>(null)
  const [costOpen, setCostOpen] = useState(false)

  useEffect(() => {
    getSetupStatus().then(s => {
      if (!s.setupComplete) router.push('/setup')
    }).catch(() => {})
    listJobs(5).then(setJobs).catch(() => {})
    listBackups().then(setBackups).catch(() => {})
    listSnapshots().then(setSnapshots).catch(() => {})
    getStorage().then(setStorage).catch(() => {})
  }, [router])

  const packBytes = storage?.packBytes ?? 0
  const totalBytes = storage?.totalBytes ?? snapshots.reduce((sum, s) => sum + s.totalSize, 0)
  const hotBytes = Math.max(0, totalBytes - packBytes)
  const estMonthly = (packBytes / 1024 ** 3) * DEEP_ARCHIVE_GB_MONTH + (hotBytes / 1024 ** 3) * STANDARD_GB_MONTH

  return (
    <Shell>
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-sm text-muted mt-1">Your archive at a glance — most of it is below the waterline.</p>
        </div>

        {/* Storage hero */}
        <div className="bg-surface border border-line rounded-2xl p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="font-semibold">Storage</h2>
            <Link href="/snapshots" className="text-accent text-sm font-medium hover:underline">Browse snapshots →</Link>
          </div>
          <IcebergMeter hotBytes={hotBytes} coldBytes={packBytes} />
        </div>

        {/* Stat cards */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="bg-surface border border-line rounded-xl p-5">
            <p className="text-muted text-xs font-semibold uppercase tracking-wider">Stored</p>
            <p className="text-3xl font-bold mt-1">{formatBytes(totalBytes)}</p>
          </div>
          <div className="bg-surface border border-line rounded-xl p-5">
            <p className="text-muted text-xs font-semibold uppercase tracking-wider">Snapshots</p>
            <p className="text-3xl font-bold mt-1">{storage?.snapshotCount ?? snapshots.length}</p>
          </div>
          <div className="bg-surface border border-line rounded-xl p-5">
            <p className="text-muted text-xs font-semibold uppercase tracking-wider">Backups</p>
            <p className="text-3xl font-bold mt-1">{backups.length}</p>
          </div>
          <button
            onClick={() => setCostOpen(true)}
            className="bg-surface border border-line rounded-xl p-5 text-left hover:border-accent transition-colors group"
            title="View cost breakdown"
          >
            <p className="text-muted text-xs font-semibold uppercase tracking-wider">Est. cost / mo</p>
            <p className="text-3xl font-bold mt-1 text-accent-strong group-hover:underline">{formatUSD(estMonthly)}</p>
            <p className="text-[11px] text-faint mt-1">click for breakdown →</p>
          </button>
        </div>

        {/* Recent jobs */}
        <div className="bg-surface border border-line rounded-2xl p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="font-semibold">Recent jobs</h2>
            <Link href="/jobs" className="text-accent text-sm font-medium hover:underline">All jobs →</Link>
          </div>
          {jobs.length === 0 ? (
            <p className="text-muted text-sm">No jobs yet.</p>
          ) : (
            <div className="space-y-2">
              {jobs.slice(0, 5).map(j => (
                <div key={j.id} className="flex items-center gap-3 text-sm">
                  {statusPill(j.status)}
                  <span className="text-body truncate">{new Date(j.startedAt).toLocaleString()}</span>
                  {j.errorMessage && <span className="text-err text-xs truncate">{j.errorMessage}</span>}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {costOpen && (
        <CostDialog packBytes={packBytes} totalBytes={totalBytes} onClose={() => setCostOpen(false)} />
      )}
    </Shell>
  )
}
