'use client'
import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import Shell from '@/components/Shell'
import RestoreConfirmDialog from '@/components/RestoreConfirmDialog'
import { formatBytes } from '@/lib/restoreCost'
import {
  listSnapshots, getSnapshotFiles, initiateRestore, deleteSnapshot,
  getStorage, pruneRepo,
  type Snapshot, type FileEntry, type StorageInfo,
  errMsg,
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
  const [confirmRestore, setConfirmRestore] = useState(false)
  const [indexing, setIndexing] = useState<{ phase: string; done: number; total: number } | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(1000)
  const [totalFiles, setTotalFiles] = useState(0)
  const loadToken = useRef(0)

  function refresh() {
    listSnapshots().then(setSnapshots).catch(() => {})
    getStorage().then(s => { setStorage(s); setStorageErr('') }).catch(() => setStorageErr('Could not load storage info.'))
  }

  useEffect(refresh, [])

  // Loads a file listing, polling while the server builds the snapshot's
  // file index in the background (202 responses). The token discards stale
  // results when the user clicks another snapshot mid-poll.
  async function loadFiles(snapId: number, p: string, pg: number, ps: number) {
    const t = ++loadToken.current
    setIndexing(null)
    setFiles([])
    for (;;) {
      if (loadToken.current !== t) return
      const res = await getSnapshotFiles(snapId, p, ps, (pg - 1) * ps).catch(() => null)
      if (loadToken.current !== t) return
      if (res == null) {
        setIndexing(null)
        return
      }
      if (res.status === 'ready') {
        setIndexing(null)
        setFiles(res.files)
        setTotalFiles(res.total)
        return
      }
      setIndexing({ phase: res.phase, done: res.done, total: res.total })
      await new Promise(r => setTimeout(r, 2000))
    }
  }

  async function openSnapshot(snap: Snapshot) {
    setSelected(snap)
    setPrefix('')
    setChecked([])
    setPage(1)
    loadFiles(snap.id, '', 1, pageSize)
  }

  async function navigatePrefix(p: string) {
    if (!selected) return
    setPrefix(p)
    setPage(1)
    loadFiles(selected.id, p, 1, pageSize)
  }

  function changePage(pg: number) {
    if (!selected || pg === page) return
    setPage(pg)
    loadFiles(selected.id, prefix, pg, pageSize)
  }

  function changePageSize(ps: number) {
    if (!selected || ps === pageSize) return
    setPageSize(ps)
    setPage(1)
    loadFiles(selected.id, prefix, 1, ps)
  }

  // Compact page-number items: 1 … p-1 p p+1 … N.
  function pageItems(pg: number, totalPages: number): (number | '…')[] {
    const keep = new Set([1, totalPages, pg - 1, pg, pg + 1])
    const nums = [...keep].filter(n => n >= 1 && n <= totalPages).sort((a, b) => a - b)
    const out: (number | '…')[] = []
    nums.forEach((n, i) => {
      if (i > 0 && n - nums[i - 1] > 1) out.push('…')
      out.push(n)
    })
    return out
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
    setConfirmRestore(true)
  }

  // Best-effort byte total for the checked paths, from the loaded file list.
  // Directories include everything beneath them. May undercount when checked
  // files sit on other pages — the dialog labels the cost an estimate.
  function selectedBytes(): number {
    if (!selected) return 0
    if (checked.length === 0) return selected.totalSize
    let sum = 0
    const seen = new Set<string>()
    for (const c of checked) {
      for (const f of files) {
        if (f.isDir) continue
        if ((f.path === c || f.path.startsWith(c + '/')) && !seen.has(f.path)) {
          seen.add(f.path)
          sum += f.size
        }
      }
    }
    return sum
  }

  async function confirmRestoreJob(): Promise<number> {
    if (!selected) throw new Error('No snapshot selected')
    const result = await initiateRestore(selected.id, checked, restoreDest)
    setRestoreMsg(`Restore job #${result.jobId} started.`)
    setChecked([])
    setConfirmRestore(false)
    return result.jobId
  }

  async function handleDelete() {
    if (!confirmDelete) return
    setBusy(true)
    try {
      const result = await deleteSnapshot(confirmDelete.id, pruneOnDelete)
      setNotice(result.alreadyGone
        ? 'Snapshot was already gone from the repository; catalog entry removed.'
        : pruneOnDelete
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
    } catch (err) {
      setNotice(`Delete failed: ${errMsg(err)}`)
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
    } catch (err) {
      setNotice(`Prune failed: ${errMsg(err)}`)
    } finally {
      setBusy(false)
    }
  }

  const allChecked = files.length > 0 && files.every(f => checked.includes(f.path))

  return (
    <Shell>

      {/* Storage summary bar */}
      <div className="border-b border-line px-6 py-3 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
        {storage ? (
          <>
            <span className="font-semibold text-body">Repository storage</span>
            <span><span className="font-bold text-ink">{formatBytes(storage.totalBytes)}</span> <span className="text-muted">total</span></span>
            <span className="text-muted">{formatBytes(storage.packBytes)} in data packs · {formatBytes(storage.indexBytes)} index</span>
            <span className="text-muted">{storage.snapshotCount} snapshots · {formatBytes(storage.logicalBytes)} latest snapshot</span>
            <button
              onClick={() => setConfirmPrune(true)}
              disabled={busy}
              className="ml-auto px-3 py-1.5 bg-raised hover:bg-raised disabled:opacity-50 rounded-lg text-sm transition-colors"
              title="Remove unreferenced data and repack. Reclaims space freed by deleted snapshots."
            >
              Prune repository
            </button>
          </>
        ) : (
          <span className="text-muted text-sm">{storageErr || 'Loading storage info…'}</span>
        )}
      </div>

      {notice && (
        <div className="mx-6 mt-3 px-4 py-2 bg-surface border border-line rounded-lg text-sm text-body flex justify-between items-center">
          <span>{notice}</span>
          <button onClick={() => setNotice('')} className="text-muted hover:text-ink">✕</button>
        </div>
      )}

      <div className="flex" style={{ height: 'calc(100vh - 130px)' }}>
      {/* Sidebar: snapshot list */}
      <aside className="w-72 border-r border-line p-4 space-y-2 overflow-y-auto">
        <h2 className="font-semibold text-sm text-muted uppercase tracking-wide mb-3">Snapshots</h2>
        {snapshots.map(s => (
          <div
            key={s.id}
            className={`w-full px-3 py-2 rounded-lg text-sm hover:bg-raised transition-colors flex items-start gap-2 ${selected?.id === s.id ? 'bg-raised text-ink' : 'text-body'}`}
          >
            <button onClick={() => openSnapshot(s)} className="flex-1 text-left">
              <div className="font-medium">{s.hostname}</div>
              <div className="text-xs text-muted">{new Date(s.backupTime).toLocaleString()}</div>
              <div className="text-xs text-muted">{s.fileCount} files · {formatBytes(s.totalSize)}</div>
            </button>
            <button
              onClick={() => { setConfirmDelete(s); setPruneOnDelete(false) }}
              className="text-faint hover:text-err px-1 py-0.5 text-base leading-none"
              title="Delete this snapshot"
            >
              🗑
            </button>
          </div>
        ))}
        {snapshots.length === 0 && <p className="text-muted text-sm">No snapshots yet.</p>}
      </aside>

      {/* Main: file browser */}
      <div className="flex-1 p-6 space-y-4 overflow-y-auto">
        {!selected ? (
          <div className="text-muted mt-16 text-center">Select a snapshot to browse its contents.</div>
        ) : (
          <>
            <div className="flex items-center gap-3">
              <h1 className="text-xl font-bold">{selected.hostname}</h1>
              <span className="text-muted text-sm">{new Date(selected.backupTime).toLocaleString()}</span>
              <span className="text-muted text-sm">· {formatBytes(selected.totalSize)}</span>
            </div>

            {/* Breadcrumb */}
            <div className="flex items-center gap-1 text-sm text-muted">
              <button onClick={() => navigatePrefix('')} className="hover:text-ink">/</button>
              {prefix.split('/').filter(Boolean).map((segment, i, arr) => (
                <span key={i}>
                  <span className="mx-1">/</span>
                  <button
                    onClick={() => navigatePrefix(arr.slice(0, i + 1).join('/') + '/')}
                    className="hover:text-ink"
                  >{segment}</button>
                </span>
              ))}
            </div>

            {/* File list */}
            <div className="bg-surface rounded-xl overflow-hidden">
              <table className="w-full text-sm">
                <thead className="border-b border-line">
                  <tr className="text-muted text-xs uppercase">
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
                    <tr key={i} className="border-b border-line/50 hover:bg-raised/30">
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
                          <button onClick={() => navigatePrefix(f.path + '/')} className="text-accent hover:underline">
                            📁 {f.path.split('/').pop()}/
                          </button>
                        ) : (
                          <span>📄 {f.path.split('/').pop()}</span>
                        )}
                      </td>
                      <td className="px-4 py-2 text-right text-muted">{f.isDir ? '' : formatBytes(f.size)}</td>
                      <td className="px-4 py-2 text-right text-muted text-xs">{f.mtime ? new Date(f.mtime).toLocaleDateString() : ''}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!indexing && totalFiles > pageSize && (
                <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm border-t border-line">
                  <span className="text-muted">
                    Showing {((page - 1) * pageSize + 1).toLocaleString()}–
                    {Math.min(page * pageSize, totalFiles).toLocaleString()} of {totalFiles.toLocaleString()}
                  </span>
                  <div className="flex items-center gap-1.5">
                    <select
                      value={pageSize}
                      onChange={e => changePageSize(Number(e.target.value))}
                      className="bg-raised text-body text-xs rounded px-2 py-1 mr-2"
                    >
                      {[250, 500, 1000, 2500].map(n => (
                        <option key={n} value={n}>{n} / page</option>
                      ))}
                    </select>
                    <button
                      disabled={page === 1}
                      onClick={() => changePage(page - 1)}
                      className="px-2 py-1 rounded bg-raised text-body text-xs disabled:opacity-40"
                    >‹ Prev</button>
                    {pageItems(page, Math.ceil(totalFiles / pageSize)).map((it, i) =>
                      it === '…' ? (
                        <span key={`e${i}`} className="text-faint text-xs px-1">…</span>
                      ) : (
                        <button
                          key={it}
                          onClick={() => changePage(it)}
                          className={`px-2 py-1 rounded text-xs ${it === page ? 'bg-accent text-ink' : 'bg-raised text-body'}`}
                        >{it}</button>
                      )
                    )}
                    <button
                      disabled={page >= Math.ceil(totalFiles / pageSize)}
                      onClick={() => changePage(page + 1)}
                      className="px-2 py-1 rounded bg-raised text-body text-xs disabled:opacity-40"
                    >Next ›</button>
                  </div>
                </div>
              )}
              {files.length === 0 && !indexing && <p className="text-center text-muted py-8 text-sm">Empty directory.</p>}
              {indexing && (
                <div className="text-center py-10 space-y-3">
                  <p className="text-sm text-body">
                    {indexing.phase === 'indexing' ? 'Indexing snapshot files…' : 'Discovering snapshot files…'}
                  </p>
                  <p className="text-xs text-muted">
                    {indexing.done.toLocaleString()} of {indexing.total > 0 ? indexing.total.toLocaleString() : '…'} files
                  </p>
                  {indexing.total > 0 && (
                    <div className="mx-auto max-w-md h-2 bg-raised rounded-full overflow-hidden">
                      <div
                        className="h-full bg-accent transition-all"
                        style={{ width: `${Math.min(100, (indexing.done / indexing.total) * 100).toFixed(1)}%` }}
                      />
                    </div>
                  )}
                  <p className="text-xs text-faint">First browse of a snapshot builds its file index — one-time per snapshot.</p>
                </div>
              )}
            </div>

            {/* Restore */}
            <div className="bg-surface rounded-xl p-5 space-y-3">
              <h3 className="font-semibold">
                {checked.length === 0 ? 'Restore This Snapshot' : `Restore ${checked.length} Selected`}
              </h3>
              <div className="flex gap-3">
                <input
                  type="text"
                  placeholder="Destination path (e.g. /restore/output)"
                  value={restoreDest}
                  onChange={e => setRestoreDest(e.target.value)}
                  className="flex-1 px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
                />
                <button
                  onClick={handleRestore}
                  className="px-4 py-2 bg-orange-600 hover:bg-orange-700 rounded-lg font-medium text-sm transition-colors"
                >
                  Review & restore
                </button>
              </div>
              {restoreMsg && (
                <p className="text-sm text-ok">
                  {restoreMsg} <Link href="/restore" className="underline hover:text-ok">Track progress →</Link>
                </p>
              )}
              <p className="text-xs text-muted">
                {checked.length === 0
                  ? 'No files selected — restores the full snapshot.'
                  : 'Restores only the selected files and folders.'}{' '}
                Glacier retrieval may take 12–48 hours.
              </p>
            </div>
          </>
        )}
      </div>
      </div>

      {/* Restore confirmation dialog with cost estimate */}
      {confirmRestore && selected && (
        <RestoreConfirmDialog
          snapshotLabel={`Snapshot #${selected.id}`}
          fileSummary={
            checked.length === 0
              ? `Full snapshot (${selected.fileCount.toLocaleString()} files)`
              : `${checked.length} selected item${checked.length === 1 ? '' : 's'}`
          }
          bytes={selectedBytes()}
          destination={restoreDest}
          onConfirm={confirmRestoreJob}
          onClose={() => setConfirmRestore(false)}
        />
      )}

      {/* Delete confirmation modal */}
      {confirmDelete && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50 px-4">
          <div className="bg-surface border border-line rounded-xl p-6 max-w-md w-full space-y-4">
            <h3 className="text-lg font-bold">Delete snapshot?</h3>
            <p className="text-sm text-body">
              <span className="font-medium text-ink">{confirmDelete.hostname}</span> ·{' '}
              {new Date(confirmDelete.backupTime).toLocaleString()} ·{' '}
              {confirmDelete.fileCount} files · {formatBytes(confirmDelete.totalSize)}
            </p>
            <p className="text-sm text-muted">
              This removes the snapshot from the repository. This cannot be undone.
            </p>
            <label className="flex items-start gap-2 text-sm text-body cursor-pointer">
              <input
                type="checkbox"
                checked={pruneOnDelete}
                onChange={e => setPruneOnDelete(e.target.checked)}
                className="accent-blue-500 mt-0.5"
              />
              <span>
                Also reclaim space now (prune)
                <span className="block text-xs text-muted mt-0.5">
                  Rewrites pack files to drop unreferenced data. May take a long time and
                  can trigger Glacier retrieval charges on large repositories.
                </span>
              </span>
            </label>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmDelete(null)}
                disabled={busy}
                className="px-4 py-2 bg-raised hover:bg-raised disabled:opacity-50 rounded-lg text-sm transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleDelete}
                disabled={busy}
                className="px-4 py-2 bg-err hover:opacity-90 disabled:opacity-50 rounded-lg text-sm font-medium text-white transition-colors"
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
          <div className="bg-surface border border-line rounded-xl p-6 max-w-md w-full space-y-4">
            <h3 className="text-lg font-bold">Prune repository?</h3>
            <p className="text-sm text-muted">
              This removes data that is no longer referenced by any snapshot and
              repacks pack files, reclaiming space freed by deleted snapshots.
              It may take a long time on large repositories and can trigger
              Glacier retrieval charges.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmPrune(false)}
                className="px-4 py-2 bg-raised hover:bg-raised rounded-lg text-sm transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handlePrune}
                disabled={busy}
                className="px-4 py-2 bg-accent hover:bg-accent-deep disabled:opacity-50 rounded-lg text-sm font-medium transition-colors"
              >
                {busy ? 'Pruning…' : 'Prune repository'}
              </button>
            </div>
          </div>
        </div>
      )}
    </Shell>
  )
}
