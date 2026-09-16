'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import Nav from '@/components/Nav'
import { listJobs, listBackups, listSnapshots, getSetupStatus, type Job, type BackupDef, type Snapshot } from '@/lib/api'

export default function Dashboard() {
  const router = useRouter()
  const [jobs, setJobs] = useState<Job[]>([])
  const [backups, setBackups] = useState<BackupDef[]>([])
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])

  useEffect(() => {
    getSetupStatus().then(s => {
      if (!s.setupComplete) router.push('/setup')
    }).catch(() => {})
    listJobs(5).then(setJobs).catch(() => {})
    listBackups().then(setBackups).catch(() => {})
    listSnapshots().then(setSnapshots).catch(() => {})
  }, [])

  const latestJob = jobs[0]
  const totalSize = snapshots.reduce((sum, s) => sum + s.totalSize, 0)

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      <Nav />
      <main className="max-w-6xl mx-auto px-6 py-8 space-y-6">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="grid grid-cols-3 gap-4">
          <div className="bg-gray-900 rounded-xl p-5"><p className="text-gray-400 text-sm">Backup Definitions</p><p className="text-3xl font-bold mt-1">{backups.length}</p></div>
          <div className="bg-gray-900 rounded-xl p-5"><p className="text-gray-400 text-sm">Snapshots</p><p className="text-3xl font-bold mt-1">{snapshots.length}</p></div>
          <div className="bg-gray-900 rounded-xl p-5"><p className="text-gray-400 text-sm">Total Archived</p><p className="text-3xl font-bold mt-1">{formatBytes(totalSize)}</p></div>
        </div>
        {latestJob && (
          <div className="bg-gray-900 rounded-xl p-6">
            <h2 className="font-semibold mb-3">Last Backup Job</h2>
            <div className="flex items-center gap-3">
              <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-gray-700 text-gray-300">{latestJob.status}</span>
              <span className="text-sm text-gray-300">{new Date(latestJob.startedAt).toLocaleString()}</span>
            </div>
            {latestJob.errorMessage && <p className="text-red-400 text-sm mt-2">{latestJob.errorMessage}</p>}
          </div>
        )}
        <div className="bg-gray-900 rounded-xl p-6">
          <div className="flex items-center justify-between mb-3">
            <h2 className="font-semibold">Backup Sources</h2>
            <Link href="/backups" className="text-blue-400 text-sm hover:underline">Manage →</Link>
          </div>
          {backups.length === 0
            ? <p className="text-gray-500 text-sm">No backups configured. <Link href="/backups" className="text-blue-400 hover:underline">Add one →</Link></p>
            : backups.slice(0, 5).map(b => (
              <div key={b.id} className="flex items-center justify-between text-sm py-1">
                <span className="font-medium">{b.name}</span>
                <span className="text-gray-400">{b.schedule}</span>
              </div>
            ))
          }
        </div>
      </main>
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}
