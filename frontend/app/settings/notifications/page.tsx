'use client'
import { useEffect, useState } from 'react'
import {
  getNotificationConfig, saveNotificationConfig, testNotification,
  errMsg,
} from '@/lib/api'

function Toggle({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={() => onChange(!on)}
      className={`relative w-10 h-6 shrink-0 rounded-full transition-colors ${on ? 'bg-accent' : 'bg-raised'}`}
    >
      <span
        className={`absolute top-0.5 w-5 h-5 rounded-full bg-white transition-all ${on ? 'left-[18px]' : 'left-0.5'}`}
      />
    </button>
  )
}

export default function NotificationsPage() {
  const [destinations, setDestinations] = useState('')
  const [swBackup, setSwBackup] = useState(false)
  const [swWarmup, setSwWarmup] = useState(false)
  const [swRestore, setSwRestore] = useState(false)
  const [notifyMsg, setNotifyMsg] = useState('')
  const [notifySaving, setNotifySaving] = useState(false)
  const [notifyTesting, setNotifyTesting] = useState(false)

  useEffect(() => {
    getNotificationConfig().then(cfg => {
      setDestinations((cfg.destinations || []).join('\n'))
      setSwBackup(cfg.notifyBackupCompleted)
      setSwWarmup(cfg.notifyWarmupCompleted)
      setSwRestore(cfg.notifyRestoreCompleted)
    }).catch(() => {})
  }, [])

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
    } catch (err) {
      setNotifyMsg(`Error: ${errMsg(err)}`)
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
    } catch (err) {
      setNotifyMsg(`Error: ${errMsg(err)}`)
    } finally {
      setNotifyTesting(false)
    }
  }

  function destinationList() {
    return destinations.split('\n').map(s => s.trim()).filter(Boolean)
  }

  return (
    <div className="space-y-6">
    <section className="bg-surface rounded-xl p-6 space-y-4">
      <h2 className="font-semibold text-lg">Notifications</h2>
      <p className="text-sm text-muted">
        GlacierVault sends notifications through{' '}
        <a href="https://github.com/caronc/apprise/wiki" target="_blank" rel="noreferrer" className="text-accent hover:underline">
          Apprise
        </a>
        , which supports Discord, Slack, Telegram, email, ntfy, Gotify, webhooks and
        dozens more. Add one destination URL per line — pick the format for your
        service from the Apprise wiki.
      </p>
      <div>
        <label className="block text-sm text-body mb-1">Destination URLs (one per line)</label>
        <textarea
          value={destinations}
          onChange={e => setDestinations(e.target.value)}
          rows={4}
          spellCheck={false}
          placeholder={'discord://webhook_id/webhook_token\nntfy://ntfy.sh/my-topic'}
          className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm font-mono"
        />
      </div>
      <div className="space-y-3">
        <div className="flex items-center gap-3">
          <Toggle on={swBackup} onChange={setSwBackup} label="Backup completed" />
          <div>
            <div className="text-sm">Backup completed</div>
            <div className="text-xs text-muted">When a scheduled or manual backup finishes successfully.</div>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Toggle on={swWarmup} onChange={setSwWarmup} label="Warmup complete" />
          <div>
            <div className="text-sm">Warmup complete</div>
            <div className="text-xs text-muted">When Glacier finishes thawing a restore&apos;s packs and the download starts.</div>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Toggle on={swRestore} onChange={setSwRestore} label="Restore complete" />
          <div>
            <div className="text-sm">Restore complete</div>
            <div className="text-xs text-muted">When a restore finishes and the files are on disk.</div>
          </div>
        </div>
      </div>
      <div className="flex flex-wrap gap-3">
        <button
          onClick={handleNotifySave}
          disabled={notifySaving}
          className="px-4 py-2 bg-accent hover:bg-accent-deep disabled:bg-raised rounded-lg text-sm font-medium transition-colors"
        >
          {notifySaving ? 'Saving…' : 'Save notification settings'}
        </button>
        <button
          onClick={handleNotifyTest}
          disabled={notifyTesting}
          className="px-4 py-2 bg-raised hover:bg-emboss disabled:opacity-50 rounded-lg text-sm transition-colors"
        >
          {notifyTesting ? 'Sending…' : 'Send test notification'}
        </button>
      </div>
      {notifyMsg && <p className={`text-sm ${notifyMsg.startsWith('Error') ? 'text-err' : 'text-ok'}`}>{notifyMsg}</p>}
    </section>
    </div>
  )
}
