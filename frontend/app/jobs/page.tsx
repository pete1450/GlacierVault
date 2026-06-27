'use client'

import { useEffect, useRef, useState } from 'react'
import { listJobs, getJob, type Job } from '@/lib/api'

const STATUS_COLORS: Record<string, string> = {
  running: 'bg-blue-500/20 text-blue-300 border-blue-500/30',
  completed: 'bg-green-500/20 text-green-300 border-green-500/30',
  failed: 'bg-red-500/20 text-red-300 border-red-500/30',
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
    <div className="bg-gray-950 rounded border border-gray-700 p-3 h-64 overflow-y-auto font-mono text-xs text-gray-300">
      {lines.length === 0 ? (
        <span className="text-gray-600">No log output yet…</span>
      ) : (
        lines.map((line, i) => (
          <div key={i} className={line.startsWith('[error]') ? 'text-red-400' : line.startsWith('[warn]') ? 'text-yellow-400' : ''}>
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
      className={`w-full text-left px-4 py-3 border-b border-gray-800 hover:bg-gray-800/50 transition-colors ${selected ? 'bg-gray-800/70' : ''}`}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-3 min-w-0">
          <span className="text-gray-400 text-xs font-mono w-8 shrink-0">#{job.id}</span>
          <span className={`text-xs px-2 py-0.5 rounded border font-medium ${STATUS_COLORS[job.status] ?? 'bg-gray-700 text-gray-300 border-gray-600'}`}>
            {job.status}
          </span>
          <span className="text-sm text-gray-300 truncate">
            Backup job {job.backupDefId ? `(def #${job.backupDefId})` : '(setup)'}
          </span>
        </div>
        <div className="flex items-center gap-4 shrink-0 text-xs text-gray-500">
          {job.bytesTransferred > 0 && <span>{formatBytes(job.bytesTransferred)}</span>}
          <span>{formatDuration(job.startedAt, job.completedAt)}</span>
          <span>{new Date(job.startedAt).toLocaleString()}</span>
        </div>
      </div>
      {job.status === 'failed' && job.errorMessage && (
        <p className="mt-1 ml-11 text-xs text-red-400 truncate">{job.errorMessage}</p>
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
      // Auto-select first running job, then most recent
      if (selectedId === null) {
        const running = data.find((j) => j.status === 'running')
        setSelectedId(running?.id ?? data[0]?.id ?? null)
      }
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

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      {/* Header */}
      <div className="border-b border-gray-800 px-6 py-4">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold">Jobs</h1>
            <p className="text-sm text-gray-400 mt-0.5">Backup job history and live logs</p>
          </div>
          {runningCount > 0 && (
            <span className="flex items-center gap-2 text-sm text-blue-300 bg-blue-500/10 border border-blue-500/20 px-3 py-1.5 rounded-full">
              <span className="h-2 w-2 rounded-full bg-blue-400 animate-pulse" />
              {runningCount} running
            </span>
          )}
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-64 text-gray-500">Loading…</div>
      ) : error ? (
        <div className="m-6 bg-red-500/10 border border-red-500/30 rounded p-4 text-red-300">{error}</div>
      ) : jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-64 text-gray-500 gap-2">
          <p>No jobs yet.</p>
          <p className="text-sm">Run a backup from the Backups page to get started.</p>
        </div>
      ) : (
        <div className="flex h-[calc(100vh-73px)]">
          {/* Job list */}
          <div className="w-96 shrink-0 border-r border-gray-800 overflow-y-auto">
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
                </div>

                {/* Stats grid */}
                <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
                  {[
                    { label: 'Backup Def', value: selectedJob.backupDefId ? `#${selectedJob.backupDefId}` : '—' },
                    { label: 'Started', value: new Date(selectedJob.startedAt).toLocaleString() },
                    { label: 'Duration', value: formatDuration(selectedJob.startedAt, selectedJob.completedAt) },
                    { label: 'Transferred', value: formatBytes(selectedJob.bytesTransferred) },
                  ].map(({ label, value }) => (
                    <div key={label} className="bg-gray-900 border border-gray-800 rounded p-3">
                      <p className="text-xs text-gray-500 mb-1">{label}</p>
                      <p className="text-sm font-medium text-gray-200">{value}</p>
                    </div>
                  ))}
                </div>

                {/* Error */}
                {selectedJob.status === 'failed' && selectedJob.errorMessage && (
                  <div className="bg-red-500/10 border border-red-500/30 rounded p-4">
                    <p className="text-xs font-semibold text-red-400 mb-1">Error</p>
                    <p className="text-sm text-red-300 font-mono">{selectedJob.errorMessage}</p>
                  </div>
                )}

                {/* Log output */}
                <div>
                  <p className="text-sm font-medium text-gray-400 mb-2">Log output</p>
                  {selectedJob.status === 'running' ? (
                    <LogStream jobId={selectedJob.id} initialLog={selectedJob.logOutput} />
                  ) : (
                    <div className="bg-gray-950 rounded border border-gray-700 p-3 h-64 overflow-y-auto font-mono text-xs text-gray-300">
                      {selectedJob.logOutput
                        ? selectedJob.logOutput.split('\n').filter(Boolean).map((line, i) => (
                            <div key={i} className={line.startsWith('[error]') ? 'text-red-400' : line.startsWith('[warn]') ? 'text-yellow-400' : ''}>
                              {line}
                            </div>
                          ))
                        : <span className="text-gray-600">No log output.</span>
                      }
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <div className="flex items-center justify-center h-full text-gray-600">
                Select a job to view details
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
