'use client'
import { useEffect, useState } from 'react'
import Nav from '@/components/Nav'
import { listRestores, getRestore, type RestoreJob } from '@/lib/api'

const STAGES = [
  'queued',
  'warmup_requested',
  'retrieval_in_progress',
  'retrieval_complete',
  'restoring',
  'completed',
]

const STAGE_LABELS: Record<string, string> = {
  queued: 'Queued',
  warmup_requested: 'Warmup requested',
  retrieval_in_progress: 'Retrieving from Glacier',
  retrieval_complete: 'Retrieval complete',
  restoring: 'Restoring files',
  completed: 'Completed',
  failed: 'Failed',
}

const STATUS_COLORS: Record<string, string> = {
  queued: 'bg-gray-700 text-gray-300 border-gray-600',
  warmup_requested: 'bg-yellow-500/20 text-yellow-300 border-yellow-500/30',
  retrieval_in_progress: 'bg-blue-500/20 text-blue-300 border-blue-500/30',
  retrieval_complete: 'bg-teal-500/20 text-teal-300 border-teal-500/30',
  restoring: 'bg-purple-500/20 text-purple-300 border-purple-500/30',
  completed: 'bg-green-500/20 text-green-300 border-green-500/30',
  failed: 'bg-red-500/20 text-red-300 border-red-500/30',
}

const TERMINAL = new Set(['completed', 'failed'])

function stageIndex(status: string): number {
  const i = STAGES.indexOf(status)
  return i === -1 ? STAGES.length : i
}

export default function RestorePage() {
  const [jobs, setJobs] = useState<RestoreJob[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [detail, setDetail] = useState<RestoreJob | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchJobs = async () => {
    try {
      const data = await listRestores()
      setJobs(data)
      if (selectedId === null && data.length > 0) setSelectedId(data[0].id)
    } catch {}
    setLoading(false)
  }

  useEffect(() => {
    fetchJobs()
    const t = setInterval(fetchJobs, 5000)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    if (selectedId === null) { setDetail(null); return }
    let cancelled = false
    const load = async () => {
      const j = await getRestore(selectedId).catch(() => null)
      if (!cancelled && j) setDetail(j)
    }
    load()
    const t = setInterval(async () => {
      const j = await getRestore(selectedId).catch(() => null)
      if (!cancelled && j) {
        setDetail(j)
        if (TERMINAL.has(j.status)) clearInterval(t)
      }
    }, 3000)
    return () => { cancelled = true; clearInterval(t) }
  }, [selectedId])

  const requestedPaths: string[] = (() => {
    try { return JSON.parse(detail?.requestedPaths || '[]') } catch { return [] }
  })()

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      <Nav />
      <div className="border-b border-gray-800 px-6 py-4">
        <h1 className="text-xl font-semibold">Restores</h1>
        <p className="text-sm text-gray-400 mt-0.5">Glacier retrieval and restore progress</p>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-64 text-gray-500">Loading…</div>
      ) : jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-64 text-gray-500 gap-2">
          <p>No restore jobs yet.</p>
          <p className="text-sm">Start one from the Snapshots page.</p>
        </div>
      ) : (
        <div className="flex h-[calc(100vh-137px)]">
          {/* Job list */}
          <div className="w-96 shrink-0 border-r border-gray-800 overflow-y-auto">
            {jobs.map(j => (
              <button
                key={j.id}
                onClick={() => setSelectedId(j.id)}
                className={`w-full text-left px-4 py-3 border-b border-gray-800 hover:bg-gray-800/50 transition-colors ${selectedId === j.id ? 'bg-gray-800/70' : ''}`}
              >
                <div className="flex items-center gap-3">
                  <span className="text-gray-400 text-xs font-mono w-8 shrink-0">#{j.id}</span>
                  <span className={`text-xs px-2 py-0.5 rounded border font-medium ${STATUS_COLORS[j.status] ?? 'bg-gray-700 text-gray-300 border-gray-600'}`}>
                    {STAGE_LABELS[j.status] ?? j.status}
                  </span>
                </div>
                <p className="mt-1 ml-11 text-xs text-gray-400 font-mono truncate">→ {j.destination}</p>
                <p className="ml-11 text-xs text-gray-600">{new Date(j.createdAt).toLocaleString()}</p>
              </button>
            ))}
          </div>

          {/* Detail */}
          <div className="flex-1 overflow-y-auto p-6">
            {detail ? (
              <div className="max-w-3xl space-y-6">
                <div className="flex items-center gap-4">
                  <h2 className="text-lg font-semibold">Restore #{detail.id}</h2>
                  <span className={`text-sm px-2.5 py-0.5 rounded border font-medium ${STATUS_COLORS[detail.status] ?? ''}`}>
                    {STAGE_LABELS[detail.status] ?? detail.status}
                  </span>
                </div>

                {/* Stage timeline */}
                <div className="flex items-center gap-0">
                  {STAGES.map((s, i) => {
                    const idx = detail.status === 'failed' ? stageIndex('failed') : stageIndex(detail.status)
                    const reached = i <= Math.min(idx, STAGES.length - 1)
                    const current = s === detail.status
                    return (
                      <div key={s} className="flex items-center flex-1 last:flex-none">
                        <div className="flex flex-col items-center gap-1">
                          <div className={`w-6 h-6 rounded-full border-2 flex items-center justify-center text-[10px] font-bold ${
                            reached
                              ? current ? 'border-blue-400 bg-blue-500/30 text-blue-200 animate-pulse' : 'border-green-500 bg-green-500/20 text-green-300'
                              : 'border-gray-700 text-gray-600'
                          }`}>
                            {reached && !current ? '✓' : i + 1}
                          </div>
                          <span className={`text-[10px] text-center leading-tight ${reached ? 'text-gray-300' : 'text-gray-600'}`}>
                            {STAGE_LABELS[s]}
                          </span>
                        </div>
                        {i < STAGES.length - 1 && (
                          <div className={`h-0.5 flex-1 mx-1 mb-5 ${i < idx ? 'bg-green-600' : 'bg-gray-800'}`} />
                        )}
                      </div>
                    )
                  })}
                </div>

                {/* Facts */}
                <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
                  {[
                    { label: 'Destination', value: detail.destination, mono: true },
                    { label: 'Snapshot', value: `#${detail.snapshotId}` },
                    { label: 'Retrieval started', value: detail.retrievalStartedAt ? new Date(detail.retrievalStartedAt).toLocaleString() : '—' },
                    { label: 'Restore started', value: detail.restoreStartedAt ? new Date(detail.restoreStartedAt).toLocaleString() : '—' },
                    { label: 'Completed', value: detail.completedAt ? new Date(detail.completedAt).toLocaleString() : '—' },
                    { label: 'Paths', value: requestedPaths.length === 0 ? 'Full snapshot' : `${requestedPaths.length} selected` },
                  ].map(({ label, value, mono }) => (
                    <div key={label} className="bg-gray-900 border border-gray-800 rounded p-3">
                      <p className="text-xs text-gray-500 mb-1">{label}</p>
                      <p className={`text-sm text-gray-200 ${mono ? 'font-mono break-all' : ''}`}>{value}</p>
                    </div>
                  ))}
                </div>

                {requestedPaths.length > 0 && (
                  <div className="bg-gray-900 border border-gray-800 rounded p-4">
                    <p className="text-xs text-gray-500 mb-2">Requested paths</p>
                    <div className="font-mono text-xs text-gray-300 space-y-1">
                      {requestedPaths.map(p => <div key={p}>{p}</div>)}
                    </div>
                  </div>
                )}

                {detail.status === 'failed' && detail.errorMessage && (
                  <div className="bg-red-500/10 border border-red-500/30 rounded p-4">
                    <p className="text-xs font-semibold text-red-400 mb-1">Error</p>
                    <p className="text-sm text-red-300 font-mono">{detail.errorMessage}</p>
                  </div>
                )}

                <div>
                  <p className="text-sm font-medium text-gray-400 mb-2">Log output</p>
                  <div className="bg-gray-950 rounded border border-gray-700 p-3 h-64 overflow-y-auto font-mono text-xs text-gray-300">
                    {detail.logOutput
                      ? detail.logOutput.split('\n').filter(Boolean).map((line, i) => (
                          <div key={i} className={line.startsWith('[error]') ? 'text-red-400' : ''}>{line}</div>
                        ))
                      : <span className="text-gray-600">No log output yet.</span>}
                  </div>
                </div>
              </div>
            ) : (
              <div className="flex items-center justify-center h-full text-gray-600">
                Select a restore job to view details
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
