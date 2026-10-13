'use client'

import { useEffect, useRef, useState } from 'react'
import Shell from '@/components/Shell'
import { listJobs, getJob, cancelJob, type Job } from '@/lib/api'

const STATUS_COLORS: Record<string, string> = {
  running: 'bg-accent/20 text-accent-strong border-accent/30',
  completed: 'bg-ok/15 text-ok border-ok/30',
  failed: 'bg-err/20 text-err border-err/30',
  cancelled: 'bg-raised text-muted border-line',
}

function formatBytes(bytes: number) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

function formatDuration(startedAt: string, completedAt: string | null) {
  const start = new Date(startedAt).getTime()
  const end = completedAt ? new Date(completedAt).getTime() : Date.now()
  const s = Math.floor((end - start) / 1000)
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}

function LogStream({ jobId, initialLog }: { jobId: number; initialLog: string }) {
  const [lines, setLines] = useState<string[]>(
    initialLog ? initialLog.split('\n').filter(Boolean) : []
  )
  const bottomRef = useRef<HTMLDivElement>(null)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    // Only stream if job is running
    esRef.current = new EventSource(`/api/jobs/${jobId}/stream`)
    esRef.current.onmessage = (e) => {
      setLines((prev) => [...prev, e.data])
    }
    esRef.current.onerror = () => {
      esRef.current?.close()
    }
    return () => {
      esRef.current?.close()
    }
  }, [jobId])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [lines])

  return (
    <div className="bg-base rounded border border-line p-3 h-64 overflow-y-auto font-mono text-xs text-body">
      {lines.length === 0 ? (
        <span className="text-faint">No log output yet…</span>
      ) : (
        lines.map((line, i) => (
          <div key={i} className={line.startsWith('[error]') ? 'text-err' : line.startsWith('[warn]') ? 'text-warn' : ''}>
            {line}
          </div>
        ))
      )}
      <div ref={bottomRef} />
    </div>
  )
}

function JobRow({ job, selected, onSelect }: { job: Job; selected: boolean; onSelect: () => void }) {
  return (
    <button
      onClick={onSelect}
      className={`w-full text-left px-4 py-3 border-b border-line hover:bg-raised/50 transition-colors ${selected ? 'bg-raised/70' : ''}`}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-3 min-w-0">
          <span className="text-muted text-xs font-mono w-8 shrink-0">#{job.id}</span>
          <span className={`text-xs px-2 py-0.5 rounded border font-medium ${STATUS_COLORS[job.status] ?? 'bg-raised text-body border-line'}`}>
            {job.status}
          </span>
          <span className="text-sm text-body truncate">
            Backup job {job.backupDefId ? `(def #${job.backupDefId})` : '(setup)'}
          </span>
        </div>
        <div className="flex items-center gap-4 shrink-0 text-xs text-muted">
          {job.bytesTransferred > 0 && <span>{formatBytes(job.bytesTransferred)}</span>}
          <span>{formatDuration(job.startedAt, job.completedAt)}</span>
          <span>{new Date(job.startedAt).toLocaleString()}</span>
        </div>
      </div>
      {job.status === 'failed' && job.errorMessage && (
        <p className="mt-1 ml-11 text-xs text-err truncate">{job.errorMessage}</p>
      )}
    </button>
  )
}

export default function JobsPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [selectedJob, setSelectedJob] = useState<Job | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchJobs = async () => {
    try {
      const data = await listJobs(50)
      setJobs(data)
      // Auto-select first running job, then most recent — but only until
      // the user picks one. Functional update: the 5s poll interval holds
      // the first render's closure (where selectedId is always null), so
      // reading selectedId directly here would re-select the running job
      // on every poll.
      setSelectedId((prev) =>
        prev === null ? (data.find((j) => j.status === 'running')?.id ?? data[0]?.id ?? null) : prev
      )
    } catch {
      setError('Failed to load jobs')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchJobs()
    // Poll job list every 5s to pick up new jobs and status changes
    const t = setInterval(fetchJobs, 5000)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    if (selectedId === null) { setSelectedJob(null); return }
    getJob(selectedId).then(setSelectedJob).catch(() => {})
    // Refresh detail while job is running
    const t = setInterval(async () => {
      const j = await getJob(selectedId).catch(() => null)
      if (j) {
        setSelectedJob(j)
        if (j.status !== 'running') clearInterval(t)
      }
    }, 3000)
    return () => clearInterval(t)
  }, [selectedId])

  const runningCount = jobs.filter((j) => j.status === 'running').length

  const handleCancel = async () => {
    if (selectedJob == null || selectedJob.status !== 'running') return
    if (!window.confirm(`Cancel backup job #${selectedJob.id}? Uploaded packs stay in the repo; the next backup re-uploads what this run never indexed.`)) return
    try {
      await cancelJob(selectedJob.id)
      const j = await getJob(selectedJob.id).catch(() => null)
      if (j) setSelectedJob(j)
      fetchJobs()
    } catch (e) {
      alert(e instanceof Error ? e.message : 'Cancel failed')
    }
  }

  return (
    <Shell>
      <div className="space-y-6">
      {/* Header */}
      <div className="border-b border-line px-6 py-4">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold">Jobs</h1>
            <p className="text-sm text-muted mt-0.5">Backup job history and live logs</p>
          </div>
          {runningCount > 0 && (
            <span className="flex items-center gap-2 text-sm text-accent-strong bg-accent/10 border border-accent/20 px-3 py-1.5 rounded-full">
              <span className="h-2 w-2 rounded-full bg-accent-strong animate-pulse" />
              {runningCount} running
            </span>
          )}
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-64 text-muted">Loading…</div>
      ) : error ? (
        <div className="m-6 bg-err/10 border border-err/30 rounded p-4 text-err">{error}</div>
      ) : jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-64 text-muted gap-2">
          <p>No jobs yet.</p>
          <p className="text-sm">Run a backup from the Backups page to get started.</p>
        </div>
      ) : (
        <div className="flex h-[calc(100vh-73px)]">
          {/* Job list */}
          <div className="w-96 shrink-0 border-r border-line overflow-y-auto">
            {jobs.map((job) => (
              <JobRow
                key={job.id}
                job={job}
                selected={job.id === selectedId}
                onSelect={() => setSelectedId(job.id)}
              />
            ))}
          </div>

          {/* Detail panel */}
          <div className="flex-1 overflow-y-auto p-6">
            {selectedJob ? (
              <div className="max-w-3xl space-y-6">
                {/* Title row */}
                <div className="flex items-center gap-4">
                  <h2 className="text-lg font-semibold">Job #{selectedJob.id}</h2>
                  <span className={`text-sm px-2.5 py-0.5 rounded border font-medium ${STATUS_COLORS[selectedJob.status] ?? ''}`}>
                    {selectedJob.status}
                  </span>
                  {selectedJob.status === 'running' && (
                    <button
                      onClick={handleCancel}
                      className="text-sm px-3 py-1 rounded border border-err/40 text-err hover:bg-err/10 transition-colors"
                    >
                      Cancel
                    </button>
                  )}
                </div>

                {/* Stats grid */}
                <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
                  {[
                    { label: 'Backup Def', value: selectedJob.backupDefId ? `#${selectedJob.backupDefId}` : '—' },
                    { label: 'Started', value: new Date(selectedJob.startedAt).toLocaleString() },
                    { label: 'Duration', value: formatDuration(selectedJob.startedAt, selectedJob.completedAt) },
                    { label: 'Transferred', value: formatBytes(selectedJob.bytesTransferred) },
                  ].map(({ label, value }) => (
                    <div key={label} className="bg-surface border border-line rounded p-3">
                      <p className="text-xs text-muted mb-1">{label}</p>
                      <p className="text-sm font-medium text-body">{value}</p>
                    </div>
                  ))}
                </div>

                {/* Error */}
                {(selectedJob.status === 'failed' || selectedJob.status === 'cancelled') && selectedJob.errorMessage && (
                  <div className="bg-err/10 border border-err/30 rounded p-4">
                    <p className="text-xs font-semibold text-err mb-1">Error</p>
                    <p className="text-sm text-err font-mono">{selectedJob.errorMessage}</p>
                  </div>
                )}

                {/* Log output */}
                <div>
                  <p className="text-sm font-medium text-muted mb-2">Log output</p>
                  {selectedJob.status === 'running' ? (
                    <LogStream jobId={selectedJob.id} initialLog={selectedJob.logOutput} />
                  ) : (
                    <div className="bg-base rounded border border-line p-3 h-64 overflow-y-auto font-mono text-xs text-body">
                      {selectedJob.logOutput
                        ? selectedJob.logOutput.split('\n').filter(Boolean).map((line, i) => (
                            <div key={i} className={line.startsWith('[error]') ? 'text-err' : line.startsWith('[warn]') ? 'text-warn' : ''}>
                              {line}
                            </div>
                          ))
                        : <span className="text-faint">No log output.</span>
                      }
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <div className="flex items-center justify-center h-full text-faint">
                Select a job to view details
              </div>
            )}
          </div>
        </div>
      )}
      </div>
    </Shell>
  )
}
