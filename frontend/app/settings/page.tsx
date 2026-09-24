'use client'
import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import Nav from '@/components/Nav'
import {
  changePassword, logout, rebuildCatalog, getSetupStatus, type SetupStatus,
  getCloudFrontStatus, enableCloudFront, disableCloudFront, type CloudFrontStatus,
  getNotificationConfig, saveNotificationConfig, testNotification,
} from '@/lib/api'

function Toggle({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={() => onChange(!on)}
      className={`relative w-10 h-6 shrink-0 rounded-full transition-colors ${on ? 'bg-blue-600' : 'bg-gray-700'}`}
    >
      <span
        className={`absolute top-0.5 w-5 h-5 rounded-full bg-white transition-all ${on ? 'left-[18px]' : 'left-0.5'}`}
      />
    </button>
  )
}

export default function SettingsPage() {  const router = useRouter()
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [cf, setCf] = useState<CloudFrontStatus | null>(null)

  // Change password
  const [currentPw, setCurrentPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [pwMsg, setPwMsg] = useState('')
  const [pwSaving, setPwSaving] = useState(false)

  // Catalog rebuild
  const [rebuildMsg, setRebuildMsg] = useState('')

  // CloudFront free-egress path
  const [cfAccessKey, setCfAccessKey] = useState('')
  const [cfSecretKey, setCfSecretKey] = useState('')
  const [cfMsg, setCfMsg] = useState('')
  const [cfBusy, setCfBusy] = useState(false)

  // Notifications (apprise)
  const [destinations, setDestinations] = useState('')
  const [swBackup, setSwBackup] = useState(false)
  const [swWarmup, setSwWarmup] = useState(false)
  const [swRestore, setSwRestore] = useState(false)
  const [notifyMsg, setNotifyMsg] = useState('')
  const [notifySaving, setNotifySaving] = useState(false)
  const [notifyTesting, setNotifyTesting] = useState(false)

  useEffect(() => {
    getSetupStatus().then(setStatus).catch(() => {})
    getCloudFrontStatus().then(setCf).catch(() => {})
    getNotificationConfig().then(cfg => {
      setDestinations((cfg.destinations || []).join('\n'))
      setSwBackup(cfg.notifyBackupCompleted)
      setSwWarmup(cfg.notifyWarmupCompleted)
      setSwRestore(cfg.notifyRestoreCompleted)
    }).catch(() => {})
  }, [])

  async function refreshCf() {
    try { setCf(await getCloudFrontStatus()) } catch {}
  }

  async function handleCfEnable(e: React.FormEvent) {
    e.preventDefault()
    setCfMsg('')
    setCfBusy(true)
    try {
      await enableCloudFront(cfAccessKey, cfSecretKey)
      setCfAccessKey(''); setCfSecretKey('')
      setCfMsg('Provisioning started — this takes a few minutes. This page will update when it is ready.')
      // Poll until provisioning finishes.
      for (let i = 0; i < 40; i++) {
        await new Promise(r => setTimeout(r, 15000))
        const s = await getCloudFrontStatus().catch(() => null)
        if (!s) continue
        setCf(s)
        if (!s.provisioning) break
      }
      await refreshCf()
    } catch (err: any) {
      setCfMsg(`Error: ${err.message}`)
    } finally {
      setCfBusy(false)
    }
  }

  async function handleCfDisable() {
    setCfMsg('')
    try {
      await disableCloudFront()
      setCfMsg('Free-egress path disabled. Restores will use paid S3 egress.')
      await refreshCf()
    } catch (err: any) {
      setCfMsg(`Error: ${err.message}`)
    }
  }

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

  function destinationList() {
    return destinations.split('\n').map(s => s.trim()).filter(Boolean)
  }

  async function handleNotifySave() {
    setNotifyMsg('')
    setNotifySaving(true)
    try {
      await saveNotificationConfig({
        destinations: destinationList(),
        notifyBackupCompleted: swBackup,
        notifyWarmupCompleted: swWarmup,
        notifyRestoreCompleted: swRestore,
      })
      setNotifyMsg('Notification settings saved.')
    } catch (err: any) {
      setNotifyMsg(`Error: ${err.message}`)
    } finally {
      setNotifySaving(false)
    }
  }

  async function handleNotifyTest() {
    setNotifyMsg('')
    setNotifyTesting(true)
    try {
      await testNotification(destinationList())
      setNotifyMsg('Test notification sent — check your destinations.')
    } catch (err: any) {
      setNotifyMsg(`Error: ${err.message}`)
    } finally {
      setNotifyTesting(false)
    }
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

        {/* CloudFront free-egress restores */}
        <section className="bg-gray-900 rounded-xl p-6 space-y-4">
          <h2 className="font-semibold text-lg">Free-egress restores (CloudFront)</h2>
          <p className="text-sm text-gray-400">
            Restore downloads can go through a private CloudFront distribution instead of
            directly from S3, so they consume CloudFront's monthly free data-transfer
            allowance (1 TB/month, shared across your account) instead of paid S3 egress.
            The bucket stays private: the distribution requires signed URLs, which are
            minted inside the appliance by a localhost-only proxy and never leave it.
            The distribution is provisioned automatically during initial setup with the
            credentials you entered there — the manual option below is only for
            appliances set up before this feature existed or where setup provisioning
            was skipped.
          </p>
          {cf ? (
            <div className="text-sm space-y-2">
              <div className="flex items-center gap-2">
                <span className={`inline-block w-2.5 h-2.5 rounded-full ${cf.enabled ? 'bg-green-500' : 'bg-gray-600'}`} />
                <span>{cf.provisioning ? 'Provisioning…' : cf.enabled ? 'Enabled' : 'Not enabled'}</span>
                {cf.enabled && !cf.proxyReady && !cf.provisioning && (
                  <span className="text-amber-400 text-xs">(proxy not running — restores will use direct S3)</span>
                )}
              </div>
              {cf.enabled && cf.domain && (
                <div><span className="text-gray-500 text-xs">Distribution</span><div className="font-mono break-all">{cf.domain}</div></div>
              )}
              {cf.lastError && (
                <p className="text-red-400 text-xs break-all">{cf.lastError}</p>
              )}
            </div>
          ) : (
            <p className="text-gray-500 text-sm">Loading…</p>
          )}
          {!cf?.enabled && !cf?.provisioning && (
            <details className="max-w-sm">
              <summary className="text-sm text-gray-400 cursor-pointer hover:text-gray-200">
                Manually provision (only if initial setup didn't)
              </summary>
              <form onSubmit={handleCfEnable} className="space-y-3 mt-3">
                <p className="text-sm text-gray-400">
                  Enable with a temporary AWS admin access key (the same kind used during
                  setup). It is used for this provisioning request only and is never stored.
                </p>
                <div>
                  <label className="block text-sm text-gray-300 mb-1">AWS access key ID</label>
                  <input
                    type="text"
                    value={cfAccessKey}
                    onChange={e => setCfAccessKey(e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm font-mono"
                    required
                    autoComplete="off"
                  />
                </div>
                <div>
                  <label className="block text-sm text-gray-300 mb-1">AWS secret access key</label>
                  <input
                    type="password"
                    value={cfSecretKey}
                    onChange={e => setCfSecretKey(e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm font-mono"
                    required
                    autoComplete="off"
                  />
                </div>
                <button
                  type="submit"
                  disabled={cfBusy}
                  className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 rounded-lg text-sm font-medium transition-colors"
                >
                  {cfBusy ? 'Provisioning…' : 'Enable free-egress restores'}
                </button>
              </form>
            </details>
          )}
          {cf?.enabled && !cf?.provisioning && (
            <button
              onClick={handleCfDisable}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors"
            >
              Disable free-egress path
            </button>
          )}
          {cfMsg && <p className="text-sm text-gray-300">{cfMsg}</p>}
        </section>

        {/* Notifications */}
        <section className="bg-gray-900 rounded-xl p-6 space-y-4">
          <h2 className="font-semibold text-lg">Notifications</h2>
          <p className="text-sm text-gray-400">
            GlacierVault sends notifications through{' '}
            <a href="https://github.com/caronc/apprise/wiki" target="_blank" rel="noreferrer" className="text-blue-400 hover:underline">
              Apprise
            </a>
            , which supports Discord, Slack, Telegram, email, ntfy, Gotify, webhooks and
            dozens more. Add one destination URL per line — pick the format for your
            service from the Apprise wiki.
          </p>
          <div>
            <label className="block text-sm text-gray-300 mb-1">Destination URLs (one per line)</label>
            <textarea
              value={destinations}
              onChange={e => setDestinations(e.target.value)}
              rows={4}
              spellCheck={false}
              placeholder={'discord://webhook_id/webhook_token\nntfy://ntfy.sh/my-topic'}
              className="w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 text-sm font-mono"
            />
          </div>
          <div className="space-y-3">
            <div className="flex items-center gap-3">
              <Toggle on={swBackup} onChange={setSwBackup} label="Backup completed" />
              <div>
                <div className="text-sm">Backup completed</div>
                <div className="text-xs text-gray-500">When a scheduled or manual backup finishes successfully.</div>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <Toggle on={swWarmup} onChange={setSwWarmup} label="Warmup complete" />
              <div>
                <div className="text-sm">Warmup complete</div>
                <div className="text-xs text-gray-500">When Glacier finishes thawing a restore's packs and the download starts.</div>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <Toggle on={swRestore} onChange={setSwRestore} label="Restore complete" />
              <div>
                <div className="text-sm">Restore complete</div>
                <div className="text-xs text-gray-500">When a restore finishes and the files are on disk.</div>
              </div>
            </div>
          </div>
          <div className="flex flex-wrap gap-3">
            <button
              onClick={handleNotifySave}
              disabled={notifySaving}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 rounded-lg text-sm font-medium transition-colors"
            >
              {notifySaving ? 'Saving…' : 'Save notification settings'}
            </button>
            <button
              onClick={handleNotifyTest}
              disabled={notifyTesting}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 rounded-lg text-sm transition-colors"
            >
              {notifyTesting ? 'Sending…' : 'Send test notification'}
            </button>
          </div>
          {notifyMsg && <p className={`text-sm ${notifyMsg.startsWith('Error') ? 'text-red-400' : 'text-green-400'}`}>{notifyMsg}</p>}
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
