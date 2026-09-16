'use client'
import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import Nav from '@/components/Nav'
import {
  changePassword, logout, rebuildCatalog, getSetupStatus, type SetupStatus,
} from '@/lib/api'

export default function SettingsPage() {
  const router = useRouter()
  const [status, setStatus] = useState<SetupStatus | null>(null)

  // Change password
  const [currentPw, setCurrentPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [pwMsg, setPwMsg] = useState('')
  const [pwSaving, setPwSaving] = useState(false)

  // Catalog rebuild
  const [rebuildMsg, setRebuildMsg] = useState('')

  useEffect(() => {
    getSetupStatus().then(setStatus).catch(() => {})
  }, [])

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault()
    setPwMsg('')
    if (newPw !== confirmPw) {
      setPwMsg('New passwords do not match.')
      return
    }
    setPwSaving(true)
    try {
      await changePassword(currentPw, newPw)
      setPwMsg('Password changed.')
      setCurrentPw(''); setNewPw(''); setConfirmPw('')
    } catch (err: any) {
      setPwMsg(`Error: ${err.message}`)
    } finally {
      setPwSaving(false)
    }
  }

  async function handleRebuild() {
    setRebuildMsg('')
    try {
      await rebuildCatalog()
      setRebuildMsg('Catalog rebuild started in the background.')
    } catch (err: any) {
      setRebuildMsg(`Error: ${err.message}`)
    }
  }

  async function handleLogout() {
    try { await logout() } catch {}
    router.push('/login')
  }

  return (
    <div className="min-h-screen bg-gray-950 text-white">
      <Nav />
      <main className="max-w-3xl mx-auto px-6 py-8 space-y-6">
        <h1 className="text-2xl font-bold">Settings</h1>

        {/* AWS / infrastructure */}
        <section className="bg-gray-900 rounded-xl p-6 space-y-3">
          <h2 className="font-semibold text-lg">Infrastructure</h2>
          {status ? (
            <dl className="grid grid-cols-2 gap-3 text-sm">
              <div><dt className="text-gray-500 text-xs">Region</dt><dd className="font-mono">{status.region || '—'}</dd></div>
              <div><dt className="text-gray-500 text-xs">Deployed</dt><dd>{status.deployedAt ? new Date(status.deployedAt).toLocaleString() : '—'}</dd></div>
              <div><dt className="text-gray-500 text-xs">Hot bucket</dt><dd className="font-mono break-all">{status.hotBucket || '—'}</dd></div>
              <div><dt className="text-gray-500 text-xs">Cold bucket</dt><dd className="font-mono break-all">{status.coldBucket || '—'}</dd></div>
            </dl>
          ) : (
            <p className="text-gray-500 text-sm">Loading…</p>
          )}
        </section>

        {/* Recovery */}
        <section className="bg-gray-900 rounded-xl p-6 space-y-4">
          <h2 className="font-semibold text-lg">Recovery</h2>
          <p className="text-sm text-gray-400">
            The recovery package contains everything needed to restore your data without this
            appliance: bucket names, region, and recovery instructions. Store it somewhere safe.
          </p>
          <div className="flex flex-wrap gap-3">
            <a
              href="/api/recovery/package"
              download
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded-lg text-sm font-medium transition-colors"
            >
              ⬇ Download recovery package
            </a>
            <button
              onClick={handleRebuild}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors"
            >
              Rebuild snapshot catalog
            </button>
          </div>
          {rebuildMsg && <p className="text-sm text-gray-300">{rebuildMsg}</p>}
          <p className="text-xs text-gray-500">
            Rebuilding re-indexes snapshots from the hot repository. Run this if snapshots are missing after a disruption.
          </p>
        </section>

        {/* Account */}
        <section className="bg-gray-900 rounded-xl p-6 space-y-4">
          <h2 className="font-semibold text-lg">Account</h2>
          <form onSubmit={handleChangePassword} className="space-y-3 max-w-sm">
            <div>
              <label className="block text-sm text-gray-300 mb-1">Current password</label>
              <input
                type="password"
                value={currentPw}
                onChange={e => setCurrentPw(e.target.value)}
                className="w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm"
                required
              />
            </div>
            <div>
              <label className="block text-sm text-gray-300 mb-1">New password</label>
              <input
                type="password"
                value={newPw}
                onChange={e => setNewPw(e.target.value)}
                className="w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm"
                required
                minLength={8}
              />
            </div>
            <div>
              <label className="block text-sm text-gray-300 mb-1">Confirm new password</label>
              <input
                type="password"
                value={confirmPw}
                onChange={e => setConfirmPw(e.target.value)}
                className="w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm"
                required
                minLength={8}
              />
            </div>
            {pwMsg && <p className={`text-sm ${pwMsg.startsWith('Error') ? 'text-red-400' : 'text-green-400'}`}>{pwMsg}</p>}
            <button
              type="submit"
              disabled={pwSaving}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 rounded-lg text-sm transition-colors"
            >
              {pwSaving ? 'Saving…' : 'Change password'}
            </button>
          </form>
          <div className="pt-2 border-t border-gray-800">
            <button
              onClick={handleLogout}
              className="px-4 py-2 bg-red-900/50 hover:bg-red-900 border border-red-800 rounded-lg text-sm text-red-300 transition-colors"
            >
              Log out
            </button>
          </div>
        </section>
      </main>
    </div>
  )
}
