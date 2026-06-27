'use client'
import { useEffect, useState } from 'react'
import Link from 'next/link'
import {
  listBackups, createBackup, updateBackup, deleteBackup, runBackupNow,
  type BackupDef, type CreateBackupInput,
} from '@/lib/api'

const NAMED_SCHEDULES = [
  { label: 'Daily (2 AM)', value: 'daily' },
  { label: 'Weekly (Sun 2 AM)', value: 'weekly' },
  { label: 'Every 6 hours', value: '6hourly' },
  { label: 'Hourly', value: 'hourly' },
  { label: 'Custom cron…', value: 'custom' },
]

const RETENTION_LABELS = ['critical', 'archive', 'personal']

const DEFAULT_FORM: CreateBackupInput & { schedulePreset: string } = {
  name: '',
  sourcePaths: [],
  schedule: 'daily',
  schedulePreset: 'daily',
  retentionLabel: 'archive',
  compressionLevel: 3,
  password: '',
}

export default function BackupsPage() {
  const [backups, setBackups] = useState<BackupDef[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ ...DEFAULT_FORM })
  const [sourcePathInput, setSourcePathInput] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [runningId, setRunningId] = useState<number | null>(null)
  const [runMsg, setRunMsg] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState<number | null>(null)

  useEffect(() => { refresh() }, [])

  async function refresh() {
    setLoading(true)
    try { setBackups(await listBackups()) } catch {}
    setLoading(false)
  }

  function openForm() {
    setForm({ ...DEFAULT_FORM })
    setSourcePathInput('')
    setError('')
    setShowForm(true)
  }

  function addPath() {
    const p = sourcePathInput.trim()
    if (!p || form.sourcePaths.includes(p)) return
    setForm(f => ({ ...f, sourcePaths: [...f.sourcePaths, p] }))
    setSourcePathInput('')
  }

  function removePath(p: string) {
    setForm(f => ({ ...f, sourcePaths: f.sourcePaths.filter(x => x !== p) }))
  }

  function handleSchedulePreset(preset: string) {
    setForm(f => ({
      ...f,
      schedulePreset: preset,
      schedule: preset === 'custom' ? '' : preset,
    }))
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!form.name || form.sourcePaths.length === 0 || !form.password) {
      setError('Name, at least one source path, and encryption password are required.')
      return
    }
    setSaving(true)
    setError('')
    try {
      const { schedulePreset, ...payload } = form
      await createBackup(payload)
      setShowForm(false)
      refresh()
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  async function handleToggleEnabled(b: BackupDef) {
    try {
      await updateBackup(b.id, { enabled: !b.enabled } as any)
      refresh()
    } catch {}
  }

  async function handleRunNow(id: number) {
    setRunningId(id)
    setRunMsg('')
    try {
      const result = await runBackupNow(id)
      setRunMsg(`Job #${result.jobId} started`)
    } catch (err: any) {
      setRunMsg(`Error: ${err.message}`)
    } finally {
      setRunningId(null)
    }
  }

  async function handleDelete(id: number) {
    try {
      await deleteBackup(id)
      setDeleteConfirm(null)
      refresh()
    } catch {}
  }

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      {/* Nav */}
      <nav className="border-b border-gray-800 px-6 py-4 flex items-center justify-between">
        <span className="font-bold text-lg">GlacierVault</span>
        <div className="flex gap-4 text-sm text-gray-400">
          <Link href="/">Dashboard</Link>
          <Link href="/backups" className="text-white">Backups</Link>
          <Link href="/snapshots">Snapshots</Link>
          <Link href="/restore">Restore</Link>
          <Link href="/jobs">Jobs</Link>
          <Link href="/settings">Settings</Link>
        </div>
      </nav>

      <main className="max-w-4xl mx-auto px-6 py-8 space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-bold">Backup Sources</h1>
          <button
            onClick={openForm}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded-lg text-sm font-medium transition-colors"
          >
            + New Backup
          </button>
        </div>

        {runMsg && (
          <div className="bg-green-900/30 border border-green-700 text-green-300 text-sm px-4 py-3 rounded-lg">
            {runMsg}
          </div>
        )}

        {/* Backup list */}
        {loading ? (
          <div className="text-gray-500 text-sm">Loading…</div>
        ) : backups.length === 0 ? (
          <div className="bg-gray-900 rounded-xl p-12 text-center">
            <p className="text-gray-400 text-lg mb-2">No backups configured yet</p>
            <p className="text-gray-600 text-sm mb-6">Add a backup source to get started.</p>
            <button
              onClick={openForm}
              className="px-5 py-2 bg-blue-600 hover:bg-blue-700 rounded-lg font-medium transition-colors"
            >
              + New Backup
            </button>
          </div>
        ) : (
          <div className="space-y-3">
            {backups.map(b => (
              <BackupCard
                key={b.id}
                backup={b}
                running={runningId === b.id}
                deleteConfirm={deleteConfirm === b.id}
                onToggle={() => handleToggleEnabled(b)}
                onRunNow={() => handleRunNow(b.id)}
                onDeleteClick={() => setDeleteConfirm(deleteConfirm === b.id ? null : b.id)}
                onDeleteConfirm={() => handleDelete(b.id)}
              />
            ))}
          </div>
        )}
      </main>

      {/* Create form modal */}
      {showForm && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50 p-4">
          <div className="bg-gray-900 rounded-xl w-full max-w-lg max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800">
              <h2 className="font-semibold text-lg">New Backup Source</h2>
              <button onClick={() => setShowForm(false)} className="text-gray-400 hover:text-white text-xl leading-none">×</button>
            </div>

            <form onSubmit={handleSubmit} className="px-6 py-5 space-y-5">
              {/* Name */}
              <Field label="Name" required>
                <input
                  type="text"
                  value={form.name}
                  onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                  placeholder="e.g. Home Photos"
                  className={inputCls}
                  required
                />
              </Field>

              {/* Source paths */}
              <Field label="Source Paths" required hint="Paths inside the container (mount host dirs via docker-compose volumes)">
                <div className="space-y-2">
                  {form.sourcePaths.map(p => (
                    <div key={p} className="flex items-center gap-2 bg-gray-800 px-3 py-1.5 rounded-lg text-sm font-mono">
                      <span className="flex-1 text-gray-200">{p}</span>
                      <button type="button" onClick={() => removePath(p)} className="text-gray-500 hover:text-red-400">×</button>
                    </div>
                  ))}
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={sourcePathInput}
                      onChange={e => setSourcePathInput(e.target.value)}
                      onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addPath() } }}
                      placeholder="/mnt/photos"
                      className={inputCls + ' font-mono flex-1'}
                    />
                    <button
                      type="button"
                      onClick={addPath}
                      className="px-3 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors"
                    >
                      Add
                    </button>
                  </div>
                </div>
              </Field>

              {/* Schedule */}
              <Field label="Schedule">
                <div className="space-y-2">
                  <div className="grid grid-cols-2 gap-2">
                    {NAMED_SCHEDULES.map(s => (
                      <button
                        key={s.value}
                        type="button"
                        onClick={() => handleSchedulePreset(s.value)}
                        className={`px-3 py-2 rounded-lg text-sm text-left transition-colors border ${
                          form.schedulePreset === s.value
                            ? 'bg-blue-600/20 border-blue-500 text-blue-300'
                            : 'bg-gray-800 border-gray-700 text-gray-300 hover:border-gray-500'
                        }`}
                      >
                        {s.label}
                      </button>
                    ))}
                  </div>
                  {form.schedulePreset === 'custom' && (
                    <input
                      type="text"
                      value={form.schedule}
                      onChange={e => setForm(f => ({ ...f, schedule: e.target.value }))}
                      placeholder="0 2 * * *"
                      className={inputCls + ' font-mono'}
                    />
                  )}
                </div>
              </Field>

              {/* Retention label */}
              <Field label="Retention Label">
                <div className="flex gap-2">
                  {RETENTION_LABELS.map(l => (
                    <button
                      key={l}
                      type="button"
                      onClick={() => setForm(f => ({ ...f, retentionLabel: l }))}
                      className={`px-3 py-1.5 rounded-lg text-sm capitalize transition-colors border ${
                        form.retentionLabel === l
                          ? 'bg-blue-600/20 border-blue-500 text-blue-300'
                          : 'bg-gray-800 border-gray-700 text-gray-300 hover:border-gray-500'
                      }`}
                    >
                      {l}
                    </button>
                  ))}
                </div>
              </Field>

              {/* Compression */}
              <Field label={`Compression Level: ${form.compressionLevel}`} hint="1 = fastest, 22 = smallest">
                <input
                  type="range"
                  min={1}
                  max={22}
                  value={form.compressionLevel}
                  onChange={e => setForm(f => ({ ...f, compressionLevel: Number(e.target.value) }))}
                  className="w-full accent-blue-500"
                />
              </Field>

              {/* Encryption password */}
              <Field label="Encryption Password" required hint="Used by Rustic to encrypt backup data. Store this safely.">
                <input
                  type="password"
                  value={form.password}
                  onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
                  placeholder="Strong passphrase"
                  className={inputCls}
                  required
                />
              </Field>

              {error && <p className="text-red-400 text-sm">{error}</p>}

              <div className="flex gap-3 pt-1">
                <button
                  type="submit"
                  disabled={saving}
                  className="flex-1 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-lg font-medium transition-colors"
                >
                  {saving ? 'Saving…' : 'Create Backup'}
                </button>
                <button
                  type="button"
                  onClick={() => setShowForm(false)}
                  className="px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
                >
                  Cancel
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}

function BackupCard({
  backup, running, deleteConfirm, onToggle, onRunNow, onDeleteClick, onDeleteConfirm,
}: {
  backup: BackupDef
  running: boolean
  deleteConfirm: boolean
  onToggle: () => void
  onRunNow: () => void
  onDeleteClick: () => void
  onDeleteConfirm: () => void
}) {
  const paths: string[] = (() => {
    try { return JSON.parse(backup.sourcePaths) } catch { return [backup.sourcePaths] }
  })()

  return (
    <div className="bg-gray-900 rounded-xl p-5 space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-semibold text-lg">{backup.name}</span>
            <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${
              backup.enabled ? 'bg-green-500/20 text-green-300' : 'bg-gray-700 text-gray-400'
            }`}>
              {backup.enabled ? 'enabled' : 'disabled'}
            </span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-gray-400">
            <span>⏱ {backup.schedule}</span>
            <span>🏷 {backup.retentionLabel}</span>
            <span>📦 compression {backup.compressionLevel}</span>
          </div>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-2 shrink-0">
          <button
            onClick={onRunNow}
            disabled={running}
            className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 rounded-lg text-sm transition-colors"
          >
            {running ? 'Running…' : '▶ Run now'}
          </button>
          <button
            onClick={onToggle}
            className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors"
          >
            {backup.enabled ? 'Disable' : 'Enable'}
          </button>
          <button
            onClick={onDeleteClick}
            className="px-3 py-1.5 bg-gray-700 hover:bg-red-900 rounded-lg text-sm transition-colors"
          >
            Delete
          </button>
        </div>
      </div>

      {/* Source paths */}
      <div className="flex flex-wrap gap-2">
        {paths.map(p => (
          <span key={p} className="bg-gray-800 text-gray-300 text-xs font-mono px-2 py-1 rounded">
            {p}
          </span>
        ))}
      </div>

      {/* Delete confirmation */}
      {deleteConfirm && (
        <div className="flex items-center gap-3 bg-red-950/50 border border-red-800 rounded-lg px-4 py-3 text-sm">
          <span className="text-red-300 flex-1">Delete <strong>{backup.name}</strong>? This cannot be undone.</span>
          <button
            onClick={onDeleteConfirm}
            className="px-3 py-1 bg-red-700 hover:bg-red-600 rounded text-white text-xs font-medium transition-colors"
          >
            Confirm Delete
          </button>
          <button
            onClick={onDeleteClick}
            className="px-3 py-1 bg-gray-700 hover:bg-gray-600 rounded text-xs transition-colors"
          >
            Cancel
          </button>
        </div>
      )}
    </div>
  )
}

function Field({ label, required, hint, children }: {
  label: string; required?: boolean; hint?: string; children: React.ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <label className="block text-sm font-medium text-gray-300">
        {label}
        {required && <span className="text-red-400 ml-0.5">*</span>}
      </label>
      {hint && <p className="text-xs text-gray-500">{hint}</p>}
      {children}
    </div>
  )
}

const inputCls = 'w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm'
