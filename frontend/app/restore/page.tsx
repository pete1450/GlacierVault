'use client'
import { useEffect, useState } from 'react'
import Shell from '@/components/Shell'
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
  queued: 'bg-raised text-body border-line',
  warmup_requested: 'bg-warn/20 text-warn border-warn/30',
  retrieval_in_progress: 'bg-accent/20 text-accent-strong border-accent/30',
  retrieval_complete: 'bg-teal-500/20 text-teal-300 border-teal-500/30',
  restoring: 'bg-accent-soft text-accent-strong border-accent/30',
  completed: 'bg-ok/15 text-ok border-ok/30',
  failed: 'bg-err/20 text-err border-err/30',
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
    <Shell>
      <div className="space-y-6">
      <div className="border-b border-line px-6 py-4">
        <h1 className="text-xl font-semibold">Restores</h1>
        <p className="text-sm text-muted mt-0.5">Glacier retrieval and restore progress</p>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-64 text-muted">Loading…</div>
      ) : jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-64 text-muted gap-2">
          <p>No restore jobs yet.</p>
          <p className="text-sm">Start one from the Snapshots page.</p>
        </div>
      ) : (
        <div className="flex h-[calc(100vh-137px)]">
          {/* Job list */}
          <div className="w-96 shrink-0 border-r border-line overflow-y-auto">
            {jobs.map(j => (
              <button
                key={j.id}
                onClick={() => setSelectedId(j.id)}
                className={`w-full text-left px-4 py-3 border-b border-line hover:bg-raised/50 transition-colors ${selectedId === j.id ? 'bg-raised/70' : ''}`}
              >
                <div className="flex items-center gap-3">
                  <span className="text-muted text-xs font-mono w-8 shrink-0">#{j.id}</span>
                  <span className={`text-xs px-2 py-0.5 rounded border font-medium ${STATUS_COLORS[j.status] ?? 'bg-raised text-body border-line'}`}>
                    {STAGE_LABELS[j.status] ?? j.status}
                  </span>
                </div>
                <p className="mt-1 ml-11 text-xs text-muted font-mono truncate">→ {j.destination}</p>
                <p className="ml-11 text-xs text-faint">{new Date(j.createdAt).toLocaleString()}</p>
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
                              ? current ? 'border-accent bg-accent-soft text-accent-strong animate-pulse' : 'border-ok bg-ok/15 text-ok'
                              : 'border-line text-faint'
                          }`}>
                            {reached && !current ? '✓' : i + 1}
                          </div>
                          <span className={`text-[10px] text-center leading-tight ${reached ? 'text-body' : 'text-faint'}`}>
                            {STAGE_LABELS[s]}
                          </span>
                        </div>
                        {i < STAGES.length - 1 && (
                          <div className={`h-0.5 flex-1 mx-1 mb-5 ${i < idx ? 'bg-ok' : 'bg-raised'}`} />
                        )}
                      </div>
                    )
                  })}
                </div>

                {/* Facts */}
                <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
                  {[
                    { label: 'Destination', value: detail.destination, mono: true },
                    { label: 'Snapshot', value: detail.snapshotId != null ? `#${detail.snapshotId}` : 'deleted' },
                    { label: 'Retrieval started', value: detail.retrievalStartedAt ? new Date(detail.retrievalStartedAt).toLocaleString() : '—' },
                    { label: 'Restore started', value: detail.restoreStartedAt ? new Date(detail.restoreStartedAt).toLocaleString() : '—' },
                    { label: 'Completed', value: detail.completedAt ? new Date(detail.completedAt).toLocaleString() : '—' },
                    { label: 'Paths', value: requestedPaths.length === 0 ? 'Full snapshot' : `${requestedPaths.length} selected` },
                  ].map(({ label, value, mono }) => (
                    <div key={label} className="bg-surface border border-line rounded p-3">
                      <p className="text-xs text-muted mb-1">{label}</p>
                      <p className={`text-sm text-body ${mono ? 'font-mono break-all' : ''}`}>{value}</p>
                    </div>
                  ))}
                </div>

                {requestedPaths.length > 0 && (
                  <div className="bg-surface border border-line rounded p-4">
                    <p className="text-xs text-muted mb-2">Requested paths</p>
                    <div className="font-mono text-xs text-body space-y-1">
                      {requestedPaths.map(p => <div key={p}>{p}</div>)}
                    </div>
                  </div>
                )}

                {detail.status === 'failed' && detail.errorMessage && (
                  <div className="bg-err/10 border border-err/30 rounded p-4">
                    <p className="text-xs font-semibold text-err mb-1">Error</p>
                    <p className="text-sm text-err font-mono">{detail.errorMessage}</p>
                  </div>
                )}

                <div>
                  <p className="text-sm font-medium text-muted mb-2">Log output</p>
                  <div className="bg-base rounded border border-line p-3 h-64 overflow-y-auto font-mono text-xs text-body">
                    {detail.logOutput
                      ? detail.logOutput.split('\n').filter(Boolean).map((line, i) => (
                          <div key={i} className={line.startsWith('[error]') ? 'text-err' : ''}>{line}</div>
                        ))
                      : <span className="text-faint">No log output yet.</span>}
                  </div>
                </div>
              </div>
            ) : (
              <div className="flex items-center justify-center h-full text-faint">
                Select a restore job to view details
              </div>
            )}
          </div>
        </div>
      )}
      </div>
    </Shell>
  )
}
