'use client'
import { useState } from 'react'
import { useRouter } from 'next/navigation'
import { changePassword, logout, rebuildCatalog, errMsg } from '@/lib/api'

export default function GeneralPage() {
  const router = useRouter()

  // Change password
  const [currentPw, setCurrentPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [pwMsg, setPwMsg] = useState('')
  const [pwSaving, setPwSaving] = useState(false)

  // Catalog rebuild
  const [rebuildMsg, setRebuildMsg] = useState('')

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
      setPwMsg('Password changed. All other devices have been logged out.')
      setCurrentPw(''); setNewPw(''); setConfirmPw('')
    } catch (err) {
      setPwMsg(`Error: ${errMsg(err)}`)
    } finally {
      setPwSaving(false)
    }
  }

  async function handleRebuild() {
    setRebuildMsg('')
    try {
      await rebuildCatalog()
      setRebuildMsg('Catalog rebuild started in the background.')
    } catch (err) {
      setRebuildMsg(`Error: ${errMsg(err)}`)
    }
  }

  async function handleLogout() {
    try { await logout() } catch {}
    router.push('/login')
  }

  return (
    <div className="space-y-6">
      <section className="bg-surface rounded-xl p-6 space-y-4">
        <h2 className="font-semibold text-lg">Recovery</h2>
        <p className="text-sm text-muted">
          The recovery package contains everything needed to restore your data without this
          appliance: bucket names, region, and recovery instructions. Store it somewhere safe.
        </p>
        <div className="flex flex-wrap gap-3">
          <a
            href="/api/recovery/package"
            download
            className="px-4 py-2 bg-accent hover:bg-accent-deep rounded-lg text-sm font-medium transition-colors"
          >
            ⬇ Download recovery package
          </a>
          <button
            onClick={handleRebuild}
            className="px-4 py-2 bg-raised hover:bg-emboss rounded-lg text-sm transition-colors"
          >
            Rebuild snapshot catalog
          </button>
        </div>
        {rebuildMsg && <p className="text-sm text-body">{rebuildMsg}</p>}
        <p className="text-xs text-muted">
          Rebuilding re-indexes snapshots from the hot repository. Run this if snapshots are missing after a disruption.
        </p>
      </section>

      <section className="bg-surface rounded-xl p-6 space-y-4">
        <h2 className="font-semibold text-lg">Account</h2>
        <form onSubmit={handleChangePassword} className="space-y-3 max-w-sm">
          <div>
            <label className="block text-sm text-body mb-1">Current password</label>
            <input
              type="password"
              value={currentPw}
              onChange={e => setCurrentPw(e.target.value)}
              className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
              required
            />
          </div>
          <div>
            <label className="block text-sm text-body mb-1">New password</label>
            <input
              type="password"
              value={newPw}
              onChange={e => setNewPw(e.target.value)}
              className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
              required
              minLength={8}
            />
          </div>
          <div>
            <label className="block text-sm text-body mb-1">Confirm new password</label>
            <input
              type="password"
              value={confirmPw}
              onChange={e => setConfirmPw(e.target.value)}
              className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
              required
              minLength={8}
            />
          </div>
          {pwMsg && <p className={`text-sm ${pwMsg.startsWith('Error') ? 'text-err' : 'text-ok'}`}>{pwMsg}</p>}
          <button
            type="submit"
            disabled={pwSaving}
            className="px-4 py-2 bg-raised hover:bg-emboss disabled:opacity-50 rounded-lg text-sm transition-colors"
          >
            {pwSaving ? 'Saving…' : 'Change password'}
          </button>
        </form>
        <div className="pt-2 border-t border-line">
          <button
            onClick={handleLogout}
            className="px-4 py-2 bg-err/10 hover:bg-err/20 border border-err/40 rounded-lg text-sm text-err transition-colors"
          >
            Log out
          </button>
        </div>
      </section>
    </div>
  )
}
