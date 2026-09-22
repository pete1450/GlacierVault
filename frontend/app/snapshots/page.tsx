'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import Nav from '@/components/Nav'
import {
  listSnapshots, listSnapshotFiles, initiateRestore, deleteSnapshot,
  getStorage, pruneRepo,
  type Snapshot, type FileEntry, type StorageInfo,
} from '@/lib/api'

export default function SnapshotsPage() {
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [selected, setSelected] = useState<Snapshot | null>(null)
  const [files, setFiles] = useState<FileEntry[]>([])
  const [prefix, setPrefix] = useState('')
  const [restoreDest, setRestoreDest] = useState('')
  const [restoreMsg, setRestoreMsg] = useState('')
  const [checked, setChecked] = useState<string[]>([])
  const [storage, setStorage] = useState<StorageInfo | null>(null)
  const [storageErr, setStorageErr] = useState('')
  const [confirmDelete, setConfirmDelete] = useState<Snapshot | null>(null)
  const [pruneOnDelete, setPruneOnDelete] = useState(false)
  const [confirmPrune, setConfirmPrune] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')

  function refresh() {
    listSnapshots().then(setSnapshots).catch(() => {})
    getStorage().then(s => { setStorage(s); setStorageErr('') }).catch(() => setStorageErr('Could not load storage info.'))
  }

  useEffect(refresh, [])

  async function openSnapshot(snap: Snapshot) {
    setSelected(snap)
    setPrefix('')
    setFiles([])
    setChecked([])
    const entries = await listSnapshotFiles(snap.id).catch(() => [])
    setFiles(entries)
  }

  async function navigatePrefix(p: string) {
    if (!selected) return
    setPrefix(p)
    const entries = await listSnapshotFiles(selected.id, p).catch(() => [])
    setFiles(entries)
  }

  function togglePath(path: string) {
    setChecked(c => c.includes(path) ? c.filter(x => x !== path) : [...c, path])
  }

  function toggleAll() {
    const paths = files.map(f => f.path)
    const allChecked = paths.every(p => checked.includes(p))
    setChecked(allChecked ? checked.filter(p => !paths.includes(p)) : [...new Set([...checked, ...paths])])
  }

  async function handleRestore() {
    if (!selected || !restoreDest) return
    try {
      const result = await initiateRestore(selected.id, checked, restoreDest)
      setRestoreMsg(`Restore job #${result.jobId} started.`)
      setChecked([])
    } catch (err: any) {
      setRestoreMsg(`Error: ${err.message}`)
    }
  }

  async function handleDelete() {
    if (!confirmDelete) return
    setBusy(true)
    try {
      await deleteSnapshot(confirmDelete.id, pruneOnDelete)
      setNotice(pruneOnDelete
        ? 'Snapshot deleted and space reclaimed.'
        : 'Snapshot deleted. Run "Prune repository" to reclaim its space.')
      const deletedId = confirmDelete.id
      setConfirmDelete(null)
      setPruneOnDelete(false)
      if (selected?.id === deletedId) {
        setSelected(null)
        setFiles([])
        setChecked([])
      }
      refresh()
    } catch (err: any) {
      setNotice(`Delete failed: ${err.message}`)
    } finally {
      setBusy(false)
    }
  }

  async function handlePrune() {
    setConfirmPrune(false)
    setBusy(true)
    try {
      await pruneRepo()
      setNotice('Repository pruned. Unreferenced data has been removed.')
      refresh()
    } catch (err: any) {
      setNotice(`Prune failed: ${err.message}`)
    } finally {
      setBusy(false)
    }
  }

  const allChecked = files.length > 0 && files.every(f => checked.includes(f.path))

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      <Nav />

      {/* Storage summary bar */}
      <div className="border-b border-gray-800 px-6 py-3 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
        {storage ? (
          <>
            <span className="font-semibold text-gray-200">Repository storage</span>
            <span><span className="font-bold text-white">{formatBytes(storage.totalBytes)}</span> <span className="text-gray-500">total</span></span>
            <span className="text-gray-500">{formatBytes(storage.packBytes)} in data packs · {formatBytes(storage.indexBytes)} index</span>
            <span className="text-gray-500">{storage.snapshotCount} snapshots · {formatBytes(storage.logicalBytes)} logical size</span>
            <button
              onClick={() => setConfirmPrune(true)}
              disabled={busy}
              className="ml-auto px-3 py-1.5 bg-gray-800 hover:bg-gray-700 disabled:opacity-50 rounded-lg text-sm transition-colors"
              title="Remove unreferenced data and repack. Reclaims space freed by deleted snapshots."
            >
              Prune repository
            </button>
          </>
        ) : (
          <span className="text-gray-500 text-sm">{storageErr || 'Loading storage info…'}</span>
        )}
      </div>

      {notice && (
        <div className="mx-6 mt-3 px-4 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm text-gray-300 flex justify-between items-center">
          <span>{notice}</span>
          <button onClick={() => setNotice('')} className="text-gray-500 hover:text-white">✕</button>
        </div>
      )}

      <div className="flex" style={{ height: 'calc(100vh - 130px)' }}>
      {/* Sidebar: snapshot list */}
      <aside className="w-72 border-r border-gray-800 p-4 space-y-2 overflow-y-auto">
        <h2 className="font-semibold text-sm text-gray-400 uppercase tracking-wide mb-3">Snapshots</h2>
        {snapshots.map(s => (
          <div
            key={s.id}
            className={`w-full px-3 py-2 rounded-lg text-sm hover:bg-gray-800 transition-colors flex items-start gap-2 ${selected?.id === s.id ? 'bg-gray-800 text-white' : 'text-gray-300'}`}
          >
            <button onClick={() => openSnapshot(s)} className="flex-1 text-left">
              <div className="font-medium">{s.hostname}</div>
              <div className="text-xs text-gray-500">{new Date(s.backupTime).toLocaleString()}</div>
              <div className="text-xs text-gray-500">{s.fileCount} files · {formatBytes(s.totalSize)}</div>
            </button>
            <button
              onClick={() => { setConfirmDelete(s); setPruneOnDelete(false) }}
              className="text-gray-600 hover:text-red-400 px-1 py-0.5 text-base leading-none"
              title="Delete this snapshot"
            >
              🗑
            </button>
          </div>
        ))}
        {snapshots.length === 0 && <p className="text-gray-500 text-sm">No snapshots yet.</p>}
      </aside>

      {/* Main: file browser */}
      <main className="flex-1 p-6 space-y-4 overflow-y-auto">
        {!selected ? (
          <div className="text-gray-500 mt-16 text-center">Select a snapshot to browse its contents.</div>
        ) : (
          <>
            <div className="flex items-center gap-3">
              <h1 className="text-xl font-bold">{selected.hostname}</h1>
              <span className="text-gray-400 text-sm">{new Date(selected.backupTime).toLocaleString()}</span>
              <span className="text-gray-500 text-sm">· {formatBytes(selected.totalSize)}</span>
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
                    <th className="text-left px-4 py-3 w-10">
                      <input
                        type="checkbox"
                        checked={allChecked}
                        onChange={toggleAll}
                        className="accent-blue-500"
                        title="Select all in this folder"
                      />
                    </th>
                    <th className="text-left px-4 py-3">Name</th>
                    <th className="text-right px-4 py-3">Size</th>
                    <th className="text-right px-4 py-3">Modified</th>
                  </tr>
                </thead>
                <tbody>
                  {files.map((f, i) => (
                    <tr key={i} className="border-b border-gray-800/50 hover:bg-gray-800/30">
                      <td className="px-4 py-2">
                        <input
                          type="checkbox"
                          checked={checked.includes(f.path)}
                          onChange={() => togglePath(f.path)}
                          className="accent-blue-500"
                        />
                      </td>
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
              <h3 className="font-semibold">
                {checked.length === 0 ? 'Restore This Snapshot' : `Restore ${checked.length} Selected`}
              </h3>
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
              {restoreMsg && (
                <p className="text-sm text-green-400">
                  {restoreMsg} <Link href="/restore" className="underline hover:text-green-300">Track progress →</Link>
                </p>
              )}
              <p className="text-xs text-gray-500">
                {checked.length === 0
                  ? 'No files selected — restores the full snapshot.'
                  : 'Restores only the selected files and folders.'}{' '}
                Glacier retrieval may take 12–48 hours.
              </p>
            </div>
          </>
        )}
      </main>
      </div>

      {/* Delete confirmation modal */}
      {confirmDelete && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50 px-4">
          <div className="bg-gray-900 border border-gray-700 rounded-xl p-6 max-w-md w-full space-y-4">
            <h3 className="text-lg font-bold">Delete snapshot?</h3>
            <p className="text-sm text-gray-300">
              <span className="font-medium text-white">{confirmDelete.hostname}</span> ·{' '}
              {new Date(confirmDelete.backupTime).toLocaleString()} ·{' '}
              {confirmDelete.fileCount} files · {formatBytes(confirmDelete.totalSize)}
            </p>
            <p className="text-sm text-gray-400">
              This removes the snapshot from the repository. This cannot be undone.
            </p>
            <label className="flex items-start gap-2 text-sm text-gray-300 cursor-pointer">
              <input
                type="checkbox"
                checked={pruneOnDelete}
                onChange={e => setPruneOnDelete(e.target.checked)}
                className="accent-blue-500 mt-0.5"
              />
              <span>
                Also reclaim space now (prune)
                <span className="block text-xs text-gray-500 mt-0.5">
                  Rewrites pack files to drop unreferenced data. May take a long time and
                  can trigger Glacier retrieval charges on large repositories.
                </span>
              </span>
            </label>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmDelete(null)}
                disabled={busy}
                className="px-4 py-2 bg-gray-800 hover:bg-gray-700 disabled:opacity-50 rounded-lg text-sm transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleDelete}
                disabled={busy}
                className="px-4 py-2 bg-red-600 hover:bg-red-700 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors"
              >
                {busy ? 'Deleting…' : 'Delete snapshot'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Prune confirmation modal */}
      {confirmPrune && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50 px-4">
          <div className="bg-gray-900 border border-gray-700 rounded-xl p-6 max-w-md w-full space-y-4">
            <h3 className="text-lg font-bold">Prune repository?</h3>
            <p className="text-sm text-gray-400">
              This removes data that is no longer referenced by any snapshot and
              repacks pack files, reclaiming space freed by deleted snapshots.
              It may take a long time on large repositories and can trigger
              Glacier retrieval charges.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmPrune(false)}
                className="px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handlePrune}
                disabled={busy}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors"
              >
                {busy ? 'Pruning…' : 'Prune repository'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}
