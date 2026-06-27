'use client'
import { useEffect, useState } from 'react'
import { listSnapshots, listSnapshotFiles, initiateRestore, type Snapshot, type FileEntry } from '@/lib/api'

export default function SnapshotsPage() {
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [selected, setSelected] = useState<Snapshot | null>(null)
  const [files, setFiles] = useState<FileEntry[]>([])
  const [prefix, setPrefix] = useState('')
  const [restoreDest, setRestoreDest] = useState('')
  const [restoreMsg, setRestoreMsg] = useState('')

  useEffect(() => {
    listSnapshots().then(setSnapshots).catch(() => {})
  }, [])

  async function openSnapshot(snap: Snapshot) {
    setSelected(snap)
    setPrefix('')
    setFiles([])
    const entries = await listSnapshotFiles(snap.id).catch(() => [])
    setFiles(entries)
  }

  async function navigatePrefix(p: string) {
    if (!selected) return
    setPrefix(p)
    const entries = await listSnapshotFiles(selected.id, p).catch(() => [])
    setFiles(entries)
  }

  async function handleRestore() {
    if (!selected || !restoreDest) return
    try {
      const result = await initiateRestore(selected.id, [], restoreDest)
      setRestoreMsg(`Restore job #${result.jobId} started. Check Jobs for progress.`)
    } catch (err: any) {
      setRestoreMsg(`Error: ${err.message}`)
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 text-white flex">
      {/* Sidebar: snapshot list */}
      <aside className="w-72 border-r border-gray-800 p-4 space-y-2 overflow-y-auto">
        <h2 className="font-semibold text-sm text-gray-400 uppercase tracking-wide mb-3">Snapshots</h2>
        {snapshots.map(s => (
          <button
            key={s.id}
            onClick={() => openSnapshot(s)}
            className={`w-full text-left px-3 py-2 rounded-lg text-sm hover:bg-gray-800 transition-colors ${selected?.id === s.id ? 'bg-gray-800 text-white' : 'text-gray-300'}`}
          >
            <div className="font-medium">{s.hostname}</div>
            <div className="text-xs text-gray-500">{new Date(s.backupTime).toLocaleString()}</div>
            <div className="text-xs text-gray-500">{s.fileCount} files · {formatBytes(s.totalSize)}</div>
          </button>
        ))}
        {snapshots.length === 0 && <p className="text-gray-500 text-sm">No snapshots yet.</p>}
      </aside>

      {/* Main: file browser */}
      <main className="flex-1 p-6 space-y-4">
        {!selected ? (
          <div className="text-gray-500 mt-16 text-center">Select a snapshot to browse its contents.</div>
        ) : (
          <>
            <div className="flex items-center gap-3">
              <h1 className="text-xl font-bold">{selected.hostname}</h1>
              <span className="text-gray-400 text-sm">{new Date(selected.backupTime).toLocaleString()}</span>
            </div>

            {/* Breadcrumb */}
            <div className="flex items-center gap-1 text-sm text-gray-400">
              <button onClick={() => navigatePrefix('')} className="hover:text-white">/</button>
              {prefix.split('/').filter(Boolean).map((segment, i, arr) => (
                <span key={i}>
                  <span className="mx-1">/</span>
                  <button
                    onClick={() => navigatePrefix(arr.slice(0, i + 1).join('/') + '/')}
                    className="hover:text-white"
                  >{segment}</button>
                </span>
              ))}
            </div>

            {/* File list */}
            <div className="bg-gray-900 rounded-xl overflow-hidden">
              <table className="w-full text-sm">
                <thead className="border-b border-gray-800">
                  <tr className="text-gray-400 text-xs uppercase">
                    <th className="text-left px-4 py-3">Name</th>
                    <th className="text-right px-4 py-3">Size</th>
                    <th className="text-right px-4 py-3">Modified</th>
                  </tr>
                </thead>
                <tbody>
                  {files.map((f, i) => (
                    <tr key={i} className="border-b border-gray-800/50 hover:bg-gray-800/30">
                      <td className="px-4 py-2">
                        {f.isDir ? (
                          <button onClick={() => navigatePrefix(f.path + '/')} className="text-blue-400 hover:underline">
                            📁 {f.path.split('/').pop()}/
                          </button>
                        ) : (
                          <span>📄 {f.path.split('/').pop()}</span>
                        )}
                      </td>
                      <td className="px-4 py-2 text-right text-gray-400">{f.isDir ? '' : formatBytes(f.size)}</td>
                      <td className="px-4 py-2 text-right text-gray-400 text-xs">{f.mtime ? new Date(f.mtime).toLocaleDateString() : ''}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {files.length === 0 && <p className="text-center text-gray-500 py-8 text-sm">Empty directory.</p>}
            </div>

            {/* Restore */}
            <div className="bg-gray-900 rounded-xl p-5 space-y-3">
              <h3 className="font-semibold">Restore This Snapshot</h3>
              <div className="flex gap-3">
                <input
                  type="text"
                  placeholder="Destination path (e.g. /restore/output)"
                  value={restoreDest}
                  onChange={e => setRestoreDest(e.target.value)}
                  className="flex-1 px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm"
                />
                <button
                  onClick={handleRestore}
                  className="px-4 py-2 bg-orange-600 hover:bg-orange-700 rounded-lg font-medium text-sm transition-colors"
                >
                  Restore
                </button>
              </div>
              {restoreMsg && <p className="text-sm text-green-400">{restoreMsg}</p>}
              <p className="text-xs text-gray-500">Full snapshot restore. Glacier retrieval may take 12–48 hours.</p>
            </div>
          </>
        )}
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
